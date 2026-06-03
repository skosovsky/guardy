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

// LLMJudge is a Slow-Path validator that delegates to a Judge.
type LLMJudge struct {
	judge  Judge
	shadow bool
	name   string
}

// NewLLMJudge builds a validator that calls j.Evaluate.
// If shadow is true and the judge returns block, the report is marked ShadowMode.
func NewLLMJudge(j Judge, shadow bool) *LLMJudge {
	return &LLMJudge{judge: j, shadow: shadow, name: "llm_judge"}
}

// Validate runs the judge and returns its result.
func (l *LLMJudge) Validate(ctx context.Context, input string) (string, *Report, error) {
	if l.judge == nil {
		return "", nil, errLLMJudgeNil
	}
	rep, err := l.judge.Evaluate(ctx, input)
	if err != nil {
		return input, nil, err
	}
	if l.shadow && rep.Action == ActionBlock {
		rep.ShadowMode = true
	}
	if rep.Validator == "" {
		rep.Validator = l.name
	}
	out := rep
	FinishReport(&out, ControlSpec{Action: out.Action})
	return input, &out, nil
}
