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
	r, err := v.Validate(ctx, guardy.Input{Text: "any"})
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

func TestFailingValidator(t *testing.T) {
	e := errors.New("fail")
	v := FailingValidator("fail", e)
	ctx := context.Background()
	_, err := v.Validate(ctx, guardy.Input{})
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
	if in.Text != "hello" {
		t.Errorf("Text = %q", in.Text)
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
	docs := []guardy.Document{{ID: "1", Content: "c", Metadata: map[string]string{"k": "v"}}}
	meta := map[string]any{"key": "value"}
	in := NewInputBuilder().
		Text("hello").
		Metadata(meta).
		Documents(docs).
		Build()
	if in.Text != "hello" {
		t.Errorf("Text = %q", in.Text)
	}
	if in.Metadata["key"] != "value" {
		t.Errorf("Metadata = %v", in.Metadata)
	}
	if len(in.Documents) != 1 || in.Documents[0].ID != "1" || in.Documents[0].Content != "c" {
		t.Errorf("Documents = %v", in.Documents)
	}
}
