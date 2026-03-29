package ext

import (
	"regexp"
	"sync"
	"testing"
)

func TestInMemoryTokenVault_StoreReuseAndRestore(t *testing.T) {
	vault := NewInMemoryTokenVault()
	token1 := vault.Store(TokenNamespacePII, "alice@example.com")
	token2 := vault.Store(TokenNamespacePII, "alice@example.com")
	if token1 != token2 {
		t.Fatalf("expected stable token reuse, got %q vs %q", token1, token2)
	}

	if !regexp.MustCompile(`^\[GUARDY_TOKEN_PII_[0-9]+\]$`).MatchString(token1) {
		t.Fatalf("unexpected token format: %q", token1)
	}

	restored, ok := vault.Restore(token1)
	if !ok {
		t.Fatal("expected restore success")
	}
	if restored != "alice@example.com" {
		t.Fatalf("restored = %q", restored)
	}
}

func TestInMemoryTokenVault_NamespaceSeparation(t *testing.T) {
	vault := NewInMemoryTokenVault()
	piiToken := vault.Store(TokenNamespacePII, "secret")
	wordToken := vault.Store(TokenNamespaceWordlist, "secret")
	if piiToken == wordToken {
		t.Fatalf("tokens must differ across namespaces: %q", piiToken)
	}
}

func TestInMemoryTokenVault_ConcurrentStore(t *testing.T) {
	vault := NewInMemoryTokenVault()
	const workers = 64
	results := make(chan string, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			results <- vault.Store(TokenNamespacePII, "bob@example.com")
		})
	}
	wg.Wait()
	close(results)

	var first string
	for tok := range results {
		if first == "" {
			first = tok
			continue
		}
		if tok != first {
			t.Fatalf("expected same token for identical original, got %q and %q", first, tok)
		}
	}
}

func TestUnredactText(t *testing.T) {
	vault := NewInMemoryTokenVault()
	tok := vault.Store(TokenNamespacePII, "charlie@example.com")
	got := UnredactText("email: "+tok, vault)
	if got != "email: charlie@example.com" {
		t.Fatalf("got = %q", got)
	}
	unknown := UnredactText("x [GUARDY_TOKEN_PII_999999]", vault)
	if unknown != "x [GUARDY_TOKEN_PII_999999]" {
		t.Fatalf("unknown token should stay unchanged, got %q", unknown)
	}
}

type alphaTokenVault struct {
	token string
	value string
}

func (v alphaTokenVault) Store(_, _ string) string {
	return v.token
}

func (v alphaTokenVault) Restore(token string) (string, bool) {
	if token != v.token {
		return "", false
	}
	return v.value, true
}

func TestUnredactText_AlphanumericTokenID(t *testing.T) {
	vault := alphaTokenVault{
		token: "[GUARDY_TOKEN_PII_AB12_CD]",
		value: "delta@example.com",
	}
	got := UnredactText("email: "+vault.token, vault)
	if got != "email: delta@example.com" {
		t.Fatalf("got = %q", got)
	}
}
