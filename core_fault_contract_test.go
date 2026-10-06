package guardy

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestInvalidReportsFailClosedInEveryPhaseAndBoundary(t *testing.T) {
	for name, report := range map[string]Report{
		"action":       {Action: Action(255), ShadowMode: true},
		"disposition":  {Disposition: FailureDisposition(255), ShadowMode: true},
		"retry-pass":   {Action: ActionPass, Disposition: DispositionRetryableCorrection},
		"retry-block":  {Action: ActionBlock, Disposition: DispositionRetryableCorrection, ShadowMode: true},
		"kind":         {PayloadKind: PayloadKind(255)},
		"nan":          {Score: math.NaN()},
		"infinity":     {Score: math.Inf(1)},
		"shadow-fault": {Action: ActionBlock, Disposition: DispositionSystemFault, ShadowMode: true},
	} {
		for _, phase := range []string{"sequential", "policy", "parallel"} {
			t.Run(name+"/"+phase, func(t *testing.T) {
				// Arrange.
				rule := ValidatorFunc[string](
					func(_ context.Context, value string) (string, *Report, error) { return value, report.Clone(), nil },
				)
				option := WithSequential[string](rule)
				if phase == "parallel" {
					option = WithParallel[string](rule)
				}
				if phase == "policy" {
					option = WithPolicyValidators(MustPolicyFuncWithScope[string](nil,
						func(ctx context.Context, value string, _ ExecutionScope) (string, *Report, error) {
							return rule.Validate(ctx, value)
						}))
				}
				pipeline := MustNewPipeline(option)
				// Act.
				result, err := pipeline.Run(context.Background(), nil, "source")
				// Assert: these are completed report-only faults with no infrastructure error.
				if err != nil || !result.PolicyDecision().IsSystemFault() {
					t.Fatalf("report-only fault lost: %v %+v", err, result)
				}
				checkAllEnforcementBoundaries(t, pipeline, DispositionSystemFault)
			})
		}
	}
}

func TestRunFaultChannelsRetainCauseAndSuppressDelivery(t *testing.T) {
	for _, phase := range []string{"sequential", "policy", "parallel"} {
		t.Run(phase, func(t *testing.T) {
			// Arrange.
			cause := errors.New("private processing detail")
			rule := ValidatorFunc[string](func(context.Context, string) (string, *Report, error) {
				return "failed transformation", &Report{Action: ActionPass, PayloadKind: PayloadTechnicalPayload}, cause
			})
			option := WithSequential[string](rule)
			if phase == "parallel" {
				option = WithParallel[string](rule)
			}
			if phase == "policy" {
				option = WithPolicyValidators(MustPolicyFuncWithScope[string](nil,
					func(ctx context.Context, value string, _ ExecutionScope) (string, *Report, error) {
						return rule.Validate(ctx, value)
					}))
			}
			pipeline := MustNewPipeline(option)
			// Act.
			result, err := pipeline.Run(context.Background(), nil, "source")
			guarded, boundaryErr := pipeline.GuardOutput(context.Background(), nil, "source")
			// Assert: callback errors are independent of report-only fault semantics.
			var failure *PolicyFailure
			if !errors.Is(err, cause) || !errors.Is(err, ErrValidatorFailed) ||
				!result.PolicyDecision().IsSystemFault() ||
				result.OutputKind != PayloadSafeUserText {
				t.Fatalf("failed callback result trusted or cause lost: %v %+v", err, result)
			}
			if !errors.As(boundaryErr, &failure) || !errors.Is(boundaryErr, cause) || guarded.Deliverable ||
				guarded.Value != "" ||
				guarded.Fallback {
				t.Fatal("Go-error boundary did not preserve cause/suppress delivery")
			}
			checkAllEnforcementBoundaries(t, pipeline, DispositionSystemFault)
		})
	}
}
