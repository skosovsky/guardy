package jsonschema

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestJSONSchemaValidator_Valid_Pass(t *testing.T) {
	j, err := NewJSONSchemaValidator(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, rep, err := j.Validate(ctx, `{"name": "alice"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionPass {
		t.Errorf("got Action=%v, want ActionPass", rep)
	}
}

func TestJSONSchemaValidator_Invalid_RetryWithFeedback(t *testing.T) {
	j, err := NewJSONSchemaValidator(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, rep, err := j.Validate(ctx, `{"age": 42}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionRetry {
		t.Errorf("got Action=%v, want ActionRetry", rep)
	}
	if rep.Feedback == "" {
		t.Error("Feedback should contain schema error details for LLM")
	}
}
