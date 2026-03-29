package ext

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Canonical token namespaces used by built-in validators.
const (
	TokenNamespacePII      = "PII"
	TokenNamespaceWordlist = "WORDLIST"
)

// TokenVault stores reversible redaction mappings.
type TokenVault interface {
	Store(namespace, original string) (token string)
	Restore(token string) (original string, ok bool)
}

// InMemoryTokenVault is a thread-safe reference TokenVault implementation.
type InMemoryTokenVault struct {
	mu       sync.RWMutex
	nextByNS map[string]uint64
	pairs    map[string]string
	restores map[string]string
}

// NewInMemoryTokenVault creates a thread-safe vault for request/session scope.
func NewInMemoryTokenVault() *InMemoryTokenVault {
	return &InMemoryTokenVault{
		nextByNS: make(map[string]uint64),
		pairs:    make(map[string]string),
		restores: make(map[string]string),
	}
}

// Store saves original data in namespace and returns canonical token
// [GUARDY_TOKEN_{NAMESPACE}_{ID}].
func (v *InMemoryTokenVault) Store(namespace, original string) string {
	if v == nil {
		return ""
	}
	ns := normalizeNamespace(namespace)
	key := ns + "\x00" + original

	v.mu.Lock()
	defer v.mu.Unlock()
	if token, ok := v.pairs[key]; ok {
		return token
	}

	v.nextByNS[ns]++
	token := fmt.Sprintf("[GUARDY_TOKEN_%s_%d]", ns, v.nextByNS[ns])
	v.pairs[key] = token
	v.restores[token] = original
	return token
}

// Restore maps token back to original content.
func (v *InMemoryTokenVault) Restore(token string) (string, bool) {
	if v == nil {
		return "", false
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	original, ok := v.restores[token]
	return original, ok
}

var guardyTokenRE = regexp.MustCompile(`\[GUARDY_TOKEN_[A-Z0-9_]+_[A-Z0-9_]+\]`)

// UnredactText restores known guardy tokens using vault.
func UnredactText(text string, vault TokenVault) string {
	if vault == nil {
		return text
	}
	return guardyTokenRE.ReplaceAllStringFunc(text, func(tok string) string {
		original, ok := vault.Restore(tok)
		if !ok {
			return tok
		}
		return original
	})
}

//nolint:nonamedreturns // named result is assigned from defer/recover path
func storeTokenOrFallback(vault TokenVault, namespace, original, fallback string) (token string) {
	if vault == nil {
		return fallback
	}
	defer func() {
		if recover() != nil {
			token = fallback
		}
	}()
	token = vault.Store(namespace, original)
	if token == "" || token == original {
		return fallback
	}
	return token
}

func normalizeNamespace(namespace string) string {
	if namespace == "" {
		return "GENERIC"
	}
	up := strings.ToUpper(namespace)
	var b strings.Builder
	for i := range len(up) {
		ch := up[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			b.WriteByte(ch)
		}
	}
	if b.Len() == 0 {
		return "GENERIC"
	}
	return b.String()
}
