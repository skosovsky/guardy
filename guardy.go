// Package guardy provides a pipeline engine for AI guardrails: validation,
// intervention actions (pass, block, redact, retry), and two-phase execution
// (sequential Fast-Path for mutations, parallel Slow-Path via errgroup).
//
// See .cursor/docs/task7.md for the technical specification.
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

// Report is the single result returned by a validator or the pipeline.
// It maps to metry/security attributes for telemetry.
// When Action == ActionRetry, Feedback contains the message for the LLM/orchestrator.
type Report struct {
	Action      Action  // ActionPass, ActionBlock, ActionRedact, ActionRetry
	Validator   string  // Name of the validator that produced this report (e.g. "pii_masking")
	Reason      string  // Human-readable reason
	Feedback    string  // Message for LLM retry (when Action == ActionRetry)
	Score       float64 // Confidence or distance (optional)
	ShadowMode  bool    // If true, block was logged but did not stop the pipeline
	MutatedText string  // Text after redaction (when Action == ActionRedact)
}

// RunResult holds the output and all reports from Pipeline.Run.
type RunResult[T any] struct {
	Output  T        // Mutated output (after redactions)
	Reports []Report // All validator reports for telemetry
}

// Decision returns the report that determines the pipeline outcome.
// Priority (per task7): Block > Retry > (last Redact) > (last Pass).
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
