package guardy

import (
	"errors"
	"fmt"
)

var (
	// ErrBlocked is returned when the pipeline result is Block (e.g. by StreamProcessor, WrapInput, WrapOutput).
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

// Error implements error without exposing correction feedback.
func (e *RetryError) Error() string {
	return ErrRetryRequested.Error()
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

// Error implements error without exposing diagnostic cause text.
func (e *ValidatorFaultError) Error() string {
	return ErrValidatorFailed.Error()
}

// Unwrap preserves both the fault category and its original typed cause.
func (e *ValidatorFaultError) Unwrap() error {
	return errors.Join(ErrValidatorFailed, e.Failure.Cause)
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
	normalized := normalizeReport(rep)
	cloned := &normalized
	return &BlockError{
		Message: cloned.PublicMessage(),
		Failure: *policyFailureFromReport(cloned, ErrBlocked),
		report:  *cloned,
	}
}

// errorFromDecision maps a pipeline decision to BlockError, RetryError, or nil (pass/redact).
// Control flow uses Disposition, not Action.
func errorFromDecision(rep *Report) error {
	if rep == nil {
		return blockErrorFromReport(rep)
	}
	if rep.IsSystemFault() {
		cloned := rep.Clone()
		return &ValidatorFaultError{
			Cause:   ErrValidatorFailed,
			Failure: *policyFailureFromReport(cloned, ErrValidatorFailed),
			report:  *cloned,
		}
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
	normalized := normalizeReport(rep)
	cloned := &normalized
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
