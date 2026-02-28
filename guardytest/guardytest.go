// Package guardytest provides test helpers for guardy pipelines and validators.
package guardytest

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

// FakeValidator returns a validator that always returns the given result.
// If result is nil, a zero Result is used.
func FakeValidator(name string, result *guardy.Result) guardy.Validator {
	var r guardy.Result
	if result != nil {
		r = *result
	}
	return &fakeValidator{name: name, result: r}
}

// FailingValidator returns a validator that always returns the given error.
func FailingValidator(name string, err error) guardy.Validator {
	return &failingValidator{name: name, err: err}
}

// MustPass asserts that report.FinalAction == Pass. It calls t.Fatal on failure.
func MustPass(t testing.TB, report guardy.Report) {
	t.Helper()
	if report.FinalAction != guardy.Pass {
		t.Fatalf("expected FinalAction Pass, got %s", report.FinalAction)
	}
}

// MustBlock asserts that report.FinalAction == Block. It calls t.Fatal on failure.
func MustBlock(t testing.TB, report guardy.Report) {
	t.Helper()
	if report.FinalAction != guardy.Block {
		t.Fatalf("expected FinalAction Block, got %s", report.FinalAction)
	}
}

// MustRedact asserts that report.FinalAction == Redact. It calls t.Fatal on failure.
func MustRedact(t testing.TB, report guardy.Report) {
	t.Helper()
	if report.FinalAction != guardy.Redact {
		t.Fatalf("expected FinalAction Redact, got %s", report.FinalAction)
	}
}

// MustOverride asserts that report.FinalAction == Override. It calls t.Fatal on failure.
func MustOverride(t testing.TB, report guardy.Report) {
	t.Helper()
	if report.FinalAction != guardy.Override {
		t.Fatalf("expected FinalAction Override, got %s", report.FinalAction)
	}
}

// MustRetry asserts that report.FinalAction == Retry. It calls t.Fatal on failure.
func MustRetry(t testing.TB, report guardy.Report) {
	t.Helper()
	if report.FinalAction != guardy.Retry {
		t.Fatalf("expected FinalAction Retry, got %s", report.FinalAction)
	}
}

// NewInput returns an Input with the given text (convenience for tests).
func NewInput(text string) guardy.Input {
	return guardy.Input{Text: text}
}

// InputBuilder builds guardy.Input for table-driven tests.
type InputBuilder struct {
	in guardy.Input
}

// NewInputBuilder returns a builder for Input.
func NewInputBuilder() *InputBuilder {
	return &InputBuilder{}
}

// Text sets the input text.
func (b *InputBuilder) Text(s string) *InputBuilder {
	b.in.Text = s
	return b
}

// Metadata sets the input metadata.
func (b *InputBuilder) Metadata(m map[string]any) *InputBuilder {
	b.in.Metadata = m
	return b
}

// Documents sets the input documents (RAG context).
func (b *InputBuilder) Documents(docs []guardy.Document) *InputBuilder {
	b.in.Documents = docs
	return b
}

// Messages sets the conversation history for context-aware validators.
func (b *InputBuilder) Messages(msgs []guardy.Message) *InputBuilder {
	b.in.Messages = msgs
	return b
}

// Build returns the built Input.
func (b *InputBuilder) Build() guardy.Input {
	return b.in
}

type fakeValidator struct {
	name   string
	result guardy.Result
}

func (f *fakeValidator) Validate(context.Context, guardy.Input) (guardy.Result, error) {
	return f.result, nil
}

func (f *fakeValidator) Name() string {
	return f.name
}

type failingValidator struct {
	name string
	err  error
}

func (f *failingValidator) Validate(context.Context, guardy.Input) (guardy.Result, error) {
	return guardy.Result{}, f.err
}

func (f *failingValidator) Name() string {
	return f.name
}
