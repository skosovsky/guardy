package guardy

import (
	"errors"
	"fmt"
)

var (
	// ErrBlocked is returned when the pipeline result is Block (e.g. by GuardWriter, WrapInput, WrapOutput).
	// Match with [errors.Is] against err and ErrBlocked. [GuardWriter] returns [*StreamError], which unwraps to ErrBlocked —
	// use [errors.As] into *StreamError for Code and Report. WrapInput/WrapOutput return [*BlockError] — use [errors.As] for Report.Disposition.
	ErrBlocked = errors.New("guardy: input blocked")

	// ErrRetryRequested is returned when the pipeline result is Retry; the orchestrator should retry with Feedback.
	// WrapInput/WrapOutput return *RetryError; [GuardWriter] returns [*StreamError] on retry — use [errors.Is] for a quick check,
	// or [errors.As] into *StreamError or *RetryError for Report metadata.
	ErrRetryRequested = errors.New("guardy: retry requested")

	// ErrValidatorFailed is returned when a validator returns a system error.
	// Use [errors.As] into [*ValidatorFaultError] for Report.Disposition == DispositionSystemFault.
	ErrValidatorFailed = errors.New("guardy: validator failed")
)

// BlockError carries pipeline block metadata from WrapInput, WrapOutput, or ValidateAndDecode.
type BlockError struct {
	Message string
	Report  Report
}

// Error implements error.
func (e *BlockError) Error() string {
	if e == nil {
		return ErrBlocked.Error()
	}
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", ErrBlocked.Error(), e.Message)
	}
	return ErrBlocked.Error()
}

// Unwrap returns ErrBlocked so [errors.Is] matches *BlockError.
func (e *BlockError) Unwrap() error {
	return ErrBlocked
}

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

// ValidatorFaultError carries system-fault metadata when a validator or pipeline infrastructure fails.
type ValidatorFaultError struct {
	Cause  error
	Report Report
}

// Error implements error.
func (e *ValidatorFaultError) Error() string {
	if e == nil {
		return "guardy: validator failed"
	}
	if e.Cause != nil {
		return fmt.Sprintf("guardy: validator failed: %v", e.Cause)
	}
	return "guardy: validator failed"
}

// Unwrap returns ErrValidatorFailed so [errors.Is] matches *ValidatorFaultError.
func (e *ValidatorFaultError) Unwrap() error {
	return ErrValidatorFailed
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
		return ErrBlocked.Error()
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

func blockErrorFromReport(rep *Report) error {
	if rep == nil {
		return ErrBlocked
	}
	cloned := rep.Clone()
	if cloned.Disposition == DispositionNone {
		cloned.Disposition = DeriveDisposition(cloned, nil)
	}
	return &BlockError{
		Message: cloned.PublicMessage(),
		Report:  *cloned,
	}
}

// errorFromDecision maps a pipeline decision to BlockError, RetryError, or nil (pass/redact).
// Control flow uses Disposition, not Action (task14 §2.2).
func errorFromDecision(rep *Report) error {
	if rep == nil {
		return blockErrorFromReport(rep)
	}
	if rep.IsRetryableCorrection() {
		return &RetryError{Feedback: rep.OrchestratorMessage(), Report: *rep}
	}
	if rep.IsTerminalDeny() {
		return blockErrorFromReport(rep)
	}
	switch rep.Action {
	case ActionPass, ActionRedact:
		return nil
	default:
		return fmt.Errorf(
			"%w: unsupported pipeline action %s",
			ErrValidatorFailed,
			rep.Action.String(),
		)
	}
}

func validatorFaultReport(cause error) Report {
	reason := "validator failed"
	if cause != nil {
		reason = cause.Error()
	}
	return Report{
		Validator:   "pipeline",
		Code:        CodeValidatorFailed,
		Reason:      reason,
		Disposition: DispositionSystemFault,
	}
}

func validatorFaultError(cause error) error {
	return &ValidatorFaultError{
		Cause:  cause,
		Report: validatorFaultReport(cause),
	}
}

func streamErrorFromDecision(rep *Report) error {
	if rep == nil {
		blockRep := FinishReport(&Report{
			Action: ActionBlock,
			Code:   CodePolicyViolation,
			Reason: "stream blocked",
		}, ControlSpec{Action: ActionBlock})
		return &StreamError{
			Action: ActionBlock,
			Report: *blockRep,
			Err:    ErrBlocked,
		}
	}
	cloned := rep.Clone()
	if cloned.Disposition == DispositionNone {
		cloned.Disposition = DeriveDisposition(cloned, nil)
	}
	if rep.IsRetryableCorrection() {
		return &StreamError{
			Action: ActionRetry,
			Report: *cloned,
			Err:    ErrRetryRequested,
		}
	}
	if rep.IsTerminalDeny() {
		if cloned.Action == ActionRetry {
			cloned.Retryable = false
		}
		return &StreamError{
			Action: ActionBlock,
			Report: *cloned,
			Err:    ErrBlocked,
		}
	}
	switch rep.Action {
	case ActionPass, ActionRedact:
		return fmt.Errorf(
			"%w: streamErrorFromDecision called with action %s",
			ErrValidatorFailed,
			rep.Action.String(),
		)
	default:
		return fmt.Errorf(
			"%w: streamErrorFromDecision called with action %s",
			ErrValidatorFailed,
			rep.Action.String(),
		)
	}
}
