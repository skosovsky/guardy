package guardy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// runWithoutEscapedPanic lets the regression report a failure instead of aborting
// the test when the old sequential/policy implementation lets a panic escape.
//
//nolint:nonamedreturns // The outer recovery probe records a panic instead of failing the process.
func runWithoutEscapedPanic(
	ctx context.Context,
	p *Pipeline[string],
) (result RunResult[string], escaped any, err error) {
	defer func() { escaped = recover() }()
	result, err = p.Run(ctx, nil, "source")
	return result, escaped, err
}

func TestPipelinePanicContractAllPhases(t *testing.T) {
	for _, phase := range []string{"sequential", "policy", "parallel", "middleware"} {
		t.Run(phase, func(t *testing.T) {
			// Arrange.
			cause := errors.New("private panic payload")
			prior := &fakeValidator{validate: func(_ context.Context, text string) (string, *Report, error) {
				return text, &Report{Action: ActionPass, PayloadKind: PayloadTechnicalPayload}, nil
			}}
			broken := &fakeValidator{validate: func(context.Context, string) (string, *Report, error) {
				panic(cause)
			}}
			opts := []PipelineOption[string]{WithSequential[string](prior)}
			switch phase {
			case "sequential", "middleware":
				opts = append(opts, WithSequential[string](broken))
			case "policy":
				opts = append(opts, WithPolicyValidators(MustPolicyFuncWithScope[string](nil,
					func(ctx context.Context, text string, _ ExecutionScope) (string, *Report, error) {
						return broken.Validate(ctx, text)
					})))
			case "parallel":
				opts = append(opts, WithParallel[string](broken))
			}
			p := MustNewPipeline(opts...)
			if phase == "middleware" {
				p = p.MustUse(func(next Validator[string]) Validator[string] {
					return &fakeValidator{
						validate: func(ctx context.Context, text string) (string, *Report, error) { return next.Validate(ctx, text) },
					}
				})
			}

			// Act.
			result, escaped, err := runWithoutEscapedPanic(context.Background(), p)

			// Assert.
			if escaped != nil {
				t.Fatalf("Validate panic escaped: %T", escaped)
			}
			if !errors.Is(err, ErrValidatorFailed) || !errors.Is(err, cause) {
				t.Fatalf("fault category/original error cause missing: %v", err)
			}
			if !result.PolicyDecision().IsSystemFault() || result.OutputKind != PayloadTechnicalPayload {
				t.Fatalf("fault or completed kind lost: %+v", result)
			}
			if strings.Contains(err.Error(), cause.Error()) {
				t.Fatal("public error exposes panic value")
			}
			for _, report := range result.Reports {
				if strings.Contains(report.Reason, cause.Error()) {
					t.Fatal("fault report formats panic value")
				}
			}
		})
	}
}

func TestPipelinePanicAfterCancelRemainsIndependentFault(t *testing.T) {
	// Arrange.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("private independent failure")
	rule := &fakeValidator{validate: func(context.Context, string) (string, *Report, error) {
		cancel()
		panic(cause)
	}}
	p := MustNewPipeline(WithSequential(rule))

	// Act.
	result, escaped, err := runWithoutEscapedPanic(ctx, p)

	// Assert.
	if escaped != nil {
		t.Fatal("Validate panic escaped")
	}
	if !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || !result.PolicyDecision().IsSystemFault() {
		t.Fatalf("panic/cancel fault causes lost: %v", err)
	}
}

// Formatting this value would invoke another panic; recovery must store it only.
type unformattablePanic struct{}

func (*unformattablePanic) String() string { panic("panic value was formatted") }

func TestPipelinePanicValueAndBoundarySuppression(t *testing.T) {
	for _, value := range []any{nil, "private text", &unformattablePanic{}} {
		t.Run("value", func(t *testing.T) {
			// Arrange.
			rule := &fakeValidator{validate: func(context.Context, string) (string, *Report, error) { panic(value) }}
			p := MustNewPipeline(WithSequential(rule))

			// Act.
			guarded, err := p.GuardOutput(context.Background(), nil, "secret")

			// Assert.
			var panicErr *ValidatorPanicError
			var failure *PolicyFailure
			if !errors.As(err, &panicErr) || !errors.As(err, &failure) || !failure.Decision.IsSystemFault() {
				t.Fatalf("typed panic and policy failure missing: %v", err)
			}
			if value != nil && panicErr.PanicValue() != value {
				t.Fatal("explicit panic value inspection lost original value")
			}
			// Modern Go replaces panic(nil) with runtime.PanicNilError; both representations
			// are accepted here, since the invariant is a system fault in either mode.
			if got, ok := guarded.DeliverableValue(); ok || got != "" || guarded.Value != "" || guarded.Fallback {
				t.Fatal("panic produced deliverable or fallback content")
			}
			if err.Error() != ErrValidatorFailed.Error() {
				t.Fatal("panic changed safe public error")
			}
		})
	}
}

func TestPipelineObserverPanicOutsideValidateRecovery(t *testing.T) {
	// Arrange.
	marker := &unformattablePanic{}
	rule := &fakeValidator{validate: func(_ context.Context, text string) (string, *Report, error) {
		return text, &Report{Action: ActionBlock, ShadowMode: true}, nil
	}}
	p := MustNewPipeline(
		WithSequential(rule),
		WithObserver[string](func(context.Context, GuardEvent) { panic(marker) }),
	)

	// Act.
	_, escaped, _ := runWithoutEscapedPanic(context.Background(), p)

	// Assert.
	if escaped != marker {
		t.Fatal("observer panic was incorrectly treated as Validate fault")
	}
}

func TestPipelineMiddlewareConstructionPanicRemainsExplicit(t *testing.T) {
	// Arrange.
	p := MustNewPipeline(WithSequential(&fakeValidator{}))
	marker := &unformattablePanic{}
	var escaped any

	// Act.
	func() {
		defer func() { escaped = recover() }()
		p.MustUse(func(Validator[string]) Validator[string] { panic(marker) })
	}()

	// Assert.
	if escaped != marker {
		t.Fatal("middleware construction panic was incorrectly recovered")
	}
}

func TestPipelinePhaseLabelsMatchExecutionContract(t *testing.T) {
	// Arrange.
	var seen []string
	rule := &fakeValidator{validate: func(ctx context.Context, text string) (string, *Report, error) {
		phase, ok := ValidationPhaseFromContext(ctx)
		if !ok {
			t.Error("phase context missing")
		}
		seen = append(seen, string(phase))
		return text, nil, nil
	}}
	p := MustNewPipeline(WithSequential(rule), WithPolicyValidators(MustPolicyFuncWithScope[string](nil,
		func(ctx context.Context, text string, _ ExecutionScope) (string, *Report, error) {
			return rule.Validate(ctx, text)
		})), WithParallel(rule))

	// Act.
	_, err := p.Run(context.Background(), nil, "source")

	// Assert.
	if err != nil || strings.Join(seen, ",") != "sequential,policy,parallel" {
		t.Fatalf("phase labels = %v, err = %v", seen, err)
	}
}

func TestParallelPanicCancellationIsNotSiblingCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, fmt.Errorf("wrapped: %w", context.Canceled),
		WithCompletedObservations(context.Canceled, &Report{Action: ActionPass, PayloadKind: PayloadTechnicalPayload})} {
		t.Run("cause", func(t *testing.T) {
			// Arrange: the panic deterministically follows cancellation by a sibling deny.
			deny := ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
				return value, &Report{Action: ActionBlock}, nil
			})
			broken := ValidatorFunc[string](func(ctx context.Context, _ string) (string, *Report, error) {
				<-ctx.Done()
				panic(cause)
			})
			p := MustNewPipeline(WithParallel(deny, broken))
			// Act.
			result, err := p.Run(context.Background(), nil, "secret")
			guarded, boundaryErr := p.GuardOutput(context.Background(), nil, "secret")
			// Assert.
			var panicErr *ValidatorPanicError
			if !result.PolicyDecision().IsSystemFault() || !errors.As(err, &panicErr) ||
				!errors.Is(err, context.Canceled) {
				t.Fatal("independent panic was suppressed as cooperative sibling cancellation")
			}
			if boundaryErr == nil || !guarded.Decision.IsSystemFault() || guarded.Deliverable || guarded.Value != "" {
				t.Fatal("panic boundary allowed/denied as policy instead of fault")
			}
		})
	}
}
