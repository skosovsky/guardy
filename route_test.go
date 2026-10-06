package guardy

import (
	"context"
	"errors"
	"testing"
)

func TestDecisionRoute_RetryCorrection(t *testing.T) {
	t.Parallel()
	// Arrange.
	decision := DecisionFromReport(FinishReport(&Report{
		Action:   ActionRetry,
		Code:     "FIX_INPUT",
		Feedback: "rewrite input",
	}, ControlSpec{Action: ActionRetry}))

	// Act.
	route, err := decision.Route(RemediationPolicy{RetryAttempt: 1, MaxRetries: 3})

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if route.Outcome != GuardRouteRetryCorrection {
		t.Fatalf("Outcome = %q", route.Outcome)
	}
	if !route.Retryable || route.Terminal || route.RetryFeedback != "rewrite input" {
		t.Fatalf("route = %+v", route)
	}
}

func TestDecisionRoute_RetryExhaustedUsesFallback(t *testing.T) {
	t.Parallel()
	// Arrange.
	decision := DecisionFromReport(FinishReport(&Report{
		Action:          ActionRetry,
		Code:            "FIX_INPUT",
		SafeUserMessage: "try again later",
	}, ControlSpec{Action: ActionRetry}))

	// Act.
	route, err := decision.Route(RemediationPolicy{
		RetryAttempt:    3,
		MaxRetries:      3,
		AllowFallback:   true,
		FallbackMessage: "safe fallback",
	})

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if route.Outcome != GuardRouteFallbackDelivery {
		t.Fatalf("Outcome = %q", route.Outcome)
	}
	if !route.RetryExhausted || !route.Fallback || route.Retryable {
		t.Fatalf("route = %+v", route)
	}
	if route.SafeMessage != "safe fallback" {
		t.Fatalf("SafeMessage = %q", route.SafeMessage)
	}
}

func TestDecisionRoute_TerminalDeny(t *testing.T) {
	t.Parallel()
	// Arrange.
	decision := DecisionFromReport(FinishReport(&Report{
		Action: ActionBlock,
		Code:   "DENIED",
	}, ControlSpec{Action: ActionBlock}))

	// Act.
	route, err := RouteDecision(decision, RemediationPolicy{})

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if route.Outcome != GuardRouteTerminalDeny || !route.Terminal {
		t.Fatalf("route = %+v", route)
	}
}

func TestDecisionRoute_SystemFault(t *testing.T) {
	t.Parallel()
	// Arrange.
	decision := Decision{
		Disposition: DispositionSystemFault,
		Code:        CodeValidatorFailed,
	}

	// Act.
	route, err := decision.Route(RemediationPolicy{})

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if route.Outcome != GuardRouteSystemFault || !route.SystemFault || !route.Terminal {
		t.Fatalf("route = %+v", route)
	}
}

func TestRouteRejectsNegativeCountersForEveryDecision(t *testing.T) {
	for _, decision := range []Decision{{Action: ActionPass}, {Action: ActionRedact}, {Action: ActionBlock},
		{Action: ActionRetry, Disposition: DispositionRetryableCorrection}, {Disposition: DispositionSystemFault}} {
		for _, policy := range []RemediationPolicy{{RetryAttempt: -1, MaxRetries: 3}, {MaxRetries: -1}, {RetryAttempt: -1, MaxRetries: -1, AllowFallback: true}} {
			// Arrange/Act.
			route, err := decision.Route(policy)
			// Assert.
			if !errors.Is(err, ErrConfiguration) || route != (GuardRoute{}) {
				t.Fatalf("invalid counters admitted: %+v %v", route, err)
			}
		}
	}
}

func TestRouteStatelessFallbackProposalRequiresCheckedDelivery(t *testing.T) {
	// Arrange.
	decision := Decision{Action: ActionRetry, Disposition: DispositionRetryableCorrection}
	policy := RemediationPolicy{MaxRetries: 0, AllowFallback: true, FallbackMessage: `{"private":true}`}
	pipeline := MustNewPipeline[string]()
	// Act.
	first, err := decision.Route(policy)
	again, repeatErr := decision.Route(policy)
	guarded, deliveryErr := pipeline.GuardOutput(context.Background(), nil, first.SafeMessage)
	// Assert.
	if err != nil || repeatErr != nil || first != again || first.Outcome != GuardRouteFallbackDelivery ||
		!first.RetryExhausted {
		t.Fatalf("route not stateless/zero-budget fallback: %+v %v", first, err)
	}
	if deliveryErr == nil || guarded.Deliverable {
		t.Fatal("fallback route must not authorize technical fallback delivery")
	}
	fault, err := (Decision{Disposition: DispositionSystemFault}).Route(policy)
	if err != nil || fault.Fallback || fault.Outcome != GuardRouteSystemFault {
		t.Fatal("system fault suggested fallback")
	}
}
