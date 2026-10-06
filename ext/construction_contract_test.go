package ext

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/guardy"
)

type builtInCase struct {
	name  string
	build func(...Option) (guardy.Validator[string], error)
	hit   string
}

func builtInCases() []builtInCase {
	return []builtInCase{
		{
			"regex",
			func(opts ...Option) (guardy.Validator[string], error) { return NewRegexValidator("secret", opts...) },
			"secret",
		},
		{
			"length",
			func(opts ...Option) (guardy.Validator[string], error) { return NewLengthValidator(0, 10, opts...) },
			"12345678901",
		},
		{"wordlist", func(opts ...Option) (guardy.Validator[string], error) {
			return NewWordlistValidator([]string{"secret"}, Blocklist, opts...)
		}, "secret"},
		{"pii", NewPIIValidator, "user@example.com"},
		{"classifier", func(opts ...Option) (guardy.Validator[string], error) {
			return NewClassifierValidator(
				classifierFunc(func(_ context.Context, text string) (ClassifierResult, error) {
					return ClassifierResult{IsViolation: text != "hello", Score: 7}, nil
				}),
				opts...)
		}, "violation"},
		{
			"tag_pattern",
			func(opts ...Option) (guardy.Validator[string], error) { return NewTagPatternValidator("", opts...) },
			"<system>",
		},
		{"technical_json", NewTechnicalJSONClassifier, `{"tool":"run"}`},
	}
}

func TestBuiltInViolationOptionsOnlyApplyToHits(t *testing.T) {
	t.Parallel()
	for _, tc := range builtInCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, flags := range []struct {
				name         string
				opts         []Option
				fatal, retry bool
				message      string
			}{
				{"fatal", []Option{WithFatal(true)}, true, false, ""},
				{"retry", []Option{WithRetryable(true)}, false, true, ""},
				{"notice", []Option{WithSafeUserMessage("notice")}, false, false, "notice"},
				{"all", []Option{WithFatal(true), WithRetryable(true), WithSafeUserMessage("notice")}, true, true, "notice"},
			} {
				t.Run(flags.name, func(t *testing.T) {
					t.Parallel()
					// Arrange.
					validator, err := tc.build(flags.opts...)
					if err != nil {
						t.Fatal(err)
					}
					// Act.
					_, clean, cleanErr := validator.Validate(t.Context(), "hello")
					_, hit, hitErr := validator.Validate(t.Context(), tc.hit)
					// Assert.
					if cleanErr != nil || hitErr != nil || clean.Fatal || clean.Retryable ||
						clean.SafeUserMessage != "" ||
						guardy.DecisionFromReport(clean).Disposition != guardy.DispositionNone {
						t.Fatalf("clean=%+v errors=%v %v", clean, cleanErr, hitErr)
					}
					if hit.Fatal != flags.fatal || hit.Retryable != flags.retry ||
						hit.SafeUserMessage != flags.message {
						t.Fatalf("hit=%+v", hit)
					}
					if flags.fatal {
						assertFatalHitDelivery(t, validator, tc.hit, hit)
					}
				})
			}
		})
	}
	// Arrange / Act: caller-authored fatal-pass retains its canonical semantics.
	decision := guardy.DecisionFromReport(&guardy.Report{Action: guardy.ActionPass, Fatal: true})
	// Assert.
	if !decision.IsTerminal() {
		t.Fatalf("manual fatal-pass=%+v", decision)
	}
}

func TestBuiltInConfigurationFailsBeforeValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range builtInCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, opts := range [][]Option{{nil}, {WithAction(guardy.Action(255))}, {WithTokenVault(nil)}} {
				// Arrange / Act.
				validator, err := tc.build(opts...)
				// Assert: a failed setup has no instance to invoke.
				var config *guardy.ConfigurationError
				if validator != nil || !errors.Is(err, guardy.ErrConfiguration) || !errors.As(err, &config) {
					t.Fatalf("validator=%v error=%v", validator, err)
				}
			}
		})
	}
}

func TestOptionsRejectUnsupportedCapabilities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		build func() (guardy.Validator[string], error)
	}{
		{"regex_lowercase_false", func() (guardy.Validator[string], error) { return NewRegexValidator("x", WithLowercase(false)) }},
		{"length_redact", func() (guardy.Validator[string], error) {
			return NewLengthValidator(0, 10, WithAction(guardy.ActionRedact))
		}},
		{"regex_vault", func() (guardy.Validator[string], error) {
			return NewRegexValidator("x", WithAction(guardy.ActionRedact), WithTokenVault(NewInMemoryTokenVault()))
		}},
		{"wordlist_block_replacement", func() (guardy.Validator[string], error) {
			return NewWordlistValidator([]string{"x"}, Blocklist, WithRedactionReplacement("X"))
		}},
		{"technical_action", func() (guardy.Validator[string], error) {
			return NewTechnicalJSONClassifier(WithAction(guardy.ActionBlock))
		}},
		{"pii_vault_replacement", func() (guardy.Validator[string], error) {
			return NewPIIValidator(WithTokenVault(NewInMemoryTokenVault()), WithRedactionReplacement("X"))
		}},
		{"wordlist_vault_replacement", func() (guardy.Validator[string], error) {
			return NewWordlistValidator([]string{"x"}, Blocklist, WithAction(guardy.ActionRedact), WithTokenVault(NewInMemoryTokenVault()), WithRedactionReplacement("X"))
		}},
		{"custom_ignored_field", func() (guardy.Validator[string], error) {
			return NewLengthValidator(0, 10, func(cfg *RuleConfig) { cfg.RedactionReplacement = "X" })
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange / Act.
			validator, err := tc.build()
			// Assert.
			if validator != nil || !errors.Is(err, guardy.ErrConfiguration) {
				t.Fatalf("validator=%v err=%v", validator, err)
			}
		})
	}
}

func TestRegexDetectionDoesNotDependOnMutation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, pattern, replacement, input, output string }{
		{"identity", "secret", "secret", "secret", "secret"},
		{"empty", "secret", "", "secret", ""},
		{"zero_width_identity", "^", "", "hello", "hello"},
		{"zero_width_insert", "^", "X", "hello", "Xhello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			validator := MustRegexValidator(
				tc.pattern,
				WithAction(guardy.ActionRedact),
				WithRedactionReplacement(tc.replacement),
			)
			// Act.
			output, report, err := validator.Validate(t.Context(), tc.input)
			// Assert: a hit remains a detection even if output bytes are identical.
			if err != nil || report.Action != guardy.ActionRedact || output != tc.output ||
				report.MutatedText != output {
				t.Fatalf("output=%q report=%+v error=%v", output, report, err)
			}
		})
	}
}

func TestLengthConstructorAndMustShareBounds(t *testing.T) {
	t.Parallel()
	for _, bounds := range [][2]int{{-1, 10}, {0, -1}, {3, 2}} {
		// Arrange / Act.
		validator, err := NewLengthValidator(bounds[0], bounds[1])
		// Assert.
		if validator != nil || !errors.Is(err, guardy.ErrConfiguration) {
			t.Fatalf("bounds=%v err=%v", bounds, err)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("Must accepted invalid bounds=%v", bounds)
				}
			}()
			MustLengthValidator(bounds[0], bounds[1])
		}()
	}
	for _, bounds := range [][2]int{{0, 0}, {0, 10}, {10, 0}, {10, 10}} {
		// Arrange / Act.
		_, err := NewLengthValidator(bounds[0], bounds[1])
		// Assert.
		if err != nil {
			t.Fatalf("bounds=%v error=%v", bounds, err)
		}
	}
}

func TestNilClassifierRejectedAtConstruction(t *testing.T) {
	t.Parallel()
	// Arrange.
	var typedNil classifierFunc
	// Act.
	validator, err := NewClassifierValidator(typedNil)
	// Assert.
	if validator != nil || !errors.Is(err, guardy.ErrConfiguration) {
		t.Fatalf("validator=%v error=%v", validator, err)
	}
}

func assertFatalHitDelivery(t *testing.T, validator guardy.Validator[string], input string, hit *guardy.Report) {
	t.Helper()
	if !guardy.DecisionFromReport(hit).IsTerminal() {
		t.Fatalf("fatal hit=%+v", hit)
	}
	// Act.
	delivery, fault := guardy.MustNewPipeline(guardy.WithSequential(validator)).GuardOutput(t.Context(), nil, input)
	// Assert.
	if !errors.Is(fault, guardy.ErrBlocked) || delivery.Deliverable || delivery.Value != "" {
		t.Fatalf("fatal delivery=%+v error=%v", delivery, fault)
	}
}
