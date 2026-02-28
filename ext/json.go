package ext

import (
	"context"
	"encoding/json"

	"github.com/skosovsky/guardy"

	"github.com/google/jsonschema-go/jsonschema"
)

// Ensure JSONSchema implements guardy.Validator at compile time.
var _ guardy.Validator = (*JSONSchema)(nil)

// JSONSchema is a validator that checks input.Text conforms to a JSON Schema.
// On invalid JSON or schema mismatch it always returns Retry with Reason, Evidence, and Guidance for LLM self-correction.
type JSONSchema struct {
	resolved *jsonschema.Resolved
	code     string
	name     string
}

// JSONSchemaOption configures a JSON Schema validator.
type JSONSchemaOption func(*JSONSchema)

// WithJSONSchemaName sets the validator name (default "json").
func WithJSONSchemaName(name string) JSONSchemaOption {
	return func(j *JSONSchema) {
		j.name = name
	}
}

// WithJSONName is a short alias for WithJSONSchemaName (naming consistent with WithRegexName, WithLengthName, WithWordlistName).
func WithJSONName(name string) JSONSchemaOption {
	return WithJSONSchemaName(name)
}

// NewJSONSchema creates a validator that checks text against the given JSON Schema (JSON string).
// Schema must be valid draft-07 or 2020-12. On validation failure the validator always returns
// Action Retry with Guidance set to the schema error message (and Reason/Evidence when available).
func NewJSONSchema(schemaJSON, code string, opts ...JSONSchemaOption) (*JSONSchema, error) {
	var s jsonschema.Schema
	if err := json.Unmarshal([]byte(schemaJSON), &s); err != nil {
		return nil, err
	}
	resolved, err := s.Resolve(&jsonschema.ResolveOptions{})
	if err != nil {
		return nil, err
	}
	j := &JSONSchema{
		resolved: resolved,
		code:     code,
		name:     "json",
	}
	for _, opt := range opts {
		opt(j)
	}
	return j, nil
}

// MustJSONSchema is like NewJSONSchema but panics on error (for init-time or tests).
func MustJSONSchema(schemaJSON, code string, opts ...JSONSchemaOption) *JSONSchema {
	j, err := NewJSONSchema(schemaJSON, code, opts...)
	if err != nil {
		panic("ext: MustJSONSchema: " + err.Error())
	}
	return j
}

// Validate checks that input.Text is valid JSON and conforms to the schema.
// On failure returns Retry with Guidance (and Reason, Evidence) for self-correction.
func (j *JSONSchema) Validate(ctx context.Context, input guardy.Input) (guardy.Result, error) {
	text := input.Text
	var instance any
	if err := json.Unmarshal([]byte(text), &instance); err != nil {
		return guardy.Result{
			Passed:   false,
			Action:   guardy.Retry,
			Code:     j.code,
			Reason:   "invalid JSON",
			Guidance: "Output must be valid JSON: " + err.Error(),
		}, nil
	}
	if err := j.resolved.Validate(instance); err != nil {
		return guardy.Result{
			Passed:   false,
			Action:   guardy.Retry,
			Code:     j.code,
			Reason:   "schema validation failed",
			Evidence: text,
			Guidance: err.Error(),
		}, nil
	}
	return guardy.Result{Passed: true, Action: guardy.Pass}, nil
}

// Name returns the validator name.
func (j *JSONSchema) Name() string {
	return j.name
}
