// Package jsonschema provides a JSON Schema validator that implements guardy.Validator[string].
// Valid documents return ActionPass; invalid ones return ActionRetry with Feedback for LLM.
package jsonschema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	invopopjsonschema "github.com/invopop/jsonschema"
	schemaengine "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
	"github.com/skosovsky/guardy/internal/jsondoc"
)

// Ensure JSONSchemaValidator implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*JSONSchemaValidator)(nil)

// JSONSchemaValidator validates JSON strings against a JSON Schema.
// On schema violation it returns ActionRetry with detailed Feedback for LLM.
//
//nolint:revive // The explicit type name distinguishes the validator from generated schemas.
type JSONSchemaValidator struct {
	schema *schemaengine.Schema
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
	return NewJSONSchemaValidatorWithResources(schema, nil, opts...)
}

// NewJSONSchemaValidatorWithResources compiles with caller-owned JSON resources
// keyed by absolute URI. Compilation never fetches network or filesystem resources.
// Drafts 4, 6, 7, 2019-09 and 2020-12 are supported; the default is 2020-12.
func NewJSONSchemaValidatorWithResources(
	schema string,
	resources map[string]string,
	opts ...Option,
) (*JSONSchemaValidator, error) {
	cfg, configErr := buildConfig(opts...)
	if configErr != nil {
		return nil, configErr
	}

	const rootURI = "urn:guardy:schema"
	documents := make(map[string]any, len(resources)+1)
	for uri, raw := range resources {
		parsed, parseErr := url.Parse(uri)
		if parseErr != nil || !parsed.IsAbs() || strings.Contains(uri, "#") {
			return nil, errors.New("jsonschema: resource URI must be absolute and fragment-free")
		}
		uri = parsed.String()
		if _, exists := documents[uri]; exists {
			return nil, errors.New("jsonschema: duplicate normalized resource URI")
		}
		if uri == rootURI {
			return nil, errors.New("jsonschema: reserved root resource URI")
		}
		document, err := jsondoc.Decode(raw)
		if err != nil {
			return nil, fmt.Errorf("jsonschema: decode resource: %w", err)
		}
		documents[uri] = document
	}
	document, err := jsondoc.Decode(schema)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: decode schema: %w", err)
	}
	documents[rootURI] = document
	probe, err := resourceCompiler(documents, true)
	if err != nil {
		return nil, err
	}
	compiledProbe, err := probe.Compile(rootURI)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compile schema: %w", err)
	}
	seen := make(map[*schemaengine.Schema]bool)
	if numberErr := checkCompiledNumbers(compiledProbe, documents, seen); numberErr != nil {
		return nil, numberErr
	}
	for uri, doc := range documents {
		if anchorErr := checkDynamicAnchors(probe, numericProbe(doc), uri, documents, seen); anchorErr != nil {
			return nil, anchorErr
		}
	}
	if metaErr := checkMetaNumbers(probe, documents, seen); metaErr != nil {
		return nil, metaErr
	}
	compiler, err := resourceCompiler(documents, false)
	if err != nil {
		return nil, err
	}
	compiled, err := compileSchema(compiler, rootURI)
	if err != nil {
		return nil, fmt.Errorf("jsonschema: compile schema: %w", err)
	}
	return &JSONSchemaValidator{schema: compiled, cfg: cfg}, nil
}

func resourceCompiler(documents map[string]any, probe bool) (*schemaengine.Compiler, error) {
	compiler := schemaengine.NewCompiler()
	compiler.DefaultDraft(schemaengine.Draft2020)
	for uri, document := range documents {
		if probe {
			document = numericProbe(document)
		}
		if err := compiler.AddResource(uri, document); err != nil {
			return nil, fmt.Errorf("jsonschema: add resource: %w", err)
		}
	}
	return compiler, nil
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
	for i, opt := range opts {
		if opt == nil {
			return cfg, &guardy.ConfigurationError{
				Component: "jsonschema",
				Field:     fmt.Sprintf("options[%d]", i),
				Code:      "required",
				Cause:     nil,
			}
		}
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

	doc, err := jsondoc.Decode(input)
	if err != nil {
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

	validationErr := checkNumbers(doc)
	if validationErr == nil {
		validationErr = j.schema.Validate(doc)
	}
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	if validationErr == nil {
		rep := &guardy.Report{
			Action:    guardy.ActionPass,
			Validator: j.cfg.Name,
			Code:      j.cfg.Code,
			Severity:  j.cfg.Severity,
		}
		guardy.FinishReport(rep, guardy.ControlSpec{Action: guardy.ActionPass})
		return input, rep, nil
	}
	feedback := validationErr.Error()
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

// compileSchema contains numeric panics during engine metaschema validation.
//
//nolint:nonamedreturns // Named results let recovery return a constructor error.
func compileSchema(compiler *schemaengine.Compiler, uri string) (schema *schemaengine.Schema, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			schema = nil
			err = fmt.Errorf("jsonschema: engine compilation fault: %v", recovered)
		}
	}()
	return compiler.Compile(uri)
}

func checkMetaNumbers(
	compiler *schemaengine.Compiler,
	documents map[string]any,
	seen map[*schemaengine.Schema]bool,
) error {
	for _, document := range documents {
		object, ok := document.(map[string]any)
		if !ok {
			continue
		}
		uri, ok := object["$schema"].(string)
		if !ok || documents[uri] == nil {
			continue
		}
		meta, err := compiler.Compile(uri)
		if err != nil {
			return fmt.Errorf("jsonschema: compile metaschema: %w", err)
		}
		if err := checkCompiledNumbers(meta, documents, seen); err != nil {
			return err
		}
	}
	return nil
}
