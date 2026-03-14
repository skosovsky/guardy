// Package guardy provides a pipeline engine for AI guardrails: validation,
// intervention actions (pass, block, redact), and two-phase execution
// (sequential Fast-Path for mutations, parallel Slow-Path via errgroup).
//
// See .cursor/docs/task6.md for the technical specification.
package guardy

// Action is the intervention outcome from a validator or pipeline.
type Action string

// Supported pipeline/validator actions.
const (
	ActionPass   Action = "pass"   // Validation passed
	ActionBlock  Action = "block"  // Content should be blocked
	ActionRedact Action = "redact" // Content was redacted (see MutatedText)
)

// Report is the single result returned by a validator or the pipeline.
// It maps to metry/security attributes for telemetry.
type Report struct {
	Action      Action  // "pass", "block", "redact"
	Validator   string  // Name of the validator that produced this report (e.g. "pii_masking")
	Reason      string  // Human-readable reason
	Score       float64 // Confidence or distance (optional)
	ShadowMode  bool    // If true, block was logged but did not stop the pipeline
	MutatedText string  // Text after redaction (when Action == "redact")
}
