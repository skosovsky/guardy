// Package jsonschema provides a JSON Schema validator that implements guardy.Validator[string].
// Valid documents return ActionPass; invalid ones return ActionRetry with Feedback for LLM.
package jsonschema

import (
	"context"
	"fmt"
	"strings"

	"github.com/skosovsky/guardy"
	"github.com/xeipuuv/gojsonschema"
)

// Ensure JSONSchemaValidator implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*JSONSchemaValidator)(nil)

// JSONSchemaValidator validates JSON strings against a JSON Schema.
// On schema violation it returns ActionRetry with detailed Feedback for LLM.
type JSONSchemaValidator struct {
	schema *gojsonschema.Schema
	name   string
}

// Option configures JSONSchemaValidator.
type Option func(*JSONSchemaValidator)

// WithJSONSchemaName sets the validator name (default "jsonschema").
func WithJSONSchemaName(name string) Option {
	return func(j *JSONSchemaValidator) {
		j.name = name
	}
}

// NewJSONSchemaValidator creates a validator from a JSON Schema string.
// The schema is compiled once at creation time.
func NewJSONSchemaValidator(schema string, opts ...Option) (*JSONSchemaValidator, error) {
	loader := gojsonschema.NewStringLoader(schema)
	s, err := gojsonschema.NewSchema(loader)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compile schema: %w", err)
	}
	j := &JSONSchemaValidator{schema: s, name: "jsonschema"}
	for _, opt := range opts {
		opt(j)
	}
	return j, nil
}

// Validate checks that input is valid JSON conforming to the schema.
// Valid -> ActionPass. Invalid -> ActionRetry with Feedback (detailed message for LLM).
func (j *JSONSchemaValidator) Validate(ctx context.Context, input string) (string, *guardy.Report, error) {
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	docLoader := gojsonschema.NewStringLoader(input)
	result, err := j.schema.Validate(docLoader)
	if err != nil {
		return input, nil, fmt.Errorf("jsonschema: validate: %w", err)
	}
	if result.Valid() {
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: j.name}, nil
	}
	var sb strings.Builder
	for _, e := range result.Errors() {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(e.String())
	}
	feedback := sb.String()
	return input, &guardy.Report{
		Action:    guardy.ActionRetry,
		Validator: j.name,
		Reason:    "schema validation failed",
		Feedback:  feedback,
	}, nil
}
