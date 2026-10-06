package guardy

import (
	"context"
	"errors"
)

var errLLMJudgeNil = errors.New("guardy: llm judge is nil")

// Judge evaluates text (e.g. via LLM) and returns a Report.
type Judge interface {
	Evaluate(ctx context.Context, text string) (Report, error)
}

// LLMJudge is a Parallel validator that delegates to a Judge.
type LLMJudge struct {
	judge  Judge
	shadow bool
	name   string
}

// NewLLMJudge builds a validator that calls j.Evaluate.
// If shadow is true and the judge returns block, the report is marked ShadowMode.
func NewLLMJudge(j Judge, shadow bool) (*LLMJudge, error) {
	if nilImplementation(j) {
		return nil, configurationError("llm_judge", "judge", "nil")
	}
	return &LLMJudge{judge: j, shadow: shadow, name: "llm_judge"}, nil
}

// MustLLMJudge builds a judge validator or panics on NewLLMJudge's configuration error.
func MustLLMJudge(j Judge, shadow bool) *LLMJudge {
	v, err := NewLLMJudge(j, shadow)
	if err != nil {
		panic(err)
	}
	return v
}

// Validate runs the judge and returns its result.
// Context is checked before/after the synchronous judge. Cancellation prevents a
// late report; it cannot terminate a non-cooperative judge or undo its side effects.
func (l *LLMJudge) Validate(ctx context.Context, input string) (string, *Report, error) {
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	if nilImplementation(l.judge) {
		return "", nil, errLLMJudgeNil
	}
	rep, err := l.judge.Evaluate(ctx, input)
	if ctxErr := callbackCancellation(ctx, err); ctxErr != nil {
		return input, nil, ctxErr
	}
	if err != nil {
		return input, nil, err
	}
	if l.shadow && rep.Action == ActionBlock {
		rep.ShadowMode = true
	}
	if rep.Validator == "" {
		rep.Validator = l.name
	}
	out := normalizeReport(&rep)
	return input, &out, nil
}
