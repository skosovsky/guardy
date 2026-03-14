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

// Name returns the validator name.
func (l *LLMJudge) Name() string { return l.name }

// Validate runs the judge and returns its result.
func (l *LLMJudge) Validate(ctx context.Context, text string) (Report, error) {
	if l.judge == nil {
		return Report{}, errLLMJudgeNil
	}
	rep, err := l.judge.Evaluate(ctx, text)
	if err != nil {
		return Report{}, err
	}
	if l.shadow && rep.Action == ActionBlock {
		rep.ShadowMode = true
	}
	if rep.Validator == "" {
		rep.Validator = l.name
	}
	return rep, nil
}
