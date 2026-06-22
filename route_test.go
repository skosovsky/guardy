package guardy

import "testing"

func TestDecisionRoute_RetryCorrection(t *testing.T) {
	t.Parallel()
	// Arrange.
	decision := DecisionFromReport(FinishReport(&Report{
		Action:   ActionRetry,
		Code:     "FIX_INPUT",
		Feedback: "rewrite input",
	}, ControlSpec{Action: ActionRetry}))

	// Act.
	route := decision.Route(RemediationPolicy{RetryAttempt: 1, MaxRetries: 3})

	// Assert.
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
	route := decision.Route(RemediationPolicy{
		RetryAttempt:    3,
		MaxRetries:      3,
		AllowFallback:   true,
		FallbackMessage: "safe fallback",
	})

	// Assert.
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
	route := RouteDecision(decision, RemediationPolicy{})

	// Assert.
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
		SystemFault: true,
	}

	// Act.
	route := decision.Route(RemediationPolicy{})

	// Assert.
	if route.Outcome != GuardRouteSystemFault || !route.SystemFault || !route.Terminal {
		t.Fatalf("route = %+v", route)
	}
}
