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
	Passed bool
	Action Action
	Code   string // e.g. "PROMPT_INJECTION", "PII_DETECTED"

	// Feedback triad: especially useful for Action == Retry (LLM self-correction).
	// All three are optional; simple validators may leave Evidence and Guidance empty.
	Reason   string // Short description of why the check failed
	Evidence string // Exact quote or fragment that triggered the violation
	Guidance string // Instruction for the model on how to fix the text

	// Mutations
	CleanText    string
	OverrideText string
}

// Document represents a RAG context document for grounding checks.
type Document struct {
	ID       string
	Content  string
	Metadata map[string]string
}

// Message represents a single turn in a conversation (e.g. system, user, assistant, tool).
// Used by Input.Messages for context-aware validation (e.g. Tier 3 LLM-as-judge).
type Message struct {
	Role    string // e.g. "system", "user", "assistant", "tool"
	Content string
}

// Input is the data passed into validators and pipelines.
type Input struct {
	Text      string    // Current fragment for fast checks (e.g. streaming chunk)
	Messages  []Message // Full conversation context for deep analysis (Tier 3)
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

// WorstReason returns the reason of the result that matches FinalAction (or empty if none/Pass).
// Useful for PipelineMiddleware and logging when you need a single "main" reason without iterating Results.
func (r Report) WorstReason() string {
	for _, res := range r.Results {
		if res.Action == r.FinalAction {
			return res.Reason
		}
	}
	return ""
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
