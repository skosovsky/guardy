package guardy

// FailureDisposition classifies pipeline outcomes for control flow without parsing Code or Reason.
type FailureDisposition int

const (
	// DispositionNone indicates pass or successful redact.
	DispositionNone FailureDisposition = iota
	// DispositionTerminalDeny indicates a hard deny (block, non-retryable retry, fatal).
	DispositionTerminalDeny
	// DispositionRetryableCorrection indicates the orchestrator may retry (ActionRetry && Retryable).
	DispositionRetryableCorrection
	// DispositionSystemFault indicates validator or pipeline infrastructure failure.
	DispositionSystemFault
)

// String returns a stable name for telemetry.
func (d FailureDisposition) String() string {
	switch d {
	case DispositionNone:
		return "none"
	case DispositionTerminalDeny:
		return "terminal_deny"
	case DispositionRetryableCorrection:
		return "retryable_correction"
	case DispositionSystemFault:
		return "system_fault"
	default:
		return "unknown"
	}
}

// DeriveDisposition computes disposition from report fields and an optional system error.
func DeriveDisposition(rep *Report, err error) FailureDisposition {
	if err != nil {
		return DispositionSystemFault
	}
	if rep == nil {
		return DispositionNone
	}
	switch rep.Action {
	case ActionBlock:
		return DispositionTerminalDeny
	case ActionRetry:
		if rep.Fatal {
			return DispositionTerminalDeny
		}
		if rep.Retryable {
			return DispositionRetryableCorrection
		}
		return DispositionTerminalDeny
	case ActionPass, ActionRedact:
		if rep.Fatal {
			return DispositionTerminalDeny
		}
		return DispositionNone
	default:
		return DispositionSystemFault
	}
}

func (r *Report) effectiveDisposition() FailureDisposition {
	if r == nil {
		return DispositionNone
	}
	if r.Disposition != DispositionNone {
		return r.Disposition
	}
	return DeriveDisposition(r, nil)
}

// IsTerminalDeny reports whether the outcome is a hard deny.
func (r *Report) IsTerminalDeny() bool {
	return r.effectiveDisposition() == DispositionTerminalDeny
}

// IsRetryableCorrection reports whether the orchestrator should attempt correction.
func (r *Report) IsRetryableCorrection() bool {
	return r.effectiveDisposition() == DispositionRetryableCorrection
}

// IsSystemFault reports whether the outcome is an infrastructure or validator fault.
func (r *Report) IsSystemFault() bool {
	return r.effectiveDisposition() == DispositionSystemFault
}
