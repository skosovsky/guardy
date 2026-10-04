package guardy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/skosovsky/guardy/internal/jsondoc"
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

// GuardedArgs is the canonical typed argument boundary returned by guardy.
type GuardedArgs[T any] struct {
	Value           T
	Raw             string
	SanitizedRaw    string
	Reports         []Report
	Decision        Decision
	PayloadKind     PayloadKind
	ConfigurationID string
}

// ArgsPipeline validates raw arguments and decodes them into T as one guardy-owned contract.
type ArgsPipeline[T any] struct {
	raw          *Pipeline[string]
	shape        ShapeProvider[T]
	final        *Pipeline[string]
	decode       func(string, *T) error
	encode       func(T) (string, error)
	requireFinal bool
	identity     string
}

// WithArgsConfigurationID attaches caller-owned policy/configuration identity.
func WithArgsConfigurationID[T any](identity string) ArgsOption[T] {
	return func(p *ArgsPipeline[T]) { p.identity = identity }
}

// WithArgsFinalGuard checks canonical bytes after decode and all post-bind hooks.
// Providing this option requires a non-nil pipeline; omitting it permits no final checks.
// This pipeline must be read-only: changing final bytes is a configuration fault.
func WithArgsFinalGuard[T any](final *Pipeline[string]) ArgsOption[T] {
	return func(p *ArgsPipeline[T]) { p.final = final; p.requireFinal = true }
}

// WithArgsCodec installs caller-owned bind/encode functions. Both are required.
// Encode must faithfully represent the bound value; custom codecs own this invariant.
func WithArgsCodec[T any](decode func(string, *T) error, encode func(T) (string, error)) ArgsOption[T] {
	return func(p *ArgsPipeline[T]) { p.decode, p.encode = decode, encode }
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
		return nil, configurationError("args", "raw", "required")
	}
	p := &ArgsPipeline[T]{
		raw:          raw,
		shape:        nil,
		final:        nil,
		decode:       func(s string, v *T) error { return jsondoc.Bind(s, v) },
		encode:       func(v T) (string, error) { b, err := json.Marshal(v); return string(b), err },
		requireFinal: false,
		identity:     raw.name,
	}
	for i, opt := range opts {
		if opt == nil {
			return nil, configurationError("args", fmt.Sprintf("options[%d]", i), "required")
		}
		opt(p)
	}
	if p.decode == nil || p.encode == nil {
		return nil, configurationError("args", "codec", "incomplete")
	}
	if p.requireFinal && p.final == nil {
		return nil, configurationError("args", "final", "required")
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
	if p.final == nil {
		return p.raw.RequiredScope()
	}
	return mergeScopeRequirements(p.raw.RequiredScope(), p.final.RequiredScope())
}

// RequiredScopeKeys returns scope keys from the raw guard.
func (p *ArgsPipeline[T]) RequiredScopeKeys() []string {
	if p == nil || p.raw == nil {
		return nil
	}
	return scopeRequirementKeys(p.RequiredScope())
}

// Validate runs raw validation before decoding into T.
// Cancellation is checked before/after each codec and post-bind callback and outranks
// correction. Wrapped cancellation causes remain accessible through [errors.Is].
func (p *ArgsPipeline[T]) Validate(ctx context.Context, scope ExecutionScope, raw string) (GuardedArgs[T], error) {
	if p == nil || p.raw == nil {
		var zero T
		return GuardedArgs[T]{
			ConfigurationID: "",
			Value:           zero,
			Raw:             raw,
			SanitizedRaw:    raw,
			Reports:         nil,
			Decision:        DecisionFromReport(nil),
			PayloadKind:     PayloadSafeUserText,
		}, errArgsPipelineNil
	}
	if err := checkScopeRequirements(scope, p.RequiredScope()); err != nil {
		var payload GuardedArgs[T]
		payload.Raw, payload.ConfigurationID = raw, p.identity
		return argsFault(payload, err)
	}
	result, err := p.raw.Run(ctx, scope, raw)
	payload := guardedArgsFromRun[T](raw, result)
	payload.ConfigurationID = p.identity
	if err != nil {
		return payload, err
	}
	if decErr := errorFromDecision(result.Decision()); decErr != nil {
		return payload, decErr
	}

	value, bindReport, bindErr := p.bind(ctx, result.Output)
	if bindReport != nil {
		payload.Reports = append(payload.Reports, *bindReport)
		decisionReport := refreshGuardedArgsDecision(&payload)
		return payload, retryErrorFromReport(decisionReport)
	}
	if bindErr != nil {
		return argsFault(payload, bindErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return argsFault(payload, ctxErr)
	}
	canonical, encodeErr := p.encode(value)
	if ctxErr := callbackCancellation(ctx, encodeErr); ctxErr != nil {
		return argsFault(payload, ctxErr)
	}
	if encodeErr != nil {
		return argsFault(payload, encodeErr)
	}
	payload.SanitizedRaw = canonical
	if p.final != nil {
		checked, checkErr := p.final.Run(ctx, scope, canonical)
		payload.Reports = append(payload.Reports, checked.Reports...)
		rep := refreshGuardedArgsDecision(&payload)
		if checkErr != nil {
			return argsFault(payload, checkErr)
		}
		if decErr := errorFromDecision(rep); decErr != nil {
			return payload, decErr
		}
		if checked.Output != canonical {
			return argsFault(payload, errors.New("guardy: final args guard mutated canonical payload"))
		}
	}
	if err := ctx.Err(); err != nil {
		return argsFault(payload, err)
	}
	payload.Value = value
	return payload, nil
}

// bind separates domain corrections from execution faults before canonical encoding.
func (p *ArgsPipeline[T]) bind(ctx context.Context, raw string) (T, *Report, error) {
	var value T
	if ctxErr := ctx.Err(); ctxErr != nil {
		return value, nil, ctxErr
	}
	unmarshalErr := p.decode(raw, &value)
	if ctxErr := callbackCancellation(ctx, unmarshalErr); ctxErr != nil {
		return value, nil, ctxErr
	}
	if unmarshalErr != nil {
		rep := FinishReport(&Report{
			Action:   ActionRetry,
			Code:     CodeJSONInvalid,
			Reason:   "invalid JSON for decode",
			Feedback: unmarshalErr.Error(),
		}, ControlSpec{Action: ActionRetry})
		return value, rep, unmarshalErr
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return value, nil, ctxErr
	}
	bindErr := invokePostBind(ctx, &value)
	if ctxErr := callbackCancellation(ctx, bindErr); ctxErr != nil {
		return value, nil, ctxErr
	}
	if bindErr != nil {
		rep := FinishReport(&Report{
			Action:   ActionRetry,
			Code:     CodePostBindViolation,
			Reason:   bindErr.Error(),
			Feedback: bindErr.Error(),
		}, ControlSpec{Action: ActionRetry})
		return value, rep, bindErr
	}
	return value, nil, nil
}

func argsFault[T any](payload GuardedArgs[T], cause error) (GuardedArgs[T], error) {
	payload.Reports = append(payload.Reports, validatorFaultReport(cause))
	rep := refreshGuardedArgsDecision(&payload)
	return payload, validatorFaultErrorFromReport(rep, cause)
}

func refreshGuardedArgsDecision[T any](args *GuardedArgs[T]) *Report {
	if args == nil {
		return nil
	}
	args.PayloadKind = AggregatePayloadKind(args.Reports)
	decisionReport := policyDecisionReport(args.Reports, args.PayloadKind)
	args.Decision = DecisionFromReport(decisionReport)
	return decisionReport
}

func guardedArgsFromRun[T any](raw string, result RunResult[string]) GuardedArgs[T] {
	var zero T
	return GuardedArgs[T]{
		ConfigurationID: "",
		Value:           zero,
		Raw:             raw,
		SanitizedRaw:    result.Output,
		Reports:         append([]Report(nil), result.Reports...),
		Decision:        result.PolicyDecision(),
		PayloadKind:     result.OutputKind,
	}
}
