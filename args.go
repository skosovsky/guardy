package guardy

import (
	"context"
	"encoding/json"
	"errors"
)

var errArgsPipelineNil = errors.New("guardy: args pipeline requires non-nil raw pipeline")

// ShapeProvider exposes optional schema or shape metadata without tying guardy
// to a concrete generator.
type ShapeProvider[T any] interface {
	Shape() any
}

// ShapeProviderFunc adapts a function to [ShapeProvider].
type ShapeProviderFunc[T any] func() any

// Shape implements [ShapeProvider].
func (f ShapeProviderFunc[T]) Shape() any {
	if f == nil {
		return nil
	}
	return f()
}

// GuardedPayload is the canonical typed argument result returned by guardy.
type GuardedPayload[T any] struct {
	Value        T
	Raw          string
	SanitizedRaw string
	Reports      []Report
	Decision     Decision
	OutputKind   PayloadKind
}

// ArgsPipeline validates raw arguments and decodes them into T as one guardy-owned contract.
type ArgsPipeline[T any] struct {
	raw   *Pipeline[string]
	shape ShapeProvider[T]
}

// ArgsOption configures [ArgsPipeline].
type ArgsOption[T any] func(*ArgsPipeline[T])

// WithArgsShapeProvider attaches optional shape metadata to an args pipeline.
func WithArgsShapeProvider[T any](provider ShapeProvider[T]) ArgsOption[T] {
	return func(p *ArgsPipeline[T]) {
		p.shape = provider
	}
}

// CompileArgs builds a typed arguments pipeline from a raw string guard.
func CompileArgs[T any](raw *Pipeline[string], opts ...ArgsOption[T]) (*ArgsPipeline[T], error) {
	if raw == nil {
		return nil, errArgsPipelineNil
	}
	p := &ArgsPipeline[T]{raw: raw, shape: nil}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// MustCompileArgs is like [CompileArgs] but panics on invalid configuration.
func MustCompileArgs[T any](raw *Pipeline[string], opts ...ArgsOption[T]) *ArgsPipeline[T] {
	p, err := CompileArgs[T](raw, opts...)
	if err != nil {
		panic(err)
	}
	return p
}

// Shape returns optional metadata attached at compile time.
func (p *ArgsPipeline[T]) Shape() (any, bool) {
	if p == nil || p.shape == nil {
		return nil, false
	}
	return p.shape.Shape(), true
}

// RequiredScope returns typed scope requirements from the raw guard.
func (p *ArgsPipeline[T]) RequiredScope() []ScopeRequirement {
	if p == nil || p.raw == nil {
		return nil
	}
	return p.raw.RequiredScope()
}

// RequiredScopeKeys returns scope keys from the raw guard.
func (p *ArgsPipeline[T]) RequiredScopeKeys() []string {
	if p == nil || p.raw == nil {
		return nil
	}
	return p.raw.RequiredScopeKeys()
}

// Validate runs raw validation before decoding into T.
func (p *ArgsPipeline[T]) Validate(ctx context.Context, scope ExecutionScope, raw string) (GuardedPayload[T], error) {
	if p == nil || p.raw == nil {
		var zero T
		return GuardedPayload[T]{
			Value:        zero,
			Raw:          raw,
			SanitizedRaw: raw,
			Reports:      nil,
			Decision:     DecisionFromReport(nil),
			OutputKind:   PayloadSafeUserText,
		}, errArgsPipelineNil
	}
	result, err := p.raw.Run(ctx, scope, raw)
	payload := guardedPayloadFromRun[T](raw, result)
	if err != nil {
		return payload, err
	}
	if decErr := errorFromDecision(result.Decision()); decErr != nil {
		return payload, decErr
	}

	var value T
	if unmarshalErr := json.Unmarshal([]byte(result.Output), &value); unmarshalErr != nil {
		rep := FinishReport(&Report{
			Action:   ActionRetry,
			Code:     CodeJSONInvalid,
			Reason:   "invalid JSON for decode",
			Feedback: unmarshalErr.Error(),
		}, ControlSpec{Action: ActionRetry})
		payload.Reports = append(payload.Reports, *rep)
		payload.Decision = DecisionFromReport(rep)
		return payload, retryErrorFromReport(rep)
	}
	if bindErr := invokePostBind(ctx, &value); bindErr != nil {
		rep := FinishReport(&Report{
			Action:   ActionRetry,
			Code:     CodePostBindViolation,
			Reason:   bindErr.Error(),
			Feedback: bindErr.Error(),
		}, ControlSpec{Action: ActionRetry})
		payload.Reports = append(payload.Reports, *rep)
		payload.Decision = DecisionFromReport(rep)
		return payload, retryErrorFromReport(rep)
	}
	payload.Value = value
	return payload, nil
}

func guardedPayloadFromRun[T any](raw string, result RunResult[string]) GuardedPayload[T] {
	var zero T
	return GuardedPayload[T]{
		Value:        zero,
		Raw:          raw,
		SanitizedRaw: result.Output,
		Reports:      append([]Report(nil), result.Reports...),
		Decision:     result.PolicyDecision(),
		OutputKind:   result.OutputKind,
	}
}
