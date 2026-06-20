package guardy

import "context"

// GuardedOutput is the canonical guarded output value returned by guardy.
// Host transports should format this value, not re-classify its safety.
type GuardedOutput[T any] struct {
	Value       T
	Kind        PayloadKind
	Decision    Decision
	Reports     []Report
	Deliverable bool
}

// DeliverableValue returns the guarded value only when guardy marked it deliverable.
func (o GuardedOutput[T]) DeliverableValue() (T, bool) {
	if !o.Deliverable {
		var zero T
		return zero, false
	}
	return o.Value, true
}

// GuardOutput runs the pipeline and returns one authoritative output contract.
func (p *Pipeline[T]) GuardOutput(ctx context.Context, scope ExecutionScope, output T) (GuardedOutput[T], error) {
	result, err := p.Run(ctx, scope, output)
	guarded := guardedOutputFromRun(result)
	if err != nil {
		return guarded, err
	}
	if decErr := errorFromDecision(result.Decision()); decErr != nil {
		guarded.Deliverable = false
		return guarded, decErr
	}
	return guarded, nil
}

func guardedOutputFromRun[T any](result RunResult[T]) GuardedOutput[T] {
	decision := result.PolicyDecision()
	deliverable := decision.Disposition == DispositionNone && result.OutputKind == PayloadSafeUserText
	value := result.Output
	if !deliverable {
		var zero T
		value = zero
	}
	return GuardedOutput[T]{
		Value:       value,
		Kind:        result.OutputKind,
		Decision:    decision,
		Reports:     append([]Report(nil), result.Reports...),
		Deliverable: deliverable,
	}
}
