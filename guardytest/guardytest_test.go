package guardytest

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestFakeValidator(t *testing.T) {
	v := FakeValidator("fake", &guardy.Result{Passed: false, Action: guardy.Block, Code: "X"})
	ctx := context.Background()
	r, err := v.Validate(ctx, NewInput("any"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Passed || r.Action != guardy.Block || r.Code != "X" {
		t.Errorf("got %+v", r)
	}
	if v.Name() != "fake" {
		t.Errorf("Name = %s", v.Name())
	}
}

// TestFakeValidator_nilYieldsZeroResult documents the contract: FakeValidator(name, nil) returns a validator
// that yields zero-value Result (Passed false, Action "", empty strings).
func TestFakeValidator_nilYieldsZeroResult(t *testing.T) {
	v := FakeValidator("nilFake", nil)
	ctx := context.Background()
	r, err := v.Validate(ctx, NewInput("any"))
	if err != nil {
		t.Fatal(err)
	}
	var zero guardy.Result
	if r != zero {
		t.Errorf("FakeValidator(..., nil) must yield zero Result, got %+v", r)
	}
	if r.Passed || r.Action != "" || r.Code != "" || r.Reason != "" || r.Evidence != "" || r.Guidance != "" {
		t.Errorf("expected zero Result: Passed=%v Action=%q Code=%q Reason=%q Evidence=%q Guidance=%q",
			r.Passed, r.Action, r.Code, r.Reason, r.Evidence, r.Guidance)
	}
}

func TestFailingValidator(t *testing.T) {
	e := errors.New("fail")
	v := FailingValidator("fail", e)
	ctx := context.Background()
	_, err := v.Validate(ctx, &guardy.Input{})
	if err != e {
		t.Errorf("err = %v", err)
	}
}

func TestMustPass(t *testing.T) {
	MustPass(t, guardy.Report{FinalAction: guardy.Pass})
}

func TestMustBlock(t *testing.T) {
	MustBlock(t, guardy.Report{FinalAction: guardy.Block})
}

func TestMustRedact(t *testing.T) {
	MustRedact(t, guardy.Report{FinalAction: guardy.Redact})
}

func TestMustOverride(t *testing.T) {
	MustOverride(t, guardy.Report{FinalAction: guardy.Override})
}

func TestMustRetry(t *testing.T) {
	MustRetry(t, guardy.Report{FinalAction: guardy.Retry})
}

func TestNewInput(t *testing.T) {
	in := NewInput("hello")
	if in.Data != "hello" {
		t.Errorf("Data = %q", in.Data)
	}
}

func TestPipelineWithFakeValidator(t *testing.T) {
	v := FakeValidator("block", &guardy.Result{Passed: false, Action: guardy.Block, Code: "TEST"})
	p := guardy.NewPipeline(guardy.WithTier1(v))
	report, err := p.Run(context.Background(), NewInput("x"))
	if err != nil {
		t.Fatal(err)
	}
	MustBlock(t, report)
}

func TestInputBuilder(t *testing.T) {
	meta := map[string]any{"key": "value"}
	in := NewInputBuilder().
		Text("hello").
		Metadata(meta).
		Build()
	if in.Data != "hello" {
		t.Errorf("Data = %q", in.Data)
	}
	if in.Metadata["key"] != "value" {
		t.Errorf("Metadata = %v", in.Metadata)
	}
}

func TestInputBuilder_MetadataOnly(t *testing.T) {
	in := NewInputBuilder().Text("Hi").Build()
	if in.Data != "Hi" {
		t.Errorf("Data = %q", in.Data)
	}
}
