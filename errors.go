package guardy

import (
	"errors"
	"fmt"
)

var (
	// ErrBlocked is returned when the pipeline result is Block (e.g. by GuardWriter, WrapInput, WrapOutput).
	// Match with [errors.Is] against err and ErrBlocked. [GuardWriter] returns [*StreamError], which unwraps to ErrBlocked —
	// use [errors.As] into *StreamError for Code and Report. WrapInput/WrapOutput may wrap ErrBlocked with [fmt.Errorf]("%w: ...", ErrBlocked, reason).
	ErrBlocked = errors.New("guardy: input blocked")

	// ErrRetryRequested is returned when the pipeline result is Retry; the orchestrator should retry with Feedback.
	// WrapInput/WrapOutput return *RetryError; [GuardWriter] returns [*StreamError] on retry — use [errors.Is] for a quick check,
	// or [errors.As] into *StreamError or *RetryError for Report metadata.
	ErrRetryRequested = errors.New("guardy: retry requested")

	// ErrValidatorFailed is returned when a validator returns a system error.
	ErrValidatorFailed = errors.New("guardy: validator failed")
)

// RetryError carries pipeline retry metadata from WrapInput or WrapOutput when Decision() is ActionRetry.
type RetryError struct {
	Feedback string
	Report   Report
}

// Error implements error.
func (e *RetryError) Error() string {
	if e == nil {
		return "guardy: retry"
	}
	if e.Feedback != "" {
		return fmt.Sprintf("guardy retry: %s", e.Feedback)
	}
	if e.Report.Reason != "" {
		return fmt.Sprintf("guardy retry: %s", e.Report.Reason)
	}
	return "guardy: retry requested"
}

// Unwrap returns ErrRetryRequested so [errors.Is] matches *RetryError when the second argument is ErrRetryRequested.
func (e *RetryError) Unwrap() error {
	return ErrRetryRequested
}

// StreamError is returned by [GuardWriter] when a chunk decision is Block or Retry.
// Report is a snapshot (cloned from the pipeline decision); safe to read after the writer returns.
// Use [errors.As] into *StreamError to read Report metadata without string parsing.
type StreamError struct {
	Action Action
	Report Report
	Err    error // ErrBlocked or ErrRetryRequested
}

// Error implements error.
func (e *StreamError) Error() string {
	if e == nil {
		return "guardy: stream"
	}
	switch e.Action {
	case ActionBlock:
		if e.Report.PublicMessage() != "" {
			return fmt.Sprintf("guardy stream blocked: %s", e.Report.PublicMessage())
		}
		return "guardy: input blocked"
	case ActionRetry:
		if e.Report.OrchestratorMessage() != "" {
			return fmt.Sprintf("guardy stream retry: %s", e.Report.OrchestratorMessage())
		}
		return "guardy: retry requested"
	default:
		return "guardy: stream error"
	}
}

// Unwrap returns ErrBlocked or ErrRetryRequested so [errors.Is] works on *StreamError.
func (e *StreamError) Unwrap() error {
	if e != nil && e.Err != nil {
		return e.Err
	}
	return ErrBlocked
}

func streamErrorFromDecision(rep *Report) error {
	if rep == nil {
		return ErrBlocked
	}
	cloned := rep.Clone()
	errSentinel := ErrBlocked
	switch rep.Action {
	case ActionRetry:
		if !cloned.Retryable {
			cloned.Retryable = true
		}
		errSentinel = ErrRetryRequested
	case ActionBlock:
		cloned.Retryable = false
	case ActionPass, ActionRedact:
		// GuardWriter only surfaces Block and Retry.
	}
	return &StreamError{
		Action: rep.Action,
		Report: *cloned,
		Err:    errSentinel,
	}
}
