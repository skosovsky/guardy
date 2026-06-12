package guardy

import (
	"context"
	"encoding/json"
)

// PostBindValidator is optional domain validation after JSON unmarshal in [ValidateAndDecode].
// Implement on a pointer receiver, for example:
//
//	func (u *User) ValidatePostBind(ctx context.Context) error
type PostBindValidator interface {
	ValidatePostBind(ctx context.Context) error
}

// ValidateAndDecode runs the raw interception pipeline, then unmarshals the output into T.
// Phase Raw: fast + policy + slow on the string payload.
// Phase Structured: [json.Unmarshal] + optional [PostBindValidator].
// Terminal deny returns [*BlockError]; retryable correction returns [*RetryError] (same as [WrapInput]).
func ValidateAndDecode[T any](
	ctx context.Context,
	scope ExecutionScope,
	p *Pipeline[string],
	raw string,
) (*T, *Report, error) {
	if p == nil {
		panic("guardy: ValidateAndDecode requires non-nil Pipeline")
	}
	result, err := p.Run(ctx, scope, raw)
	if err != nil {
		return nil, nil, err
	}
	rep := result.Decision()
	if decErr := errorFromDecision(rep); decErr != nil {
		return nil, rep, decErr
	}
	var v T
	if unmarshalErr := json.Unmarshal([]byte(result.Output), &v); unmarshalErr != nil {
		bindRep := FinishReport(&Report{
			Action:   ActionRetry,
			Code:     CodeJSONInvalid,
			Reason:   "invalid JSON for decode",
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
