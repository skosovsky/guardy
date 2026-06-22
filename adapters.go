package guardy

import "context"

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
	scope ExecutionScope,
	next Handler[Req, Res],
) func(context.Context, string) (Res, GuardedArgs[Req], error) {
	if p == nil {
		panic("guardy: WrapArgs requires non-nil ArgsPipeline")
	}
	if next == nil {
		panic("guardy: WrapArgs requires non-nil next")
	}
	return func(ctx context.Context, raw string) (Res, GuardedArgs[Req], error) {
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
	scope ExecutionScope,
	next GuardedArgsHandler[Req, Res],
) func(context.Context, string) (Res, GuardedArgs[Req], error) {
	if p == nil {
		panic("guardy: WrapGuardedArgs requires non-nil ArgsPipeline")
	}
	if next == nil {
		panic("guardy: WrapGuardedArgs requires non-nil next")
	}
	return func(ctx context.Context, raw string) (Res, GuardedArgs[Req], error) {
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
	scope ExecutionScope,
	next GuardedJSONArgsHandler[Res],
) func(context.Context, string) (Res, GuardedJSONArgs, error) {
	if p == nil {
		panic("guardy: WrapGuardedJSONArgs requires non-nil JSONArgsPipeline")
	}
	if next == nil {
		panic("guardy: WrapGuardedJSONArgs requires non-nil next")
	}
	return func(ctx context.Context, raw string) (Res, GuardedJSONArgs, error) {
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
func WrapGuardedOutput[Req, Res any](
	p *Pipeline[Res],
	scope ExecutionScope,
	next Handler[Req, Res],
) func(context.Context, Req) (GuardedOutput[Res], error) {
	if p == nil {
		panic("guardy: WrapGuardedOutput requires non-nil Pipeline")
	}
	if next == nil {
		panic("guardy: WrapGuardedOutput requires non-nil next")
	}
	return func(ctx context.Context, req Req) (GuardedOutput[Res], error) {
		res, err := next(ctx, req)
		if err != nil {
			var zero Res
			return GuardedOutput[Res]{
				Value:       zero,
				Kind:        PayloadSafeUserText,
				Decision:    DecisionFromReport(nil),
				Reports:     nil,
				Deliverable: false,
				Channel:     "",
				Fallback:    false,
			}, err
		}
		return p.GuardOutput(ctx, scope, res)
	}
}
