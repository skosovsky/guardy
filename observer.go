package guardy

import "context"

// Observer is a callback invoked for non-blocking shadow block reports.
// It receives a typed event so telemetry does not need to recover scope or
// pipeline metadata from context wrappers.
type Observer func(ctx context.Context, event GuardEvent)

// GuardTelemetry is safe, structured telemetry derived from a guard event.
type GuardTelemetry struct {
	Action      Action
	Disposition FailureDisposition
	Code        string
	Validator   string
	Severity    Severity
	PayloadKind PayloadKind
}

// GuardEvent carries explicit observer metadata for a guard report.
type GuardEvent struct {
	Scope        ExecutionScope
	Decision     Decision
	Report       *Report
	Phase        ValidationPhase
	PipelineName string
	PayloadKind  PayloadKind
	Telemetry    GuardTelemetry
}

func newGuardEvent(
	scope ExecutionScope,
	rep *Report,
	phase ValidationPhase,
	pipelineName string,
) GuardEvent {
	cloned := rep.Clone()
	decision := DecisionFromReport(cloned)
	return GuardEvent{
		Scope:        scope,
		Decision:     decision,
		Report:       cloned,
		Phase:        phase,
		PipelineName: pipelineName,
		PayloadKind:  decision.PayloadKind,
		Telemetry: GuardTelemetry{
			Action:      decision.Action,
			Disposition: decision.Disposition,
			Code:        decision.Code,
			Validator:   decision.Validator,
			Severity:    decision.Severity,
			PayloadKind: decision.PayloadKind,
		},
	}
}
