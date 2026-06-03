package guardy

import (
	"context"
	"fmt"
)

// WrapInput runs the pipeline on the request value before calling next.
// Deadlines and cancellation come from ctx (e.g. wrap with [context.WithTimeout] at the call site); no implicit timeout is applied.
//
// On ActionRedact, next receives the mutated Output from the pipeline.
// On ActionBlock, next is not called; the error wraps ErrBlocked (use [errors.Is]).
// On ActionRetry, next is not called; the error is *RetryError (use [errors.As]), which unwraps to ErrRetryRequested.
//
// Composing WrapOutput(outPipeline, WrapInput(inPipeline, fn)) yields input and output guards around fn without net/http.
func WrapInput[Req, Res any](
	p *Pipeline[Req],
	next func(context.Context, Req) (Res, error),
) func(context.Context, Req) (Res, error) {
	if p == nil {
		panic("guardy: WrapInput requires non-nil Pipeline")
	}
	if next == nil {
		panic("guardy: WrapInput requires non-nil next")
	}
	return func(ctx context.Context, req Req) (Res, error) {
		result, err := p.Run(ctx, req)
		if err != nil {
			var zero Res
			return zero, err
		}
		rep := result.Decision()
		switch rep.Action {
		case ActionBlock:
			var zero Res
			return zero, fmt.Errorf("%w: %s", ErrBlocked, rep.PublicMessage())
		case ActionRetry:
			var zero Res
			return zero, &RetryError{Feedback: rep.OrchestratorMessage(), Report: *rep}
		case ActionPass, ActionRedact:
			return next(ctx, result.Output)
		default:
			var zero Res
			return zero, fmt.Errorf(
				"%w: unsupported pipeline action %s in WrapInput",
				ErrValidatorFailed,
				rep.Action.String(),
			)
		}
	}
}

// WrapOutput runs next first, then validates the result with the pipeline.
// If next returns a non-nil error, the result is (res, err) with res as returned by next (possibly a zero value, e.g. nil for pointer types).
// Deadlines for the output pipeline use the same ctx as for next.
//
// On ActionRedact, the returned value is the pipeline Output (mutated).
// On ActionBlock or ActionRetry, behavior matches WrapInput (errors and no replacement of a successful next result in the success path — the error is returned instead).
func WrapOutput[Req, Res any](
	p *Pipeline[Res],
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
		result, err := p.Run(ctx, res)
		if err != nil {
			var zero Res
			return zero, err
		}
		rep := result.Decision()
		switch rep.Action {
		case ActionBlock:
			var zero Res
			return zero, fmt.Errorf("%w: %s", ErrBlocked, rep.PublicMessage())
		case ActionRetry:
			var zero Res
			return zero, &RetryError{Feedback: rep.OrchestratorMessage(), Report: *rep}
		case ActionPass, ActionRedact:
			return result.Output, nil
		default:
			var zero Res
			return zero, fmt.Errorf(
				"%w: unsupported pipeline action %s in WrapOutput",
				ErrValidatorFailed,
				rep.Action.String(),
			)
		}
	}
}
