package guardy

import (
	"errors"
	"fmt"
)

var (
	// ErrBlocked is returned when the pipeline result is Block (e.g. by GuardWriter, WrapInput, WrapOutput).
	// Match with [errors.Is] against err and ErrBlocked for quick checks; use [errors.As] into [*PolicyFailure] for routing.
	ErrBlocked = errors.New("guardy: input blocked")

	// ErrRetryRequested is returned when the pipeline result is Retry; the orchestrator should retry with Feedback.
	// Match with [errors.Is] against err and ErrRetryRequested for quick checks; use [errors.As] into [*PolicyFailure] for routing.
	ErrRetryRequested = errors.New("guardy: retry requested")

	// ErrValidatorFailed is returned when a validator returns a system error.
	// Use [errors.As] into [*PolicyFailure] for the canonical system-fault decision.
	ErrValidatorFailed = errors.New("guardy: validator failed")
)

// BlockError carries a terminal policy failure from WrapInput or WrapOutput.
type BlockError struct {
	Message string
	Failure PolicyFailure
	report  Report
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

// As exposes the canonical policy failure contract.
func (e *BlockError) As(target any) bool {
	if e == nil {
		return false
	}
	return asPolicyFailure(target, &e.Failure)
}

// ReportSnapshot returns a validator report snapshot for telemetry.
func (e *BlockError) ReportSnapshot() Report {
	if e == nil {
		return Report{}
	}
	return e.report
}

// RetryError carries a retryable policy failure from WrapInput, WrapOutput, or typed argument validation.
type RetryError struct {
	Feedback string
	Failure  PolicyFailure
	report   Report
}

// Error implements error.
func (e *RetryError) Error() string {
	if e == nil {
		return "guardy: retry"
	}
	if e.Feedback != "" {
		return fmt.Sprintf("guardy retry: %s", e.Feedback)
	}
	if e.Failure.Decision.RetryFeedback != "" {
		return fmt.Sprintf("guardy retry: %s", e.Failure.Decision.RetryFeedback)
	}
	return "guardy: retry requested"
}

// Unwrap returns ErrRetryRequested so [errors.Is] matches *RetryError when the second argument is ErrRetryRequested.
func (e *RetryError) Unwrap() error {
	return ErrRetryRequested
}

// As exposes the canonical policy failure contract.
func (e *RetryError) As(target any) bool {
	if e == nil {
		return false
	}
	return asPolicyFailure(target, &e.Failure)
}

// ReportSnapshot returns a validator report snapshot for telemetry.
func (e *RetryError) ReportSnapshot() Report {
	if e == nil {
		return Report{}
	}
	return e.report
}

// ValidatorFaultError carries system-fault metadata when a validator or pipeline infrastructure fails.
type ValidatorFaultError struct {
	Cause   error
	Failure PolicyFailure
	report  Report
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

// As exposes the canonical policy failure contract.
func (e *ValidatorFaultError) As(target any) bool {
	if e == nil {
		return false
	}
	return asPolicyFailure(target, &e.Failure)
}

// ReportSnapshot returns a validator report snapshot for telemetry.
func (e *ValidatorFaultError) ReportSnapshot() Report {
	if e == nil {
		return Report{}
	}
	return e.report
}

// StreamError is returned by [GuardWriter] when a chunk decision is Block or Retry.
// Use [errors.As] into [*PolicyFailure] for control flow without string parsing.
type StreamError struct {
	Action  Action
	Failure PolicyFailure
	Err     error // ErrBlocked or ErrRetryRequested
	report  Report
}

// Error implements error.
func (e *StreamError) Error() string {
	if e == nil {
		return "guardy: stream"
	}
	switch e.Action {
	case ActionBlock:
		if e.Failure.Decision.SafeMessage != "" {
			return fmt.Sprintf("guardy stream blocked: %s", e.Failure.Decision.SafeMessage)
		}
		return ErrBlocked.Error()
	case ActionRetry:
		if e.Failure.Decision.RetryFeedback != "" {
			return fmt.Sprintf("guardy stream retry: %s", e.Failure.Decision.RetryFeedback)
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

// As exposes the canonical policy failure contract.
func (e *StreamError) As(target any) bool {
	if e == nil {
		return false
	}
	return asPolicyFailure(target, &e.Failure)
}

// ReportSnapshot returns a validator report snapshot for telemetry.
func (e *StreamError) ReportSnapshot() Report {
	if e == nil {
		return Report{}
	}
	return e.report
}

func asPolicyFailure(target any, failure *PolicyFailure) bool {
	pf, ok := target.(**PolicyFailure)
	if !ok {
		return false
	}
	*pf = failure
	return true
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
		Failure: *policyFailureFromReport(cloned, ErrBlocked),
		report:  *cloned,
	}
}

// errorFromDecision maps a pipeline decision to BlockError, RetryError, or nil (pass/redact).
// Control flow uses Disposition, not Action (task14 §2.2).
func errorFromDecision(rep *Report) error {
	if rep == nil {
		return blockErrorFromReport(rep)
	}
	if rep.IsRetryableCorrection() {
		return retryErrorFromReport(rep)
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
	report := validatorFaultReport(cause)
	return &ValidatorFaultError{
		Cause:   cause,
		Failure: *policyFailureFromReport(&report, causeOrDefault(cause, ErrValidatorFailed)),
		report:  report,
	}
}

func retryErrorFromReport(rep *Report) error {
	cloned := rep.Clone()
	if cloned.Disposition == DispositionNone {
		cloned.Disposition = DeriveDisposition(cloned, nil)
	}
	return &RetryError{
		Feedback: cloned.OrchestratorMessage(),
		Failure:  *policyFailureFromReport(cloned, ErrRetryRequested),
		report:   *cloned,
	}
}

func causeOrDefault(cause error, fallback error) error {
	if cause != nil {
		return cause
	}
	return fallback
}

func streamErrorFromDecision(rep *Report) error {
	if rep == nil {
		blockRep := FinishReport(&Report{
			Action: ActionBlock,
			Code:   CodePolicyViolation,
			Reason: "stream blocked",
		}, ControlSpec{Action: ActionBlock})
		return &StreamError{
			Action:  ActionBlock,
			Failure: *policyFailureFromReport(blockRep, ErrBlocked),
			Err:     ErrBlocked,
			report:  *blockRep,
		}
	}
	cloned := rep.Clone()
	if cloned.Disposition == DispositionNone {
		cloned.Disposition = DeriveDisposition(cloned, nil)
	}
	if rep.IsRetryableCorrection() {
		return &StreamError{
			Action:  ActionRetry,
			Failure: *policyFailureFromReport(cloned, ErrRetryRequested),
			Err:     ErrRetryRequested,
			report:  *cloned,
		}
	}
	if rep.IsTerminalDeny() {
		if cloned.Action == ActionRetry {
			cloned.Retryable = false
		}
		return &StreamError{
			Action:  ActionBlock,
			Failure: *policyFailureFromReport(cloned, ErrBlocked),
			Err:     ErrBlocked,
			report:  *cloned,
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
