package jsonschema

import (
	"context"
	"strings"
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

func TestJSONSchemaValidator_InvalidJSON_RetryWithFeedback(t *testing.T) {
	j, err := NewJSONSchemaValidator(`{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`)
	if err != nil {
		t.Fatal(err)
	}

	_, rep, err := j.Validate(context.Background(), `{"name":}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionRetry {
		t.Fatalf("got Action=%v, want ActionRetry", rep)
	}
	if rep.Reason != "invalid JSON" {
		t.Fatalf("Reason = %q, want invalid JSON", rep.Reason)
	}
	if rep.Feedback == "" {
		t.Fatal("Feedback should contain parse error details")
	}
}

func TestNewValidatorFromStruct_Invalid_RetryWithFeedback(t *testing.T) {
	type User struct {
		Name string `json:"name" jsonschema:"required,minLength=5"`
	}

	j, err := NewValidatorFromStruct(&User{})
	if err != nil {
		t.Fatal(err)
	}

	_, rep, err := j.Validate(context.Background(), `{"name":"abc"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionRetry {
		t.Fatalf("got Action=%v, want ActionRetry", rep)
	}
	if !strings.Contains(strings.ToLower(rep.Feedback), "name") {
		t.Fatalf("Feedback = %q, want field name", rep.Feedback)
	}
	if !strings.Contains(rep.Feedback, "5") {
		t.Fatalf("Feedback = %q, want min length details", rep.Feedback)
	}
}

func TestNewValidatorFromStruct_EmbeddedStruct_Pass(t *testing.T) {
	type BaseResponse struct {
		Status string `json:"status" jsonschema:"required"`
	}
	type Profile struct {
		Nick string `json:"nick" jsonschema:"required,minLength=2"`
	}
	type UserResponse struct {
		BaseResponse
		Profile Profile `json:"profile" jsonschema:"required"`
	}

	j, err := NewValidatorFromStruct(&UserResponse{})
	if err != nil {
		t.Fatal(err)
	}

	_, rep, err := j.Validate(context.Background(), `{"status":"ok","profile":{"nick":"ab"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionPass {
		t.Fatalf("got Action=%v, want ActionPass", rep)
	}
}

func TestNewValidatorFromStruct_InvalidInput(t *testing.T) {
	type User struct {
		Name string `json:"name"`
	}

	var nilUser *User
	tests := []struct {
		name  string
		input any
	}{
		{name: "nil", input: nil},
		{name: "nil pointer", input: nilUser},
		{name: "map", input: map[string]string{}},
		{name: "slice", input: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewValidatorFromStruct(tt.input)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
