package guardy

import "context"

// Handler is a generic host function shape guardy can wrap without owning host types.
type Handler[Req, Res any] func(context.Context, Req) (Res, error)

// WrapArgs validates raw arguments through an [ArgsPipeline] before calling next.
func WrapArgs[Req, Res any](
	p *ArgsPipeline[Req],
	scope ExecutionScope,
	next Handler[Req, Res],
) func(context.Context, string) (Res, GuardedPayload[Req], error) {
	if p == nil {
		panic("guardy: WrapArgs requires non-nil ArgsPipeline")
	}
	if next == nil {
		panic("guardy: WrapArgs requires non-nil next")
	}
	return func(ctx context.Context, raw string) (Res, GuardedPayload[Req], error) {
		payload, err := p.Validate(ctx, scope, raw)
		if err != nil {
			var zero Res
			return zero, payload, err
		}
		res, err := next(ctx, payload.Value)
		return res, payload, err
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
			}, err
		}
		return p.GuardOutput(ctx, scope, res)
	}
}
