package guardy

import (
	"errors"
	"fmt"
)

var (
	// ErrBlocked is returned when the pipeline result is Block (e.g. by GuardWriter, WrapInput, WrapOutput).
	// Match with [errors.Is] against err and ErrBlocked. WrapInput/WrapOutput may wrap it with [fmt.Errorf]("%w: ...", ErrBlocked, reason).
	ErrBlocked = errors.New("guardy: input blocked")

	// ErrRetryRequested is returned when the pipeline result is Retry; the orchestrator should retry with Feedback.
	// WrapInput/WrapOutput return *RetryError, which unwraps to ErrRetryRequested — use [errors.Is] for a quick check,
	// or [errors.As] into *RetryError for Feedback and Report.
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
