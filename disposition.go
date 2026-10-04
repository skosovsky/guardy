package guardy

import "math"

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
	if r.Action < ActionPass || r.Action > ActionRetry ||
		r.Disposition < DispositionNone || r.Disposition > DispositionSystemFault ||
		r.PayloadKind < PayloadSafeUserText || r.PayloadKind > PayloadTechnicalPayload ||
		math.IsNaN(r.Score) || math.IsInf(r.Score, 0) {
		return DispositionSystemFault
	}
	if r.Disposition == DispositionSystemFault {
		return DispositionSystemFault
	}
	if r.Disposition == DispositionRetryableCorrection && r.Action != ActionRetry {
		return DispositionSystemFault
	}
	if r.Fatal || r.Disposition == DispositionTerminalDeny {
		return DispositionTerminalDeny
	}
	if r.Disposition == DispositionRetryableCorrection {
		return DispositionRetryableCorrection
	}
	return DeriveDisposition(r, nil)
}

// IsObservation reports whether this is a shadow policy block without a fault or escalation.
// Retry, fatal and invalid results are never observations.
func (r *Report) IsObservation() bool {
	return r != nil && r.ShadowMode && r.Action == ActionBlock && !r.Fatal &&
		r.effectiveDisposition() == DispositionTerminalDeny
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
