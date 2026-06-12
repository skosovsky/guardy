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
	_, rep, err := v.Validate(ctx, "any")
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionBlock || rep.Reason != "X" {
		t.Errorf("got %+v", rep)
	}
	if rep != nil && rep.Validator != "fake" {
		t.Errorf("Validator = %q", rep.Validator)
	}
}

func TestFakeValidator_zeroReportYieldsPass(t *testing.T) {
	v := FakeValidator("nilFake", nil)
	ctx := context.Background()
	_, rep, err := v.Validate(ctx, "any")
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionPass {
		t.Errorf("zero report should yield pass, got Action=%v", rep)
	}
	if rep != nil && rep.Validator != "nilFake" {
		t.Errorf("Validator = %q", rep.Validator)
	}
}

func TestFailingValidator(t *testing.T) {
	e := errors.New("fail")
	v := FailingValidator("fail", e)
	ctx := context.Background()
	_, _, err := v.Validate(ctx, "any")
	if !errors.Is(err, e) {
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

func TestMustRetry(t *testing.T) {
	report := guardy.Report{Action: guardy.ActionRetry, Retryable: true}
	MustRetry(t, &report)
}

func TestMustTerminalDeny(t *testing.T) {
	report := guardy.FinishReport(
		&guardy.Report{Action: guardy.ActionBlock},
		guardy.ControlSpec{Action: guardy.ActionBlock},
	)
	MustTerminalDeny(t, report)
}

func TestMustRetryableCorrection(t *testing.T) {
	report := guardy.FinishReport(
		&guardy.Report{Action: guardy.ActionRetry, Retryable: true},
		guardy.ControlSpec{Action: guardy.ActionRetry},
	)
	MustRetryableCorrection(t, report)
}

func TestMustSystemFault(t *testing.T) {
	report := guardy.Report{
		Action:      guardy.ActionPass,
		Disposition: guardy.DispositionSystemFault,
	}
	MustSystemFault(t, &report)
}

func TestMustOutputKind(t *testing.T) {
	result := guardy.RunResult[string]{
		Output:     "hello",
		OutputKind: guardy.PayloadTechnicalPayload,
	}
	MustOutputKind(t, result, guardy.PayloadTechnicalPayload)
}

func TestMustScopeIncomplete(t *testing.T) {
	MustScopeIncomplete(t, guardy.ErrScopeIncomplete)
}

func TestMustScopeIncomplete_viaRun(t *testing.T) {
	p := guardy.NewPipeline(
		guardy.WithPolicyValidators(guardy.NewAttributePresent[string]("tenant.id")),
	)
	_, err := p.Run(context.Background(), guardy.MapScope{}, "x")
	MustScopeIncomplete(t, err)
}

func TestPipelineWithFakeValidator(t *testing.T) {
	v := FakeValidator("block", &guardy.Report{Action: guardy.ActionBlock, Reason: "TEST"})
	p := guardy.NewPipeline(guardy.WithFastPath(v))
	result, err := p.Run(context.Background(), nil, "x")
	if err != nil {
		t.Fatal(err)
	}
	MustBlock(t, result.Decision())
}
