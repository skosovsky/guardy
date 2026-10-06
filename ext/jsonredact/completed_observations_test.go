package jsonredact

import (
	"context"
	"errors"
	"fmt"
	"testing"

	g "github.com/skosovsky/guardy"
)

func TestJSONCompletedClassificationSurvivesFault(t *testing.T) {
	t.Parallel()
	for _, kind := range []g.PayloadKind{g.PayloadTechnicalPayload, g.PayloadInternalControlSignal} {
		for _, wrappedCancel := range []bool{false, true} {
			t.Run(fmt.Sprint(kind, wrappedCancel), func(t *testing.T) {
				t.Parallel()
				assertJSONCompletedFault(t, kind, wrappedCancel)
			})
		}
	}
}

func TestJSONPreservesConcurrentErrorAndCancellation(t *testing.T) {
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
	adapter := NewJSONRedactValidator(leaf, "")
	input := `["first","second"]`
	// Act.
	output, report, err := adapter.Validate(ctx, input)
	// Assert.
	if !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || output != input ||
		report.PayloadKind != g.PayloadInternalControlSignal {
		t.Fatalf("output=%v report=%+v error=%v", output, report, err)
	}
}

func assertJSONCompletedFault(t *testing.T, kind g.PayloadKind, wrappedCancel bool) {
	t.Helper()
	// Arrange: array order makes the completed observation independent of map ordering (T07).
	cause := errors.New("leaf fault")
	if wrappedCancel {
		cause = fmt.Errorf("child: %w", context.Canceled)
	}
	leaf := g.ValidatorFunc[string](func(_ context.Context, input string) (string, *g.Report, error) {
		if input == "first" {
			return "redacted", &g.Report{Action: g.ActionRedact, PayloadKind: kind}, nil
		}
		return "failed output", &g.Report{
			Action:      g.ActionRedact,
			PayloadKind: g.PayloadInternalControlSignal,
			Validator:   "failed",
		}, cause
	})
	adapter := NewJSONRedactValidator(leaf, "")
	input := `["first",{"second":"fail"}]`
	pipeline := g.NewPipeline(g.WithFastPath[string](adapter))
	policy := g.NewUserTextPolicy(
		"internal",
		g.WithDeliveryAllowedKinds(g.PayloadTechnicalPayload, g.PayloadInternalControlSignal),
	)
	// Act.
	output, report, directErr := adapter.Validate(t.Context(), input)
	result, runErr := pipeline.Run(t.Context(), nil, input)
	delivery, boundaryErr := pipeline.GuardDelivery(t.Context(), nil, policy, input)
	// Assert.
	var failure *g.PolicyFailure
	if output != input || !errors.Is(directErr, cause) ||
		g.DecisionFromReport(report).PayloadKind != kind ||
		report.MutatedText != "" {
		t.Fatalf("direct=%q report=%+v error=%v", output, report, directErr)
	}
	if !errors.Is(runErr, cause) || result.OutputKind != kind || !result.PolicyDecision().IsSystemFault() ||
		result.PolicyDecision().PayloadKind != kind {
		t.Fatalf("result=%+v error=%v", result, runErr)
	}
	if !errors.Is(boundaryErr, cause) || !errors.As(boundaryErr, &failure) {
		t.Fatalf("boundary error=%v", boundaryErr)
	}
	if failure.Decision.PayloadKind != kind || !failure.Decision.IsSystemFault() {
		t.Fatalf("failure=%+v", failure)
	}
	if delivery.Deliverable || delivery.Value != "" || delivery.Kind != kind ||
		delivery.Decision.PayloadKind != kind {
		t.Fatalf("delivery=%+v error=%v", delivery, boundaryErr)
	}
}
