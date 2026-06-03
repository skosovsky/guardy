// Package jsonschema provides a JSON Schema validator that implements guardy.Validator[string].
// Valid documents return ActionPass; invalid ones return ActionRetry with Feedback for LLM.
package jsonschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	invopopjsonschema "github.com/invopop/jsonschema"
	"github.com/xeipuuv/gojsonschema"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

// Ensure JSONSchemaValidator implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*JSONSchemaValidator)(nil)

// JSONSchemaValidator validates JSON strings against a JSON Schema.
// On schema violation it returns ActionRetry with detailed Feedback for LLM.
//
//nolint:revive // keep the public type name stable for backward compatibility.
type JSONSchemaValidator struct {
	schema *gojsonschema.Schema
	cfg    ext.RuleConfig
}

const defaultJSONSchemaValidatorName = "jsonschema"

// Option configures JSONSchemaValidator using shared ext validator options.
type Option = ext.Option

// WithJSONSchemaName sets the validator name (default "jsonschema").
func WithJSONSchemaName(name string) Option {
	return ext.WithName(name)
}

// NewJSONSchemaValidator creates a validator from a JSON Schema string.
// The schema is compiled once at creation time.
func NewJSONSchemaValidator(schema string, opts ...Option) (*JSONSchemaValidator, error) {
	cfg, err := buildConfig(opts...)
	if err != nil {
		return nil, err
	}

	loader := gojsonschema.NewStringLoader(schema)
	s, err := gojsonschema.NewSchema(loader)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compile schema: %w", err)
	}
	return &JSONSchemaValidator{schema: s, cfg: cfg}, nil
}

// NewJSONSchemaValidatorFromStruct generates a JSON schema from the provided Go struct
// and returns a Validator that enforces this schema.
func NewJSONSchemaValidatorFromStruct(v any, opts ...Option) (*JSONSchemaValidator, error) {
	if v == nil {
		return nil, errors.New("jsonschema: expected struct or *struct, got nil")
	}

	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Pointer {
		value := reflect.ValueOf(v)
		if value.IsNil() {
			return nil, errors.New("jsonschema: expected struct or *struct, got nil pointer")
		}
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("jsonschema: expected struct or *struct, got %s", reflect.TypeOf(v))
	}

	schema := invopopjsonschema.Reflect(v)
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: marshal generated schema: %w", err)
	}

	return NewJSONSchemaValidator(string(schemaBytes), opts...)
}

func buildConfig(opts ...Option) (ext.RuleConfig, error) {
	cfg := ext.RuleConfig{
		Action:   guardy.ActionRetry,
		Severity: guardy.SeverityMedium,
		Name:     defaultJSONSchemaValidatorName,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.Action != guardy.ActionRetry {
		return ext.RuleConfig{}, fmt.Errorf(
			"jsonschema: unsupported action %q: only retry is supported",
			cfg.Action.String(),
		)
	}
	if cfg.Lowercase {
		return ext.RuleConfig{}, errors.New("jsonschema: WithLowercase is not supported")
	}
	if cfg.RedactionReplacement != "" {
		return ext.RuleConfig{}, errors.New("jsonschema: WithRedactionReplacement is not supported")
	}
	if cfg.TokenVault != nil {
		return ext.RuleConfig{}, errors.New("jsonschema: WithTokenVault is not supported")
	}
	return cfg, nil
}

// Validate checks that input is valid JSON conforming to the schema.
// Valid -> ActionPass. Invalid -> ActionRetry with Feedback (detailed message for LLM).
func (j *JSONSchemaValidator) Validate(ctx context.Context, input string) (string, *guardy.Report, error) {
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}

	var doc any
	if err := json.Unmarshal([]byte(input), &doc); err != nil {
		rep := &guardy.Report{
			Action:    guardy.ActionRetry,
			Validator: j.cfg.Name,
			Code:      guardy.CodeJSONInvalid,
			Severity:  j.cfg.Severity,
			Reason:    "invalid JSON",
			Feedback:  err.Error(),
		}
		ext.FinalizeRuleReport(rep, j.cfg, guardy.ActionRetry)
		return input, rep, nil //nolint:nilerr // parse failure is surfaced as ActionRetry + Feedback, not as error
	}

	docLoader := gojsonschema.NewGoLoader(doc)
	result, err := j.schema.Validate(docLoader)
	if err != nil {
		return input, nil, fmt.Errorf("jsonschema: validate: %w", err)
	}
	if result.Valid() {
		rep := &guardy.Report{
			Action:    guardy.ActionPass,
			Validator: j.cfg.Name,
			Code:      j.cfg.Code,
			Severity:  j.cfg.Severity,
		}
		ext.FinalizeRuleReport(rep, j.cfg, guardy.ActionPass)
		return input, rep, nil
	}
	var sb strings.Builder
	for _, e := range result.Errors() {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(e.String())
	}
	feedback := sb.String()
	code := j.cfg.Code
	if code == "" {
		code = guardy.CodeJSONSchemaInvalid
	}
	reason := "schema validation failed"
	if j.cfg.Reason != "" {
		reason = j.cfg.Reason
	}
	rep := &guardy.Report{
		Action:    guardy.ActionRetry,
		Validator: j.cfg.Name,
		Code:      code,
		Severity:  j.cfg.Severity,
		Reason:    reason,
		Feedback:  feedback,
	}
	ext.FinalizeRuleReport(rep, j.cfg, guardy.ActionRetry)
	return input, rep, nil
}
