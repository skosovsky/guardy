// Package guardy provides a pipeline engine for AI guardrails: validation,
// intervention actions (pass, block, redact, retry), and three-phase execution
// (sequential mutation, scoped policy, then parallel read-only via errgroup).
//
// Boundary contracts: typed [ScopeKey] / [ScopeRequirement],
// canonical [Decision] / [PolicyFailure], [ArgsPipeline] / [GuardedArgs],
// [JSONArgsPipeline] / [GuardedJSONArgs], [GuardedDelivery],
// [DeliveryPolicy], [GuardEvent], and [GuardRoute].
//
// See CONTRACTS.md for boundary and release invariants.
package guardy

// Canonical action names for [Action.String], logging, and telemetry.
const (
	actionStringPass    = "pass"
	actionStringBlock   = "block"
	actionStringRedact  = "redact"
	actionStringRetry   = "retry"
	actionStringUnknown = "unknown"
)

// Action is the intervention outcome from a validator or pipeline.
type Action int

// String returns the canonical name for the action.
func (a Action) String() string {
	switch a {
	case ActionPass:
		return actionStringPass
	case ActionBlock:
		return actionStringBlock
	case ActionRedact:
		return actionStringRedact
	case ActionRetry:
		return actionStringRetry
	default:
		return actionStringUnknown
	}
}

// Supported pipeline/validator actions.
const (
	ActionPass   Action = iota // Validation passed
	ActionBlock                // Content should be blocked
	ActionRedact               // Content was redacted (see MutatedText)
	ActionRetry                // Orchestrator should retry with Feedback (e.g. LLM correction)
)

// Severity describes risk/importance of a rule hit.
// It is string-based for interoperability while still supporting typed constants.
type Severity string

// Canonical severity values for built-in validators and telemetry.
const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Report is the single result returned by a validator or the pipeline.
// It maps to security telemetry attributes and control-flow decisions.
// Route control flow with IsTerminalDeny() and IsRetryableCorrection() — not Reason string parsing.
// Action, Code, Retryable, and Fatal remain for telemetry and validator semantics.
// When Action == ActionRetry, Feedback contains the message for the LLM/orchestrator.
type Report struct {
	Action          Action             // ActionPass, ActionBlock, ActionRedact, ActionRetry
	Validator       string             // Name of the validator that produced this report
	Code            string             // Machine-readable rule code (for alerting/telemetry)
	Severity        Severity           // Risk level for the report
	Reason          string             // Human-readable reason (internal/operator detail)
	Feedback        string             // Message for LLM retry (when Action == ActionRetry)
	Score           float64            // Confidence or distance (optional)
	ShadowMode      bool               // If true, block was logged but did not stop the pipeline
	MutatedText     string             // Text after redaction (when Action == ActionRedact); for string T mirrors Output
	Retryable       bool               // Whether a retry may succeed (default true for ActionRetry)
	Fatal           bool               // Hard stop for upstream pipeline; spec alias Escalate
	SafeUserMessage string             // User-facing message without internal details
	Disposition     FailureDisposition // Control-flow classification; set by FinishReport
	PayloadKind     PayloadKind        // Optional output classification from validator
}

// Clone returns a shallow copy of report.
func (r *Report) Clone() *Report {
	if r == nil {
		return nil
	}
	cp := *r
	return &cp
}

// CloneWithoutState returns a copy without input-specific runtime state.
func (r *Report) CloneWithoutState() *Report {
	cp := r.Clone()
	if cp == nil {
		return nil
	}
	cp.MutatedText = ""
	return cp
}

// RunResult holds the output and all reports from Pipeline.Run.
type RunResult[T any] struct {
	Output  T        // Mutated output (after redactions)
	Reports []Report // All validator reports for telemetry
	// OutputKind is the most restrictive PayloadKind from all reports (incl. user_channel blocks).
	OutputKind PayloadKind
}

// Decision returns the report that determines the pipeline outcome.
// Priority: system fault > terminal deny > retryable correction > redact > pass.
// Shadow block reports are observations and never select an enforcement decision.
// Reports order is nondeterministic in parallel phase; must scan entire slice.
func (r *RunResult[T]) Decision() *Report {
	var selected *Report
	priority := 0
	for i := range r.Reports {
		rep := &r.Reports[i]
		if rep.IsObservation() {
			continue
		}
		rank := reportPriority(rep)
		if rank >= priority {
			selected, priority = rep, rank
		}
	}
	if selected != nil {
		normalized := normalizeReport(selected)
		return &normalized
	}
	return FinishReport(&Report{Action: ActionPass}, ControlSpec{Action: ActionPass})
}

// ComposeReports selects the strongest enforcement and preserves the most restrictive
// payload classification. It returns a new report without mutating its inputs.
// Shadow policy blocks remain observations; faults always enforce.
func ComposeReports(reports ...*Report) *Report {
	values := make([]Report, 0, len(reports))
	for _, rep := range reports {
		if rep != nil {
			values = append(values, normalizeReport(rep))
		}
	}
	result := RunResult[struct{}]{Output: struct{}{}, Reports: values, OutputKind: AggregatePayloadKind(values)}
	selected := result.Decision()
	selected.PayloadKind = result.OutputKind
	return selected
}

const (
	priorityPass = iota + 1
	priorityRedact
	priorityRetry
	priorityFatal
	priorityNonRetryable
	priorityBlock
	priorityFault
)

func reportPriority(rep *Report) int {
	switch rep.effectiveDisposition() {
	case DispositionSystemFault:
		return priorityFault
	case DispositionTerminalDeny:
		if rep.Action == ActionBlock {
			return priorityBlock
		}
		if rep.Action == ActionRetry {
			return priorityNonRetryable
		}
		return priorityFatal
	case DispositionRetryableCorrection:
		return priorityRetry
	default:
		if rep.Action == ActionRedact {
			return priorityRedact
		}
		return priorityPass
	}
}
