package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestBuiltInCleanPassHasNoViolationFlags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		make func(...Option) guardy.Validator[string]
	}{
		{"regex", func(opts ...Option) guardy.Validator[string] { return MustRegexValidator("secret", opts...) }},
		{"length", func(opts ...Option) guardy.Validator[string] { return MustLengthValidator(0, 10, opts...) }},
		{"wordlist", func(opts ...Option) guardy.Validator[string] {
			return MustWordlistValidator([]string{"secret"}, Blocklist, opts...)
		}},
		{"pii", MustPIIValidator},
		{"classifier", func(opts ...Option) guardy.Validator[string] {
			return MustClassifierValidator(classifierFunc(func(context.Context, string) (ClassifierResult, error) {
				return ClassifierResult{}, nil
			}), opts...)
		}},
		{"tag", func(opts ...Option) guardy.Validator[string] { return MustTagPatternValidator("", opts...) }},
		{"technical_json", MustTechnicalJSONClassifier},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			validator := tc.make(WithFatal(true), WithRetryable(true), WithSafeUserMessage("deny notice"))
			// Act.
			output, report, err := validator.Validate(context.Background(), "hello")
			decision := guardy.DecisionFromReport(report)
			// Assert.
			if err != nil || output != "hello" || report.Fatal || report.Retryable || report.SafeUserMessage != "" ||
				decision.Disposition != guardy.DispositionNone {
				t.Fatalf("clean output=%q report=%+v decision=%+v error=%v", output, report, decision, err)
			}
		})
	}
}
