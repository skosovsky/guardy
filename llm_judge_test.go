package guardy

import (
	"context"
	"testing"
)

type fakeJudge struct {
	eval func(context.Context, string) (Report, error)
}

func (f fakeJudge) Evaluate(ctx context.Context, text string) (Report, error) {
	return f.eval(ctx, text)
}

func TestLLMJudge_PassthroughPreservesFields(t *testing.T) {
	j := fakeJudge{
		eval: func(context.Context, string) (Report, error) {
			return Report{
				Action:     ActionBlock,
				Validator:  "judge_impl",
				Reason:     "policy_violation",
				Score:      0.7,
				ShadowMode: false,
			}, nil
		},
	}
	v := NewLLMJudge(j, true)

	_, rep, err := v.Validate(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Validator != "judge_impl" {
		t.Errorf("Validator = %q, want judge_impl", rep.Validator)
	}
	if rep.Reason != "policy_violation" || rep.Score != 0.7 {
		t.Errorf("Reason=%q Score=%v", rep.Reason, rep.Score)
	}
	if !rep.ShadowMode {
		t.Error("ShadowMode should be true when shadow is enabled and action is block")
	}
}

func TestLLMJudge_FillsValidatorWhenEmpty(t *testing.T) {
	j := fakeJudge{
		eval: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass}, nil
		},
	}
	v := NewLLMJudge(j, false)

	_, rep, err := v.Validate(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Validator != "llm_judge" {
		t.Errorf("Validator = %q, want llm_judge", rep.Validator)
	}
}

func TestLLMJudge_NilJudge_ReturnsError(t *testing.T) {
	v := NewLLMJudge(nil, false)
	_, _, err := v.Validate(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
}
