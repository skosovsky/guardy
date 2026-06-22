package guardy

// GuardRouteOutcome is the machine-readable remediation outcome for a decision.
type GuardRouteOutcome string

// Known remediation outcomes.
const (
	GuardRouteAllow            GuardRouteOutcome = "allow"
	GuardRouteRetryCorrection  GuardRouteOutcome = "retry_correction"
	GuardRouteTerminalDeny     GuardRouteOutcome = "terminal_deny"
	GuardRouteSystemFault      GuardRouteOutcome = "system_fault"
	GuardRouteFallbackDelivery GuardRouteOutcome = "fallback_delivery"
)

// RemediationPolicy supplies host-owned routing limits without making guardy
// own the workflow that executes the route.
type RemediationPolicy struct {
	RetryAttempt    int
	MaxRetries      int
	AllowFallback   bool
	FallbackMessage string
}

// GuardRoute is guardy's routing projection for a canonical decision.
type GuardRoute struct {
	Outcome        GuardRouteOutcome
	Retryable      bool
	Terminal       bool
	SystemFault    bool
	Fallback       bool
	RetryExhausted bool
	SafeMessage    string
	RetryFeedback  string
	Code           string
	PayloadKind    PayloadKind
}

// Route projects the decision into a host routing outcome.
func (d Decision) Route(policy RemediationPolicy) GuardRoute {
	return RouteDecision(d, policy)
}

// RouteDecision projects a canonical decision into a machine-readable route.
func RouteDecision(decision Decision, policy RemediationPolicy) GuardRoute {
	route := GuardRoute{
		Outcome:        GuardRouteAllow,
		Retryable:      false,
		Terminal:       false,
		SystemFault:    false,
		Fallback:       false,
		RetryExhausted: false,
		SafeMessage:    decision.SafeMessage,
		RetryFeedback:  decision.RetryFeedback,
		Code:           decision.Code,
		PayloadKind:    decision.PayloadKind,
	}
	switch {
	case decision.IsSystemFault():
		route.Outcome = GuardRouteSystemFault
		route.SystemFault = true
		route.Terminal = true
	case decision.IsRetryable():
		route = routeRetryCorrection(route, policy)
	case decision.IsTerminal():
		route.Outcome = GuardRouteTerminalDeny
		route.Terminal = true
	case decision.Action == ActionBlock:
		route.Outcome = GuardRouteTerminalDeny
		route.Terminal = true
	default:
		route.Outcome = GuardRouteAllow
	}
	return route
}

func routeRetryCorrection(route GuardRoute, policy RemediationPolicy) GuardRoute {
	route.Outcome = GuardRouteRetryCorrection
	route.Retryable = true
	if policy.MaxRetries > 0 && policy.RetryAttempt >= policy.MaxRetries {
		route.RetryExhausted = true
		route.Retryable = false
		if policy.AllowFallback {
			route.Outcome = GuardRouteFallbackDelivery
			route.Fallback = true
			route.SafeMessage = firstNonEmpty(policy.FallbackMessage, route.SafeMessage)
			return route
		}
		route.Outcome = GuardRouteTerminalDeny
		route.Terminal = true
	}
	return route
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
