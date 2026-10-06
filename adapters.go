package guardy

import (
	"context"
	"errors"
)

// Handler is a generic host function shape guardy can wrap without owning host types.
type Handler[Req, Res any] func(context.Context, Req) (Res, error)

// GuardedArgsHandler is a host function shape that receives the full guardy
// argument boundary instead of only the decoded value.
type GuardedArgsHandler[Req, Res any] func(context.Context, GuardedArgs[Req]) (Res, error)

// GuardedJSONArgsHandler is a host function shape for dynamic JSON argument
// boundaries.
type GuardedJSONArgsHandler[Res any] func(context.Context, GuardedJSONArgs) (Res, error)

// WrapArgs validates raw arguments through an [ArgsPipeline] before calling next.
func WrapArgs[Req, Res any](
	p *ArgsPipeline[Req],
	scopeFactory ScopeFactory,
	next Handler[Req, Res],
) func(context.Context, string) (Res, GuardedArgs[Req], error) {
	if p == nil {
		panic("guardy: WrapArgs requires non-nil ArgsPipeline")
	}
	if next == nil {
		panic("guardy: WrapArgs requires non-nil next")
	}
	return func(ctx context.Context, raw string) (Res, GuardedArgs[Req], error) {
		scope, scopeErr := scopeFactory.scope(ctx)
		if scopeErr != nil {
			var zero Res
			var payload GuardedArgs[Req]
			payload.ConfigurationID = p.identity
			payload, fault := argsFault(payload, scopeErr)
			return zero, payload, fault
		}
		payload, err := p.Validate(ctx, scope, raw)
		if err != nil {
			var zero Res
			return zero, payload, err
		}
		res, err := next(ctx, payload.Value)
		return res, payload, err
	}
}

// WrapGuardedArgs validates raw arguments and passes the full [GuardedArgs]
// boundary to next.
func WrapGuardedArgs[Req, Res any](
	p *ArgsPipeline[Req],
	scopeFactory ScopeFactory,
	next GuardedArgsHandler[Req, Res],
) func(context.Context, string) (Res, GuardedArgs[Req], error) {
	if p == nil {
		panic("guardy: WrapGuardedArgs requires non-nil ArgsPipeline")
	}
	if next == nil {
		panic("guardy: WrapGuardedArgs requires non-nil next")
	}
	return func(ctx context.Context, raw string) (Res, GuardedArgs[Req], error) {
		scope, scopeErr := scopeFactory.scope(ctx)
		if scopeErr != nil {
			var zero Res
			var payload GuardedArgs[Req]
			payload.ConfigurationID = p.identity
			payload, fault := argsFault(payload, scopeErr)
			return zero, payload, fault
		}
		payload, err := p.Validate(ctx, scope, raw)
		if err != nil {
			var zero Res
			return zero, payload, err
		}
		res, err := next(ctx, payload)
		return res, payload, err
	}
}

// WrapGuardedJSONArgs validates dynamic raw JSON and passes the full
// [GuardedJSONArgs] boundary to next.
func WrapGuardedJSONArgs[Res any](
	p *JSONArgsPipeline,
	scopeFactory ScopeFactory,
	next GuardedJSONArgsHandler[Res],
) func(context.Context, string) (Res, GuardedJSONArgs, error) {
	if p == nil {
		panic("guardy: WrapGuardedJSONArgs requires non-nil JSONArgsPipeline")
	}
	if next == nil {
		panic("guardy: WrapGuardedJSONArgs requires non-nil next")
	}
	return func(ctx context.Context, raw string) (Res, GuardedJSONArgs, error) {
		scope, scopeErr := scopeFactory.scope(ctx)
		if scopeErr != nil {
			var zero Res
			var payload GuardedJSONArgs
			payload.ConfigurationID = p.identity
			payload, fault := jsonArgsFault(payload, scopeErr)
			return zero, payload, fault
		}
		args, err := p.Validate(ctx, scope, raw)
		if err != nil {
			var zero Res
			return zero, args, err
		}
		res, err := next(ctx, args)
		return res, args, err
	}
}

// WrapGuardedOutput validates next's output and returns a guarded output contract.
// ScopeFactory obtains current facts after next succeeds, immediately before validation.
// Handler errors suppress the partial value and do not call the scope factory.
func WrapGuardedOutput[Req, Res any](
	p *Pipeline[Res],
	scopeFactory ScopeFactory,
	next Handler[Req, Res],
) func(context.Context, Req) (GuardedDelivery[Res], error) {
	if p == nil {
		panic("guardy: WrapGuardedOutput requires non-nil Pipeline")
	}
	if next == nil {
		panic("guardy: WrapGuardedOutput requires non-nil next")
	}
	return func(ctx context.Context, req Req) (GuardedDelivery[Res], error) {
		res, err := next(ctx, req)
		if err != nil {
			var zero Res
			return GuardedDelivery[Res]{
				ConfigurationID: p.name,
				Value:           zero,
				Kind:            PayloadSafeUserText,
				Decision:        DecisionFromReport(nil),
				Reports:         nil,
				Deliverable:     false,
				Channel:         "",
				Fallback:        false,
			}, err
		}
		scope, scopeErr := scopeFactory.scope(ctx)
		if scopeErr != nil {
			decision := DecisionFromReport(nil)
			if failure, ok := errors.AsType[*PolicyFailure](scopeErr); ok {
				decision = failure.Decision
			}
			var output GuardedDelivery[Res]
			output.ConfigurationID, output.Decision = p.name, decision
			return output, scopeErr
		}
		return p.GuardOutput(ctx, scope, res)
	}
}
