package guardy_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	g "github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func TestMapSliceCompletedClassificationSurvivesFault(t *testing.T) {
	t.Parallel()
	for _, kind := range []g.PayloadKind{g.PayloadTechnicalPayload, g.PayloadInternalControlSignal} {
		for _, wrappedCancel := range []bool{false, true} {
			for _, phase := range []string{"fast", "policy", "slow"} {
				t.Run(fmt.Sprintf("%v/%t/%s", kind, wrappedCancel, phase), func(t *testing.T) {
					t.Parallel()
					assertCompletedMapFault(t, kind, wrappedCancel, phase)
				})
			}
		}
	}
}

func assertCompletedMapFault(t *testing.T, kind g.PayloadKind, wrappedCancel bool, phase string) {
	t.Helper()
	// Arrange: only the first leaf completes; the failed leaf's report/output are untrusted.
	cause := errors.New("detector failed")
	if wrappedCancel {
		cause = fmt.Errorf("child: %w", context.Canceled)
	}
	leaf := g.ValidatorFunc[string](func(_ context.Context, input string) (string, *g.Report, error) {
		if input == "first" {
			return "changed", &g.Report{Action: g.ActionRedact, PayloadKind: kind, MutatedText: "changed"}, nil
		}
		return "failed output", &g.Report{
			Action:      g.ActionRedact,
			Validator:   "failed",
			PayloadKind: g.PayloadInternalControlSignal,
			MutatedText: "failed mirror",
		}, cause
	})
	adapter := ext.MapSlice(func(s string) string { return s }, func(_ string, s string) string { return s }, leaf)
	input := []string{"first", "second"}
	var option g.PipelineOption[[]string]
	switch phase {
	case "fast":
		option = g.WithFastPath(adapter)
	case "slow":
		option = g.WithSlowPath(adapter)
	case "policy":
		option = g.WithPolicyValidators(
			g.NewPolicyFuncWithScope(
				nil,
				func(ctx context.Context, input []string, _ g.ExecutionScope) ([]string, *g.Report, error) {
					return adapter.Validate(ctx, input)
				},
			),
		)
	}
	pipeline := g.NewPipeline(option)
	policy := g.NewDeliveryPolicy(
		"internal",
		g.WithDeliveryAllowedKinds(kind),
		g.WithDeliveryClassifier(func(any) (g.PayloadKind, error) { return kind, nil }),
	)
	// Act.
	output, report, directErr := adapter.Validate(t.Context(), input)
	result, runErr := pipeline.Run(t.Context(), nil, input)
	delivery, boundaryErr := pipeline.GuardDelivery(t.Context(), nil, policy, input)
	// Assert.
	if !reflect.DeepEqual(output, input) || !errors.Is(directErr, cause) ||
		g.DecisionFromReport(report).PayloadKind != kind {
		t.Fatalf("direct output=%v report=%+v error=%v", output, report, directErr)
	}
	var failure *g.PolicyFailure
	if !errors.Is(runErr, cause) || result.OutputKind != kind || result.PolicyDecision().PayloadKind != kind ||
		!result.PolicyDecision().IsSystemFault() {
		t.Fatalf("result=%+v error=%v", result, runErr)
	}
	if !errors.Is(boundaryErr, cause) || !errors.As(boundaryErr, &failure) || failure.Decision.PayloadKind != kind ||
		!failure.Decision.IsSystemFault() || delivery.Kind != kind || !delivery.Decision.IsSystemFault() || delivery.Deliverable || delivery.Value != nil {
		t.Fatalf("delivery=%+v failure=%+v error=%v", delivery, failure, boundaryErr)
	}
}

func TestMapSlicePreservesConcurrentErrorAndCancellation(t *testing.T) {
	t.Parallel()
	// Arrange.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cause := errors.New("independent detector failure")
	leaf := g.ValidatorFunc[string](func(_ context.Context, input string) (string, *g.Report, error) {
		if input == "first" {
			return input, &g.Report{Action: g.ActionPass, PayloadKind: g.PayloadInternalControlSignal}, nil
		}
		cancel()
		return "failed", &g.Report{PayloadKind: g.PayloadTechnicalPayload}, cause
	})
	adapter := ext.MapSlice(func(s string) string { return s }, func(_ string, s string) string { return s }, leaf)
	input := []string{"first", "second"}
	// Act.
	output, report, err := adapter.Validate(ctx, input)
	// Assert.
	if !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || !reflect.DeepEqual(output, input) ||
		report.PayloadKind != g.PayloadInternalControlSignal {
		t.Fatalf("output=%v report=%+v error=%v", output, report, err)
	}
}
