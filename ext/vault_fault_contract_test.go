package ext

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/guardy"
)

type faultVault struct {
	store func(string, string) (string, error)
}

func (v faultVault) Store(ns, original string) (string, error) { return v.store(ns, original) }
func (faultVault) Restore(string) (string, bool)               { return "", false }

func TestConfiguredVaultNeverDegrades(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("private storage detail")
	for _, tc := range []struct {
		name  string
		store func(string, string) (string, error)
		cause error
	}{
		{"error", func(string, string) (string, error) { return "token", sentinel }, sentinel},
		{"panic", func(string, string) (string, error) { panic("private panic detail") }, nil},
		{"empty", func(string, string) (string, error) { return "", nil }, nil},
		{"identity", func(_ string, s string) (string, error) { return s, nil }, nil},
		{"cancel", func(string, string) (string, error) { return "", context.Canceled }, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, recipe := range []struct {
				name, input string
				build       func(TokenVault) guardy.Validator[string]
			}{
				{"pii", "user@example.com", func(v TokenVault) guardy.Validator[string] { return MustPIIValidator(WithTokenVault(v)) }},
				{"wordlist", "secret", func(v TokenVault) guardy.Validator[string] {
					return MustWordlistValidator([]string{"secret"}, Blocklist, WithAction(guardy.ActionRedact), WithTokenVault(v))
				}},
			} {
				t.Run(recipe.name, func(t *testing.T) {
					// Arrange.
					v := recipe.build(faultVault{tc.store})
					// Act.
					output, report, err := v.Validate(t.Context(), recipe.input)
					delivery, boundaryErr := guardy.NewPipeline(guardy.WithFastPath(v)).
						GuardOutput(t.Context(), nil, recipe.input)
					// Assert.
					var storage *TokenStorageError
					if output != recipe.input || report != nil || !errors.Is(err, ErrTokenStorage) ||
						!errors.As(err, &storage) {
						t.Fatalf("output=%q report=%+v error=%v", output, report, err)
					}
					if tc.cause != nil && !errors.Is(err, tc.cause) {
						t.Fatalf("missing cause: %v", err)
					}
					if storage.Error() != ErrTokenStorage.Error() {
						t.Fatalf("unsafe message %q", storage.Error())
					}
					if boundaryErr == nil || delivery.Deliverable || delivery.Value != "" {
						t.Fatalf("delivery=%+v error=%v", delivery, boundaryErr)
					}
				})
			}
		})
	}
}

func TestVaultLaterFailureDiscardsPartialTransformation(t *testing.T) {
	// Arrange.
	calls := 0
	v := MustPIIValidator(WithTokenVault(faultVault{func(string, string) (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("failed")
		}
		return "[GUARDY_TOKEN_PII_1]", nil
	}}))
	input := "first@example.com second@example.com"
	// Act.
	output, report, err := v.Validate(t.Context(), input)
	// Assert: already performed Store side effects are host-owned; transformed output is discarded.
	if calls != 2 || output != input || report != nil || !errors.Is(err, ErrTokenStorage) {
		t.Fatalf("calls=%d output=%q report=%+v err=%v", calls, output, report, err)
	}
}

func TestVaultZeroValueAndAllowlistNoTokens(t *testing.T) {
	// Arrange.
	var vault InMemoryTokenVault
	v := MustWordlistValidator([]string{"allowed"}, Allowlist, WithAction(guardy.ActionRedact), WithTokenVault(&vault))
	// Act.
	output, report, err := v.Validate(t.Context(), "!!!")
	restored, ok := vault.Restore(output)
	// Assert.
	if err != nil || report.Action != guardy.ActionRedact || !ok || restored != "!!!" {
		t.Fatalf("output=%q report=%+v err=%v restore=%q/%v", output, report, err, restored, ok)
	}
	var absent *InMemoryTokenVault
	_, err = absent.Store("PII", "secret")
	if !errors.Is(err, ErrTokenStorage) {
		t.Fatalf("nil receiver error=%v", err)
	}
}
