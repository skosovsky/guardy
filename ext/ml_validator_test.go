package ext

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/guardy"
)

type stubClassifier struct {
	result ClassifierResult
	err    error
}

func (s stubClassifier) Classify(_ context.Context, _ string) (ClassifierResult, error) {
	return s.result, s.err
}

func TestClassifierValidator_Pass(t *testing.T) {
	v := MustClassifierValidator(stubClassifier{
		result: ClassifierResult{IsViolation: false},
	}, WithCode("PROMPT_INJECTION_ML"))

	_, rep, err := v.Validate(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Fatalf("action = %v", rep.Action)
	}
}

func TestClassifierValidator_Block(t *testing.T) {
	v := MustClassifierValidator(stubClassifier{
		result: ClassifierResult{
			IsViolation: true,
			Score:       0.93,
			Label:       "prompt_injection",
		},
	}, WithSeverity(guardy.SeverityCritical))

	_, rep, err := v.Validate(context.Background(), "inject")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Fatalf("action = %v", rep.Action)
	}
	if rep.Score != 0.93 {
		t.Fatalf("score = %v", rep.Score)
	}
	if rep.Code != "prompt_injection" {
		t.Fatalf("code = %q", rep.Code)
	}
	if rep.Severity != guardy.SeverityCritical {
		t.Fatalf("severity = %q", rep.Severity)
	}
	if rep.Reason != "classifier violation: prompt_injection" {
		t.Fatalf("reason = %q", rep.Reason)
	}
}

func TestClassifierValidator_Error(t *testing.T) {
	want := errors.New("classifier down")
	v := MustClassifierValidator(stubClassifier{err: want})
	_, _, err := v.Validate(context.Background(), "x")
	if !errors.Is(err, want) {
		t.Fatalf("err = %v", err)
	}
}

func TestClassifierValidator_Block_CustomReasonOverridesLabel(t *testing.T) {
	v := MustClassifierValidator(
		stubClassifier{
			result: ClassifierResult{
				IsViolation: true,
				Label:       "toxicity",
			},
		},
		WithReason("policy violation"),
	)
	_, rep, err := v.Validate(context.Background(), "text")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Reason != "policy violation" {
		t.Fatalf("reason = %q", rep.Reason)
	}
}
