package ext

import (
	"errors"
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

// TokenVault stores reversible redaction mappings, not disclosure permissions.
// The host owns lifetime/isolation, recipient authorization and final output checks.
type TokenVault interface {
	Store(namespace, original string) (token string, err error)
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
func (v *InMemoryTokenVault) Store(namespace, original string) (string, error) {
	if v == nil {
		return "", &TokenStorageError{Code: "nil_vault", Cause: nil}
	}
	ns := normalizeNamespace(namespace)
	key := ns + "\x00" + original

	v.mu.Lock()
	defer v.mu.Unlock()
	if v.nextByNS == nil {
		v.nextByNS = make(map[string]uint64)
	}
	if v.pairs == nil {
		v.pairs = make(map[string]string)
	}
	if v.restores == nil {
		v.restores = make(map[string]string)
	}
	if token, ok := v.pairs[key]; ok {
		return token, nil
	}

	v.nextByNS[ns]++
	token := fmt.Sprintf("[GUARDY_TOKEN_%s_%d]", ns, v.nextByNS[ns])
	v.pairs[key] = token
	v.restores[token] = original
	return token, nil
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
// Possession of a token does not authorize disclosure. The host must authorize the
// recipient before restoration and validate the final destination payload.
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

// ErrTokenStorage indicates loss of the requested reversible-redaction contract.
var ErrTokenStorage = errors.New("ext: token storage failed")

// TokenStorageError exposes a safe category and an explicitly inspectable cause.
type TokenStorageError struct {
	Code  string
	Cause error
}

func (e *TokenStorageError) Error() string { return ErrTokenStorage.Error() }
func (e *TokenStorageError) Unwrap() error { return errors.Join(ErrTokenStorage, e.Cause) }

// No configured vault means explicit irreversible redaction. A configured vault
// never silently degrades: errors, panic and empty/identity tokens are faults.
//
//nolint:nonamedreturns // recovery sets the typed fault instead of publishing partial replacement.
func storeRedaction(vault TokenVault, namespace, original, replacement string) (token string, err error) {
	if vault == nil {
		return replacement, nil
	}
	defer func() {
		if caught := recover(); caught != nil {
			token = ""
			err = &TokenStorageError{Code: "panic", Cause: fmt.Errorf("vault panic: %v", caught)}
		}
	}()
	token, err = vault.Store(namespace, original)
	if err != nil {
		return "", &TokenStorageError{Code: "store_failed", Cause: err}
	}
	if token == "" {
		return "", &TokenStorageError{Code: "empty_token", Cause: nil}
	}
	if token == original {
		return "", &TokenStorageError{Code: "identity_token", Cause: nil}
	}
	return token, nil
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
