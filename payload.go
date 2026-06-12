package guardy

// PayloadKind classifies pipeline output for user-channel filtering and control flow.
type PayloadKind int

const (
	// PayloadSafeUserText is safe to expose on the user channel.
	PayloadSafeUserText PayloadKind = iota
	// PayloadInternalControlSignal is orchestrator-internal (tool calls, routing).
	PayloadInternalControlSignal
	// PayloadTechnicalPayload is technical JSON or similar; must not reach end users.
	PayloadTechnicalPayload
)

// String returns a stable name for telemetry.
func (k PayloadKind) String() string {
	switch k {
	case PayloadSafeUserText:
		return "safe_user_text"
	case PayloadInternalControlSignal:
		return "internal_control_signal"
	case PayloadTechnicalPayload:
		return "technical_payload"
	default:
		return "unknown"
	}
}

const (
	payloadPrioritySafeUserText          = 1
	payloadPriorityInternalControlSignal = 2
	payloadPriorityTechnicalPayload      = 3
)

func payloadKindPriority(k PayloadKind) int {
	switch k {
	case PayloadTechnicalPayload:
		return payloadPriorityTechnicalPayload
	case PayloadInternalControlSignal:
		return payloadPriorityInternalControlSignal
	case PayloadSafeUserText:
		return payloadPrioritySafeUserText
	default:
		return 0
	}
}

// AggregatePayloadKind picks the most restrictive kind from reports.
// Default is PayloadSafeUserText when no report sets a non-zero kind.
func AggregatePayloadKind(reports []Report) PayloadKind {
	best := PayloadSafeUserText
	for i := range reports {
		k := reports[i].PayloadKind
		if k == PayloadSafeUserText {
			continue
		}
		if payloadKindPriority(k) > payloadKindPriority(best) {
			best = k
		}
	}
	return best
}

// WithPayloadKind is a helper for validators that classify output.
func WithPayloadKind(rep *Report, kind PayloadKind) *Report {
	if rep == nil {
		return nil
	}
	rep.PayloadKind = kind
	return rep
}
