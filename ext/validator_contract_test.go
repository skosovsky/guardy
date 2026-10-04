package ext

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestUnicodeWordlistDetectionAndLiteralRedaction(t *testing.T) {
	for _, word := range []string{"секрет", "secрет", "cafe\u0301", "用户", "слово_42", "\u0301"} {
		for _, mode := range []WordlistMode{Blocklist, Allowlist} {
			// Arrange: allowlist omits the word, blocklist includes it.
			words := []string{word}
			if mode == Allowlist {
				words = []string{"ok"}
			}
			raw := "ok, " + word + "!"
			validator := NewWordlistValidator(
				words,
				mode,
				WithAction(guardy.ActionRedact),
				WithRedactionReplacement(`$1\literal`),
			)
			// Act.
			out, report, err := validator.Validate(context.Background(), raw)
			// Assert.
			if err != nil || report.Action != guardy.ActionRedact || out != `ok, $1\literal!` {
				t.Fatalf("word=%q mode=%v output=%q report=%+v err=%v", word, mode, out, report, err)
			}
			blocker := NewWordlistValidator(words, mode)
			_, blocked, err := blocker.Validate(context.Background(), raw)
			if err != nil || blocked.Action != guardy.ActionBlock {
				t.Fatalf("word=%q report=%+v err=%v", word, blocked, err)
			}
		}
	}
}

func TestUnicodeWordlistCaseAndNoNormalization(t *testing.T) {
	// Arrange.
	folded := NewWordlistValidator(
		[]string{"СЕКРЕТ", "İ"},
		Blocklist,
		WithLowercase(true),
		WithAction(guardy.ActionRedact),
	)
	raw := "секрет İ сосед"
	// Act.
	out, report, foldErr := folded.Validate(context.Background(), raw)
	// Assert: byte spans from original text survive case mappings with different byte lengths.
	if foldErr != nil || report.Action != guardy.ActionRedact || out != "[REDACTED] [REDACTED] сосед" {
		t.Fatalf("%q %+v %v", out, report, foldErr)
	}
	validator := NewWordlistValidator([]string{"café"}, Blocklist)
	for _, benign := range []string{"cafe\u0301", "безопасно, 42!", "notcafé", "café_42", "CAFE"} {
		_, rep, err := validator.Validate(context.Background(), benign)
		if err != nil || rep.Action != guardy.ActionPass {
			t.Fatalf("benign=%q report=%+v err=%v", benign, rep, err)
		}
	}
	allowed := NewWordlistValidator([]string{"привет", "用户", "42"}, Allowlist)
	_, rep, err := allowed.Validate(context.Background(), "привет, 用户! 42")
	if err != nil || rep.Action != guardy.ActionPass {
		t.Fatalf("%+v %v", rep, err)
	}
}

func TestWordlistInvalidConfigurationFailsFast(t *testing.T) {
	for _, words := range [][]string{{""}, {"two words"}, {"word!"}} {
		t.Run(strings.Join(words, ""), func(t *testing.T) {
			// Arrange / Assert.
			defer func() {
				if recover() == nil {
					t.Fatal("invalid word accepted")
				}
			}()
			// Act.
			NewWordlistValidator(words, Blocklist)
		})
	}
}

func TestPIIFiniteFormatsAndNoTails(t *testing.T) {
	for _, raw := range []string{
		"user@example.com", "5551234567", "555-123-4567", "555.123.4567", "(555) 123 4567", "+1 (555) 123-4567", "15551234567",
		"+7 (999) 123-45-67", "+79991234567", "+7.999.123.45.67", "4111111111111", "4111111111111111", "4111111111111111111",
		"4111 1111 1111 1111", "4111-1111-1111-1111", "5111111111111111", "5511 1111 1111 1111",
		"4111111111111111@example.com", "5551234567@example.com",
	} {
		t.Run(raw, func(t *testing.T) {
			// Arrange.
			validator := NewPIIValidator(WithRedactionReplacement(`$1\literal`))
			// Act.
			out, report, err := validator.Validate(context.Background(), raw)
			// Assert: entire recognized span replaced once, with no expansion or leftover digits.
			if err != nil || report.Action != guardy.ActionRedact || out != `$1\literal` {
				t.Fatalf("%q %+v %v", out, report, err)
			}
		})
	}
	for _, benign := range []string{"Привет, 42!", "12345", "9007199254740993", "order4111111111111111", "41111111111111111111", "6111111111111111", "5611111111111111", "+44 20 1234 5678", "用户@example.com"} {
		// Arrange / Act.
		out, report, err := NewPIIValidator().Validate(context.Background(), benign)
		// Assert.
		if err != nil || report.Action != guardy.ActionPass || out != benign {
			t.Fatalf("%q: %q %+v %v", benign, out, report, err)
		}
	}
}

type countingPIIVault struct{ originals []string }

func (v *countingPIIVault) Store(_ string, original string) string {
	v.originals = append(v.originals, original)
	return "replacement@example.com"
}
func (*countingPIIVault) Restore(_ string) (string, bool) { return "", false }

func TestPIIOverlapUsesOriginalSpansAndNeverRescansTokens(t *testing.T) {
	// Arrange.
	vault := &countingPIIVault{}
	validator := NewPIIValidator(WithTokenVault(vault))
	// Act.
	out, report, err := validator.Validate(context.Background(), "4111111111111111@example.com and 5551234567")
	// Assert.
	if err != nil || report.Action != guardy.ActionRedact ||
		out != "replacement@example.com and replacement@example.com" ||
		len(vault.originals) != 2 ||
		vault.originals[0] != "4111111111111111@example.com" ||
		vault.originals[1] != "5551234567" {
		t.Fatalf("out=%q originals=%v rep=%+v err=%v", out, vault.originals, report, err)
	}
}

type classifierContextKey struct{}

type classifierFunc func(context.Context, string) (ClassifierResult, error)

func (f classifierFunc) Classify(ctx context.Context, text string) (ClassifierResult, error) {
	return f(ctx, text)
}

func TestClassifierContextFaultAndNoLateDelivery(t *testing.T) {
	for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, violation := range []bool{false, true} {
			// Arrange.
			validator := NewMLValidator(classifierFunc(func(context.Context, string) (ClassifierResult, error) {
				return ClassifierResult{IsViolation: violation, Score: score}, nil
			}))
			pipeline := guardy.NewPipeline(guardy.WithFastPath(validator))
			// Act.
			result, err := pipeline.GuardOutput(context.Background(), nil, "payload")
			// Assert.
			_, available := result.DeliverableValue()
			if err == nil || !result.Decision.IsSystemFault() || available {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		}
	}
	// Arrange: callback receives original caller context, cancels it, then returns late success.
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), classifierContextKey{}, "caller"))
	defer cancel()
	called := false
	validator := NewMLValidator(classifierFunc(func(got context.Context, _ string) (ClassifierResult, error) {
		if got.Value(classifierContextKey{}) != "caller" {
			t.Fatal("caller context values lost")
		}
		called = true
		cancel()
		return ClassifierResult{Score: 1}, nil
	}))
	pipeline := guardy.NewPipeline(guardy.WithFastPath(validator))
	// Act.
	result, err := pipeline.GuardOutput(ctx, nil, "payload")
	// Assert.
	_, available := result.DeliverableValue()
	if !called || !errors.Is(err, context.Canceled) || available || !result.Decision.IsSystemFault() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	called = false
	_, _, err = validator.Validate(ctx, "payload")
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("pre-cancelled callback called=%v err=%v", called, err)
	}
}

func TestRegexReplacementIsLiteral(t *testing.T) {
	// Arrange.
	validator := MustRegexValidator(`(secret)`, WithAction(guardy.ActionRedact), WithRedactionReplacement(`$1\literal`))
	// Act.
	out, report, err := validator.Validate(context.Background(), "secret")
	// Assert.
	if err != nil || report.Action != guardy.ActionRedact || out != `$1\literal` {
		t.Fatalf("%q %+v %v", out, report, err)
	}
}

func TestPIIOverlappingCandidatesAfterRejectedBoundary(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"x4111 4111 1111 1111 1111", "x4111 [REDACTED]"},
		{"64111 4111 1111 1111 1111", "64111 [REDACTED]"},
		{"4111 4111 1111 1111 1111", "[REDACTED]"},
	} {
		// Arrange / Act.
		out, report, err := NewPIIValidator().Validate(context.Background(), tc.raw)
		// Assert: a rejected earlier candidate must not hide later qualified spans.
		if err != nil || report.Action != guardy.ActionRedact || out != tc.want {
			t.Fatalf("%q: %q %+v %v", tc.raw, out, report, err)
		}
	}
}

func TestUnsupportedPIIFormatsHaveNoWholeValueGuarantee(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`"alice"@example.com`, `"alice"@example.com`},
		{`alice@[127.0.0.1]`, `alice@[127.0.0.1]`},
		{"4111 1111 11111", "4111 1111 11111"},
		{"4111-1111 1111-1111", "4111-1111 1111-1111"},
		{"4111 1111 1111 1111 111", "[REDACTED] 111"},
		{"555-123-4567 ext 42", "[REDACTED] ext 42"},
		{"+49 5551234567", "+49 [REDACTED]"},
	} {
		// Arrange / Act.
		out, _, err := NewPIIValidator().Validate(context.Background(), tc.raw)
		// Assert: supported substrings may match inside unsupported overall formats.
		if err != nil || out != tc.want {
			t.Fatalf("%q: %q %v", tc.raw, out, err)
		}
	}
}

func TestClassifierErrorIsPipelineFault(t *testing.T) {
	// Arrange.
	want := errors.New("detector unavailable")
	validator := NewMLValidator(
		classifierFunc(func(context.Context, string) (ClassifierResult, error) { return ClassifierResult{}, want }),
	)
	pipeline := guardy.NewPipeline(guardy.WithFastPath(validator))
	// Act.
	result, err := pipeline.GuardOutput(context.Background(), nil, "payload")
	// Assert.
	_, available := result.DeliverableValue()
	if !errors.Is(err, want) || !result.Decision.IsSystemFault() || available {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPIIOverlappingEmailsMergeWholeOriginalSpans(t *testing.T) {
	// Arrange.
	validator := NewPIIValidator()
	// Act.
	out, report, err := validator.Validate(context.Background(), "a@b.com.cc@d.org")
	// Assert.
	if err != nil || report.Action != guardy.ActionRedact || out != "[REDACTED]" {
		t.Fatalf("%q %+v %v", out, report, err)
	}
}

func TestEmailSupportedPrefixesBeforeUnsupportedSuffixes(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"alice@example.com-next", "[REDACTED]-next"},
		{"alice@example.com-42", "[REDACTED]-42"},
		{"a@b.com.42", "[REDACTED].42"},
		{"alice@example.com-next.org", "[REDACTED]"},
		{"alice@example.comé", "alice@example.comé"},
	} {
		// Arrange / Act.
		out, _, err := NewPIIValidator().Validate(context.Background(), tc.raw)
		// Assert.
		if err != nil || out != tc.want {
			t.Fatalf("%q: %q %v", tc.raw, out, err)
		}
	}
}
