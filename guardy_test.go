package guardy

import (
	"context"
	"testing"
)

func TestAction_String(t *testing.T) {
	if got := string(Block); got != "block" {
		t.Errorf("Block = %q, want block", got)
	}
	if got := string(Pass); got != "pass" {
		t.Errorf("Pass = %q, want pass", got)
	}
}

func TestResult_ZeroValue(t *testing.T) {
	var r Result
	if r.Passed != false {
		t.Error("zero Result should have Passed false")
	}
	if r.Action != "" {
		t.Errorf("zero Result Action = %q", r.Action)
	}
}

func TestInput_ZeroValue(t *testing.T) {
	var in Input
	if in.Text != "" {
		t.Error("zero Input should have empty Text")
	}
	if in.Metadata != nil {
		t.Error("zero Input Metadata should be nil")
	}
	if in.Documents != nil {
		t.Error("zero Input Documents should be nil")
	}
}

func TestReport_ZeroValue(t *testing.T) {
	var rep Report
	if rep.Results != nil {
		t.Error("zero Report Results should be nil")
	}
	if rep.FinalAction != "" {
		t.Errorf("zero Report FinalAction = %q", rep.FinalAction)
	}
}

func TestConditionalValidator_SkipsWhenPredicateFalse(t *testing.T) {
	called := false
	inner := &fakeValidator{
		name: "inner",
		validate: func(context.Context, Input) (Result, error) {
			called = true
			return Result{Passed: false, Action: Block, Code: "TEST"}, nil
		},
	}
	cv := &ConditionalValidator{
		Validator: inner,
		Predicate: func(Input) bool { return false },
	}
	ctx := context.Background()
	in := Input{Text: "hello"}
	r, err := cv.Validate(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("inner validator should not have been called")
	}
	if !r.Passed || r.Action != Pass {
		t.Errorf("expected Pass, got Passed=%v Action=%s", r.Passed, r.Action)
	}
}

func TestConditionalValidator_RunsWhenPredicateTrue(t *testing.T) {
	inner := &fakeValidator{
		name: "inner",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: false, Action: Block, Code: "TEST"}, nil
		},
	}
	cv := &ConditionalValidator{
		Validator: inner,
		Predicate: func(Input) bool { return true },
	}
	ctx := context.Background()
	in := Input{Text: "hello"}
	r, err := cv.Validate(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed || r.Action != Block || r.Code != "TEST" {
		t.Errorf("got Passed=%v Action=%s Code=%s", r.Passed, r.Action, r.Code)
	}
}

func TestConditionalValidator_NilPredicateRunsAlways(t *testing.T) {
	inner := &fakeValidator{
		name: "inner",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: false, Action: Redact, Code: "REDACT"}, nil
		},
	}
	cv := &ConditionalValidator{Validator: inner, Predicate: nil}
	ctx := context.Background()
	r, err := cv.Validate(ctx, Input{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed || r.Action != Redact {
		t.Errorf("got Passed=%v Action=%s", r.Passed, r.Action)
	}
}

func TestConditionalValidator_Name(t *testing.T) {
	inner := &fakeValidator{name: "inner-name"}
	cv := &ConditionalValidator{Validator: inner, Predicate: nil}
	if got := cv.Name(); got != "inner-name" {
		t.Errorf("Name() = %q, want inner-name", got)
	}
}

// fakeValidator is used in tests.
type fakeValidator struct {
	name     string
	validate func(context.Context, Input) (Result, error)
}

func (f *fakeValidator) Validate(ctx context.Context, input Input) (Result, error) {
	if f.validate != nil {
		return f.validate(ctx, input)
	}
	return Result{Passed: true, Action: Pass}, nil
}

func (f *fakeValidator) Name() string { return f.name }
