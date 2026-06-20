package guardy

// Decision is the canonical control-flow contract returned at guardy boundaries.
// Use it for orchestration and host integration instead of parsing Report text fields.
type Decision struct {
	Action          Action
	Disposition     FailureDisposition
	Code            string
	SafeMessage     string
	RetryFeedback   string
	PayloadKind     PayloadKind
	Validator       string
	Severity        Severity
	Retryable       bool
	Terminal        bool
	SystemFault     bool
	UserCorrectable bool
}

// DecisionFromReport converts a validator or pipeline report into the canonical
// host-facing decision contract.
func DecisionFromReport(rep *Report) Decision {
	if rep == nil {
		return Decision{
			Action:          ActionPass,
			Disposition:     DispositionNone,
			Code:            "",
			SafeMessage:     "",
			RetryFeedback:   "",
			PayloadKind:     PayloadSafeUserText,
			Validator:       "",
			Severity:        "",
			Retryable:       false,
			Terminal:        false,
			SystemFault:     false,
			UserCorrectable: false,
		}
	}
	cp := *rep
	if cp.Disposition == DispositionNone {
		cp.Disposition = DeriveDisposition(&cp, nil)
	}
	d := Decision{
		Action:          cp.Action,
		Disposition:     cp.Disposition,
		Code:            cp.Code,
		SafeMessage:     cp.PublicMessage(),
		RetryFeedback:   cp.OrchestratorMessage(),
		PayloadKind:     cp.PayloadKind,
		Validator:       cp.Validator,
		Severity:        cp.Severity,
		Retryable:       cp.Retryable,
		Terminal:        false,
		SystemFault:     false,
		UserCorrectable: false,
	}
	d.Terminal = d.Disposition == DispositionTerminalDeny
	d.SystemFault = d.Disposition == DispositionSystemFault
	d.UserCorrectable = d.Disposition == DispositionRetryableCorrection
	return d
}

// IsTerminal reports whether the decision is a hard stop.
func (d Decision) IsTerminal() bool {
	return d.Terminal || d.Disposition == DispositionTerminalDeny
}

// IsRetryable reports whether retrying with a corrected payload may succeed.
func (d Decision) IsRetryable() bool {
	return d.UserCorrectable || d.Disposition == DispositionRetryableCorrection
}

// IsSystemFault reports whether the decision came from guardy infrastructure or validator failure.
func (d Decision) IsSystemFault() bool {
	return d.SystemFault || d.Disposition == DispositionSystemFault
}

// PolicyDecision returns the canonical decision for a run result.
func (r *RunResult[T]) PolicyDecision() Decision {
	if r == nil {
		return DecisionFromReport(nil)
	}
	d := DecisionFromReport(r.Decision())
	if r.OutputKind != PayloadSafeUserText || d.PayloadKind == PayloadSafeUserText {
		d.PayloadKind = r.OutputKind
	}
	return d
}

// PolicyFailure exposes a guardy decision as an error contract.
// It is available through [errors.As] from guardy boundary errors.
//
//nolint:errname // Task15 names the canonical contract PolicyFailure.
type PolicyFailure struct {
	Decision Decision
	Cause    error
}

// Error implements error.
func (e *PolicyFailure) Error() string {
	if e == nil {
		return "guardy: policy failure"
	}
	if e.Decision.SafeMessage != "" {
		return e.Decision.SafeMessage
	}
	if e.Decision.RetryFeedback != "" {
		return e.Decision.RetryFeedback
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "guardy: policy failure"
}

// Unwrap returns the underlying sentinel or validator cause.
func (e *PolicyFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func policyFailureFromReport(rep *Report, cause error) *PolicyFailure {
	d := DecisionFromReport(rep)
	if cause != nil && d.Disposition == DispositionNone {
		d.Disposition = DispositionSystemFault
		d.SystemFault = true
	}
	return &PolicyFailure{Decision: d, Cause: cause}
}
