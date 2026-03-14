// Package guardytest provides test helpers for guardy pipelines and validators.
package guardytest

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

// FakeValidator returns a validator that always returns the given report.
// If report is nil or the zero value, it returns Action: pass.
func FakeValidator(name string, report *guardy.Report) guardy.Validator {
	return &fakeValidator{name: name, report: report}
}

// FailingValidator returns a validator that always returns the given error.
func FailingValidator(name string, err error) guardy.Validator {
	return &failingValidator{name: name, err: err}
}

// MustPass asserts that report.Action == ActionPass. It calls t.Fatal on failure.
func MustPass(t testing.TB, report *guardy.Report) {
	t.Helper()
	if report.Action != guardy.ActionPass {
		t.Fatalf("expected Action pass, got %s", report.Action)
	}
}

// MustBlock asserts that report.Action == ActionBlock. It calls t.Fatal on failure.
func MustBlock(t testing.TB, report *guardy.Report) {
	t.Helper()
	if report.Action != guardy.ActionBlock {
		t.Fatalf("expected Action block, got %s", report.Action)
	}
}

// MustRedact asserts that report.Action == ActionRedact. It calls t.Fatal on failure.
func MustRedact(t testing.TB, report *guardy.Report) {
	t.Helper()
	if report.Action != guardy.ActionRedact {
		t.Fatalf("expected Action redact, got %s", report.Action)
	}
}

type fakeValidator struct {
	name   string
	report *guardy.Report
}

func (f *fakeValidator) Validate(context.Context, string) (guardy.Report, error) {
	var rep guardy.Report
	if f.report != nil {
		rep = *f.report
	}
	if rep.Action == "" {
		rep.Action = guardy.ActionPass
	}
	rep.Validator = f.name
	return rep, nil
}

func (f *fakeValidator) Name() string {
	return f.name
}

type failingValidator struct {
	name string
	err  error
}

func (f *failingValidator) Validate(context.Context, string) (guardy.Report, error) {
	return guardy.Report{}, f.err
}

func (f *failingValidator) Name() string {
	return f.name
}
