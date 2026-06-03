package guardy

import (
	"context"
	"encoding/json"
	"fmt"
)

// PostBindValidator is optional domain validation after JSON unmarshal in [ValidateAndBind].
// Implement on a pointer receiver, for example:
//
//	func (u *User) ValidatePostBind(ctx context.Context) error
type PostBindValidator interface {
	ValidatePostBind(ctx context.Context) error
}

// ValidateAndBind runs the string pipeline, then unmarshals the output into T.
// If T implements [PostBindValidator] (via pointer receiver on *T), ValidatePostBind runs last.
// Block and Retry follow the same error style as [WrapInput] ([ErrBlocked], [*RetryError]).
// The returned Report is the pipeline decision on success; post-bind failures return a bind Report with [CodePostBindViolation].
func ValidateAndBind[T any](ctx context.Context, p *Pipeline[string], raw string) (*T, *Report, error) {
	if p == nil {
		panic("guardy: ValidateAndBind requires non-nil Pipeline")
	}
	result, err := p.Run(ctx, raw)
	if err != nil {
		return nil, nil, err
	}
	rep := result.Decision()
	if rep.ShouldStop() {
		return nil, rep, fmt.Errorf("%w: %s", ErrBlocked, rep.PublicMessage())
	}
	if rep.ShouldRetry() {
		return nil, rep, &RetryError{Feedback: rep.OrchestratorMessage(), Report: *rep}
	}
	var v T
	if unmarshalErr := json.Unmarshal([]byte(result.Output), &v); unmarshalErr != nil {
		bindRep := FinishReport(&Report{
			Action:   ActionRetry,
			Code:     CodeJSONInvalid,
			Reason:   "invalid JSON for bind",
			Feedback: unmarshalErr.Error(),
		}, ControlSpec{Action: ActionRetry})
		return nil, bindRep, &RetryError{Feedback: bindRep.OrchestratorMessage(), Report: *bindRep}
	}
	if err := invokePostBind(ctx, &v); err != nil {
		bindRep := FinishReport(&Report{
			Action:   ActionRetry,
			Code:     CodePostBindViolation,
			Reason:   err.Error(),
			Feedback: err.Error(),
		}, ControlSpec{Action: ActionRetry})
		return nil, bindRep, &RetryError{Feedback: bindRep.OrchestratorMessage(), Report: *bindRep}
	}
	return &v, rep, nil
}

func invokePostBind(ctx context.Context, v any) error {
	pb, ok := v.(PostBindValidator)
	if !ok {
		return nil
	}
	return pb.ValidatePostBind(ctx)
}
