// Package guardy provides a pipeline engine for AI guardrails: validation,
// intervention actions (pass, block, redact, retry), and two-phase execution
// (sequential Fast-Path for mutations, parallel Slow-Path via errgroup).
//
// See .cursor/docs/task9.md for the v2 technical specification.
package guardy

// Action is the intervention outcome from a validator or pipeline.
type Action int

// String returns the canonical name for the action.
func (a Action) String() string {
	switch a {
	case ActionPass:
		return "pass"
	case ActionBlock:
		return "block"
	case ActionRedact:
		return "redact"
	case ActionRetry:
		return "retry"
	default:
		return "unknown"
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
// It maps to metry/security attributes for telemetry and control-flow decisions.
// Consumers should use Code, Retryable, Fatal, and Action — not parse Reason strings.
// When Action == ActionRetry, Feedback contains the message for the LLM/orchestrator.
type Report struct {
	Action          Action   // ActionPass, ActionBlock, ActionRedact, ActionRetry
	Validator       string   // Name of the validator that produced this report
	Code            string   // Machine-readable rule code (for alerting/telemetry)
	Severity        Severity // Risk level for the report
	Reason          string   // Human-readable reason (internal/operator detail)
	Feedback        string   // Message for LLM retry (when Action == ActionRetry)
	Score           float64  // Confidence or distance (optional)
	ShadowMode      bool     // If true, block was logged but did not stop the pipeline
	MutatedText     string   // Text after redaction (when Action == ActionRedact); for string T mirrors Output
	Retryable       bool     // Whether a retry may succeed (default true for ActionRetry)
	Fatal           bool     // Hard stop for upstream pipeline; spec alias Escalate
	SafeUserMessage string   // User-facing message without internal details
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
}

// Decision returns the report that determines the pipeline outcome.
// Priority (per task9): Block > Retry > (last Redact) > (last Pass).
// Reports order is nondeterministic in slow path; must scan entire slice.
func (r *RunResult[T]) Decision() *Report {
	var block, retry, lastRedact, lastPass *Report
	for i := range r.Reports {
		rep := &r.Reports[i]
		switch rep.Action {
		case ActionBlock:
			if !rep.ShadowMode && block == nil {
				block = rep
			}
		case ActionRetry:
			if retry == nil {
				retry = rep
			}
		case ActionRedact:
			lastRedact = rep
		case ActionPass:
			lastPass = rep
		}
	}
	if block != nil {
		return block
	}
	if retry != nil {
		return retry
	}
	if lastRedact != nil {
		return lastRedact
	}
	if lastPass != nil {
		return lastPass
	}
	return &Report{Action: ActionPass}
}
