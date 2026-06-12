// Package guardytest provides test helpers for guardy pipelines and validators.
package guardytest

import (
	"context"
	"errors"
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
// Prefer [MustTerminalDeny] for disposition-based control-flow tests (task14 §2.2).
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

// MustTerminalDeny asserts DispositionTerminalDeny.
func MustTerminalDeny(t testing.TB, report *guardy.Report) {
	t.Helper()
	if report == nil || !report.IsTerminalDeny() {
		t.Fatalf("expected terminal deny, got %+v", report)
	}
}

// MustRetryableCorrection asserts DispositionRetryableCorrection.
func MustRetryableCorrection(t testing.TB, report *guardy.Report) {
	t.Helper()
	if report == nil || !report.IsRetryableCorrection() {
		t.Fatalf("expected retryable correction, got %+v", report)
	}
}

// MustSystemFault asserts DispositionSystemFault.
func MustSystemFault(t testing.TB, report *guardy.Report) {
	t.Helper()
	if report == nil || !report.IsSystemFault() {
		t.Fatalf("expected system fault, got %+v", report)
	}
}

// MustOutputKind asserts RunResult.OutputKind.
func MustOutputKind[T any](t testing.TB, result guardy.RunResult[T], kind guardy.PayloadKind) {
	t.Helper()
	if result.OutputKind != kind {
		t.Fatalf("OutputKind = %v, want %v", result.OutputKind, kind)
	}
}

// MustScopeIncomplete asserts ErrScopeIncomplete from Run or scope-aware entry points.
func MustScopeIncomplete(t testing.TB, err error) {
	t.Helper()
	if !errors.Is(err, guardy.ErrScopeIncomplete) {
		t.Fatalf("expected ErrScopeIncomplete, got %v", err)
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
