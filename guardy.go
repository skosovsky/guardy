// Package guardy provides a pipeline engine for AI guardrails: validation,
// intervention actions (block, redact, override, retry), and tiered execution.
//
// See [.cursor/docs/TD.md] for the full technical design.
package guardy

// Action represents the intervention strategy for a validation violation.
type Action string

// Standard intervention actions.
const (
	Pass     Action = "pass"
	Redact   Action = "redact"
	Override Action = "override"
	Retry    Action = "retry"
	Block    Action = "block"
)

// Result is returned by a Validator after checking the input.
type Result struct {
	Passed       bool
	Action       Action
	Code         string // e.g. "PROMPT_INJECTION", "PII_DETECTED"
	Reason       string
	CleanText    string
	OverrideText string
}

// Document represents a RAG context document for grounding checks.
type Document struct {
	ID       string
	Content  string
	Metadata map[string]string
}

// Input is the data passed into validators and pipelines.
type Input struct {
	Text      string
	Metadata  map[string]any
	Documents []Document
}

// Report is the aggregated result of a pipeline run.
// When FinalAction is Override, OverrideText contains the response to return to the user.
type Report struct {
	Results      []Result
	FinalAction  Action
	FinalText    string
	OverrideText string
}

// PriorityForAction returns the aggregation priority of the action (higher wins).
// Used when picking which result to use for error code/reason when multiple validators run.
func PriorityForAction(a Action) int {
	switch a {
	case Block:
		return 5
	case Override:
		return 4
	case Redact:
		return 3
	case Retry:
		return 2
	case Pass:
		return 1
	default:
		return 0
	}
}
