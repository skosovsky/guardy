package guardytest

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestFakeValidator(t *testing.T) {
	v := FakeValidator("fake", &guardy.Report{Action: guardy.ActionBlock, Reason: "X"})
	ctx := context.Background()
	rep, err := v.Validate(ctx, "any")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock || rep.Reason != "X" {
		t.Errorf("got %+v", rep)
	}
	if v.Name() != "fake" {
		t.Errorf("Name = %s", v.Name())
	}
}

func TestFakeValidator_zeroReportYieldsPass(t *testing.T) {
	v := FakeValidator("nilFake", nil)
	ctx := context.Background()
	rep, err := v.Validate(ctx, "any")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Errorf("zero report should yield pass, got Action=%s", rep.Action)
	}
	if rep.Validator != "nilFake" {
		t.Errorf("Validator = %q", rep.Validator)
	}
}

func TestFailingValidator(t *testing.T) {
	e := errors.New("fail")
	v := FailingValidator("fail", e)
	ctx := context.Background()
	_, err := v.Validate(ctx, "any")
	if err != e {
		t.Errorf("err = %v", err)
	}
}

func TestMustPass(t *testing.T) {
	report := guardy.Report{Action: guardy.ActionPass}
	MustPass(t, &report)
}

func TestMustBlock(t *testing.T) {
	report := guardy.Report{Action: guardy.ActionBlock}
	MustBlock(t, &report)
}

func TestMustRedact(t *testing.T) {
	report := guardy.Report{Action: guardy.ActionRedact}
	MustRedact(t, &report)
}

func TestPipelineWithFakeValidator(t *testing.T) {
	v := FakeValidator("block", &guardy.Report{Action: guardy.ActionBlock, Reason: "TEST"})
	p := guardy.NewPipeline(guardy.WithFastPath(v))
	report, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	MustBlock(t, &report)
}
