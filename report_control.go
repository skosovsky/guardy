package guardy

// ControlSpec supplies optional overrides for Report control-flow flags.
type ControlSpec struct {
	Action          Action
	Retryable       *bool
	Fatal           bool
	SafeUserMessage string
}

// ApplyControlDefaults sets Retryable, Fatal, and SafeUserMessage on rep.
// When Retryable is nil, ActionRetry yields Retryable true; other actions false.
func ApplyControlDefaults(rep *Report, spec ControlSpec) {
	if rep == nil {
		return
	}
	if spec.Retryable != nil {
		rep.Retryable = *spec.Retryable
	} else {
		rep.Retryable = spec.Action == ActionRetry
	}
	rep.Fatal = spec.Fatal
	if spec.SafeUserMessage != "" {
		rep.SafeUserMessage = spec.SafeUserMessage
	}
}

// ShouldRetry reports whether the caller should attempt a retry (e.g. LLM correction).
func (r *Report) ShouldRetry() bool {
	if r == nil {
		return false
	}
	return r.Retryable && r.Action == ActionRetry
}

// ShouldStop reports whether the upstream pipeline or request must halt.
// Fatal is an alias for hard escalation in the spec (stop entire upstream flow).
func (r *Report) ShouldStop() bool {
	if r == nil {
		return false
	}
	return r.Fatal || r.Action == ActionBlock
}

// PublicMessage returns text safe for external APIs and end users.
// It never exposes Feedback (use [Report.OrchestratorMessage] for LLM/orchestrator hints).
func (r *Report) PublicMessage() string {
	if r == nil {
		return ""
	}
	if r.SafeUserMessage != "" {
		return r.SafeUserMessage
	}
	return "validation failed"
}

// OrchestratorMessage returns detailed guidance for LLM retry or internal orchestration.
func (r *Report) OrchestratorMessage() string {
	if r == nil {
		return ""
	}
	if r.Action == ActionRetry && r.Feedback != "" {
		return r.Feedback
	}
	if r.Reason != "" {
		return r.Reason
	}
	return ""
}
