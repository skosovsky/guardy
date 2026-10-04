package guardy

import (
	"context"
)

// WrapInput constructs current caller-owned facts through scopeFactory, then runs
// the pipeline on the request value before calling next. Nil factories mean empty scope.
// Deadlines and cancellation come from ctx (e.g. wrap with [context.WithTimeout] at the call site); no implicit timeout is applied.
//
// On ActionRedact, next receives the mutated Output from the pipeline.
// Terminal deny ([Report.IsTerminalDeny]) returns *BlockError (unwraps to [ErrBlocked]; use [errors.As] for Disposition).
// Retryable correction ([Report.IsRetryableCorrection]) returns *RetryError (unwraps to [ErrRetryRequested]).
//
// Composing WrapOutput(outPipeline, WrapInput(inPipeline, fn)) yields input and output guards around fn without net/http.
func WrapInput[Req, Res any](
	p *Pipeline[Req],
	scopeFactory ScopeFactory,
	next func(context.Context, Req) (Res, error),
) func(context.Context, Req) (Res, error) {
	if p == nil {
		panic("guardy: WrapInput requires non-nil Pipeline")
	}
	if next == nil {
		panic("guardy: WrapInput requires non-nil next")
	}
	return func(ctx context.Context, req Req) (Res, error) {
		scope, err := scopeFactory.scope(ctx)
		if err != nil {
			var zero Res
			return zero, err
		}
		result, err := p.Run(ctx, scope, req)
		if err != nil {
			var zero Res
			return zero, err
		}
		rep := result.Decision()
		if decErr := errorFromDecision(rep); decErr != nil {
			var zero Res
			return zero, decErr
		}
		return next(ctx, result.Output)
	}
}

// WrapOutput runs next first, then constructs current caller-owned facts through
// scopeFactory and validates the result. Facts are refreshed after handler execution,
// including any host-controlled pause. Nil factories mean empty scope.
// If next returns a non-nil error, the result is (res, err) with res as returned by next (possibly a zero value, e.g. nil for pointer types).
// Deadlines for the output pipeline use the same ctx as for next.
//
// On ActionRedact, the returned value is the pipeline Output (mutated).
// Terminal deny and retryable correction follow the same disposition routing as [WrapInput].
func WrapOutput[Req, Res any](
	p *Pipeline[Res],
	scopeFactory ScopeFactory,
	next func(context.Context, Req) (Res, error),
) func(context.Context, Req) (Res, error) {
	if p == nil {
		panic("guardy: WrapOutput requires non-nil Pipeline")
	}
	if next == nil {
		panic("guardy: WrapOutput requires non-nil next")
	}
	return func(ctx context.Context, req Req) (Res, error) {
		res, err := next(ctx, req)
		if err != nil {
			return res, err
		}
		scope, err := scopeFactory.scope(ctx)
		if err != nil {
			var zero Res
			return zero, err
		}
		result, err := p.Run(ctx, scope, res)
		if err != nil {
			var zero Res
			return zero, err
		}
		rep := result.Decision()
		if decErr := errorFromDecision(rep); decErr != nil {
			var zero Res
			return zero, decErr
		}
		return result.Output, nil
	}
}
