// Package guardytest provides test helpers for guardy pipelines and validators.
package guardytest

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

// FakeValidator returns a validator that always returns the given report.
// If report is nil or the zero value, it returns Action: pass.
func FakeValidator(name string, report *guardy.Report) guardy.Validator[string] {
	return &fakeValidator{name: name, report: report}
}

// FailingValidator returns a validator that always returns the given error.
func FailingValidator(name string, err error) guardy.Validator[string] {
	return &failingValidator{name: name, err: err}
}

// MustPass asserts that report.Action == ActionPass. It calls t.Fatal on failure.
func MustPass(t testing.TB, report *guardy.Report) {
	t.Helper()
	if report == nil || report.Action != guardy.ActionPass {
		t.Fatalf("expected Action pass, got %v", report)
	}
}

// MustBlock asserts that report.Action == ActionBlock. It calls t.Fatal on failure.
func MustBlock(t testing.TB, report *guardy.Report) {
	t.Helper()
	if report == nil || report.Action != guardy.ActionBlock {
		t.Fatalf("expected Action block, got %v", report)
	}
}

// MustRedact asserts that report.Action == ActionRedact. It calls t.Fatal on failure.
func MustRedact(t testing.TB, report *guardy.Report) {
	t.Helper()
	if report == nil || report.Action != guardy.ActionRedact {
		t.Fatalf("expected Action redact, got %v", report)
	}
}

// MustRetry asserts that report indicates a retryable orchestrator correction.
func MustRetry(t testing.TB, report *guardy.Report) {
	t.Helper()
	if report == nil || !report.ShouldRetry() {
		t.Fatalf("expected retryable report, got %+v", report)
	}
}

type fakeValidator struct {
	name   string
	report *guardy.Report
}

func (f *fakeValidator) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	var rep guardy.Report
	if f.report != nil {
		rep = *f.report
	}
	rep.Validator = f.name
	if rep.Action == 0 {
		rep.Action = guardy.ActionPass
	}
	guardy.FinishReport(&rep, guardy.ControlSpec{Action: rep.Action})
	if rep.Action == guardy.ActionRedact && rep.MutatedText != "" {
		return rep.MutatedText, &rep, nil
	}
	return input, &rep, nil
}

type failingValidator struct {
	name string
	err  error
}

func (f *failingValidator) Validate(context.Context, string) (string, *guardy.Report, error) {
	return "", nil, f.err
}
