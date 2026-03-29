package jsonschema

import (
	"context"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

const testNameSchema = `{"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}`

func TestJSONSchemaValidator_Valid_Pass(t *testing.T) {
	j, err := NewJSONSchemaValidator(testNameSchema)
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
	if rep.Severity != guardy.SeverityMedium {
		t.Fatalf("severity = %q, want %q", rep.Severity, guardy.SeverityMedium)
	}
	if rep.Code != "" {
		t.Fatalf("code = %q, want empty default", rep.Code)
	}
}

func TestJSONSchemaValidator_Valid_PassPreservesConfiguredMetadata(t *testing.T) {
	j, err := NewJSONSchemaValidator(
		testNameSchema,
		ext.WithCode("JSON_SCHEMA_OK"),
		ext.WithSeverity(guardy.SeverityHigh),
		WithJSONSchemaName("jsonschema_custom"),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, rep, err := j.Validate(context.Background(), `{"name":"alice"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionPass {
		t.Fatalf("got Action=%v, want ActionPass", rep)
	}
	if rep.Code != "JSON_SCHEMA_OK" {
		t.Fatalf("code = %q, want %q", rep.Code, "JSON_SCHEMA_OK")
	}
	if rep.Severity != guardy.SeverityHigh {
		t.Fatalf("severity = %q, want %q", rep.Severity, guardy.SeverityHigh)
	}
	if rep.Validator != "jsonschema_custom" {
		t.Fatalf("validator = %q, want %q", rep.Validator, "jsonschema_custom")
	}
}

func TestJSONSchemaValidator_Invalid_RetryWithFeedback(t *testing.T) {
	j, err := NewJSONSchemaValidator(
		testNameSchema,
		ext.WithCode("JSON_SCHEMA_BAD"),
		ext.WithReason("schema mismatch"),
		ext.WithSeverity(guardy.SeverityCritical),
	)
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
	if rep.Code != "JSON_SCHEMA_BAD" {
		t.Fatalf("code = %q, want %q", rep.Code, "JSON_SCHEMA_BAD")
	}
	if rep.Reason != "schema mismatch" {
		t.Fatalf("reason = %q, want %q", rep.Reason, "schema mismatch")
	}
	if rep.Severity != guardy.SeverityCritical {
		t.Fatalf("severity = %q, want %q", rep.Severity, guardy.SeverityCritical)
	}
}

func TestJSONSchemaValidator_InvalidJSON_RetryWithFeedback(t *testing.T) {
	j, err := NewJSONSchemaValidator(
		testNameSchema,
		ext.WithCode("IGNORED_FOR_PARSE"),
		ext.WithReason("ignored for parse"),
		ext.WithSeverity(guardy.SeverityHigh),
		WithJSONSchemaName("jsonschema_custom"),
	)
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
	if rep.Validator != "jsonschema_custom" {
		t.Fatalf("validator = %q, want %q", rep.Validator, "jsonschema_custom")
	}
	if rep.Code != "JSON_INVALID" {
		t.Fatalf("Code = %q, want JSON_INVALID", rep.Code)
	}
	if rep.Reason != "invalid JSON" {
		t.Fatalf("Reason = %q, want invalid JSON", rep.Reason)
	}
	if rep.Severity != guardy.SeverityHigh {
		t.Fatalf("severity = %q, want %q", rep.Severity, guardy.SeverityHigh)
	}
	if rep.Feedback == "" {
		t.Fatal("Feedback should contain parse error details")
	}
}

func TestNewJSONSchemaValidatorFromStruct_Invalid_RetryWithFeedback(t *testing.T) {
	type User struct {
		Name string `json:"name" jsonschema:"required,minLength=5"`
	}

	j, err := NewJSONSchemaValidatorFromStruct(
		&User{},
		ext.WithCode("JSON_SCHEMA_CUSTOM"),
		ext.WithReason("schema mismatch"),
		ext.WithSeverity(guardy.SeverityLow),
		WithJSONSchemaName("jsonschema_custom"),
	)
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
	if rep.Validator != "jsonschema_custom" {
		t.Fatalf("validator = %q, want %q", rep.Validator, "jsonschema_custom")
	}
	if rep.Code != "JSON_SCHEMA_CUSTOM" {
		t.Fatalf("code = %q, want %q", rep.Code, "JSON_SCHEMA_CUSTOM")
	}
	if rep.Severity != guardy.SeverityLow {
		t.Fatalf("severity = %q, want %q", rep.Severity, guardy.SeverityLow)
	}
	if rep.Reason != "schema mismatch" {
		t.Fatalf("reason = %q, want %q", rep.Reason, "schema mismatch")
	}
	if !strings.Contains(strings.ToLower(rep.Feedback), "name") {
		t.Fatalf("Feedback = %q, want field name", rep.Feedback)
	}
	if !strings.Contains(rep.Feedback, "5") {
		t.Fatalf("Feedback = %q, want min length details", rep.Feedback)
	}
}

func TestNewJSONSchemaValidatorFromStruct_EmbeddedStruct_Pass(t *testing.T) {
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

	j, err := NewJSONSchemaValidatorFromStruct(&UserResponse{})
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
	if rep.Severity != guardy.SeverityMedium {
		t.Fatalf("severity = %q, want %q", rep.Severity, guardy.SeverityMedium)
	}
}

func TestJSONSchemaValidator_UnsupportedOptions(t *testing.T) {
	t.Run("action block", func(t *testing.T) {
		_, err := NewJSONSchemaValidator(testNameSchema, ext.WithAction(guardy.ActionBlock))
		if err == nil || !strings.Contains(err.Error(), "unsupported action") {
			t.Fatalf("expected unsupported action error, got %v", err)
		}
	})
	t.Run("token vault", func(t *testing.T) {
		_, err := NewJSONSchemaValidator(testNameSchema, ext.WithTokenVault(ext.NewInMemoryTokenVault()))
		if err == nil || !strings.Contains(err.Error(), "WithTokenVault is not supported") {
			t.Fatalf("expected token vault option error, got %v", err)
		}
	})
	t.Run("redaction replacement", func(t *testing.T) {
		_, err := NewJSONSchemaValidator(testNameSchema, ext.WithRedactionReplacement("[X]"))
		if err == nil || !strings.Contains(err.Error(), "WithRedactionReplacement is not supported") {
			t.Fatalf("expected redaction replacement option error, got %v", err)
		}
	})
	t.Run("lowercase", func(t *testing.T) {
		_, err := NewJSONSchemaValidator(testNameSchema, ext.WithLowercase(true))
		if err == nil || !strings.Contains(err.Error(), "WithLowercase is not supported") {
			t.Fatalf("expected lowercase option error, got %v", err)
		}
	})
}

func TestNewJSONSchemaValidatorFromStruct_InvalidInput(t *testing.T) {
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
			_, err := NewJSONSchemaValidatorFromStruct(tt.input)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
