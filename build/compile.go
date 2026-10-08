// Package build provides declarative GuardSpec compilation into guardy pipelines.
// It imports guardy core and extensions; the core package stays free of ext/jsonschema.
package build

import (
	"context"
	"fmt"
	"reflect"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
	jsonschemaext "github.com/skosovsky/guardy/ext/jsonschema"
)

// PolicyRuleKind selects built-in policy rule builders.
type PolicyRuleKind int

const (
	PolicyAttributePresent PolicyRuleKind = iota
	// PolicyAttributeDeepEqual uses [reflect.DeepEqual], including maps/slices, with no coercion.
	PolicyAttributeDeepEqual
)

// PolicyRuleSpec describes one policy validator in a GuardSpec.
type PolicyRuleSpec struct {
	Kind  PolicyRuleKind
	Key   string
	Value any // Borrowed immutable reflect.DeepEqual operand; no copying or type coercion.
}

// GuardSpec explicitly selects the rules of a string guard pipeline.
type GuardSpec struct {
	WordlistBlock []string
	PIIRedact     bool
	LengthMax     int
	PolicyRules   []PolicyRuleSpec
}

// CompileOption configures [CompileStringGuard].
type CompileOption func(*compileConfig)

type compileConfig struct {
	jsonSchema          []byte
	schemaSet           bool
	fallbackSet         bool
	userChannel         bool
	userChannelFallback string
	outputClassifier    bool
}

// WithJSONSchema adds JSON Schema validation via ext/jsonschema (optional).
// Explicitly empty bytes are invalid; {} is a valid permissive schema.
// It uses the explicit dialect (default 2020-12) without fetching external refs.
func WithJSONSchema(raw []byte) CompileOption {
	return func(c *compileConfig) {
		c.jsonSchema = raw
		c.schemaSet = true
	}
}

// WithUserChannel enables terminal filtering for compiled output guards.
func WithUserChannel() CompileOption {
	return func(c *compileConfig) {
		c.userChannel = true
	}
}

// WithUserChannelFallback sets the public message when user channel blocks technical output.
// Compilation requires WithUserChannel when this option is provided.
func WithUserChannelFallback(msg string) CompileOption {
	return func(c *compileConfig) {
		c.userChannelFallback = msg
		c.fallbackSet = true
	}
}

// WithOutputClassifier adds ext.NewTechnicalJSONClassifier to the sequential phase (for output guards).
func WithOutputClassifier() CompileOption {
	return func(c *compileConfig) {
		c.outputClassifier = true
	}
}

// CompileStringGuard builds a string pipeline from spec. Policy Value operands
// are borrowed, not snapshots: keep reachable maps/slices/pointers immutable after
// compile, including while retired pipelines still run. Reload with a fresh pipeline
// and fresh facts, publishing a single coherent version for each request.
// Sequential validator order: PII (optional) → wordlist → length → JSON schema (optional) → output classifier (optional).
func CompileStringGuard(spec GuardSpec, opts ...CompileOption) (*guardy.Pipeline[string], error) {
	cfg := compileConfig{
		jsonSchema:          nil,
		schemaSet:           false,
		fallbackSet:         false,
		userChannel:         false,
		userChannelFallback: "",
		outputClassifier:    false,
	}
	for i, opt := range opts {
		if opt == nil {
			return nil, configError(fmt.Sprintf("options[%d]", i), "required", nil)
		}
		opt(&cfg)
	}

	if err := validateConfig(spec, cfg); err != nil {
		return nil, err
	}

	var fast []guardy.Validator[string]

	if spec.PIIRedact {
		v, err := ext.NewPIIValidator(ext.WithCode("PII_DETECTED"))
		if err != nil {
			return nil, configError("PIIRedact", "invalid", err)
		}
		fast = append(fast, v)
	}
	if len(spec.WordlistBlock) > 0 {
		v, err := ext.NewWordlistValidator(spec.WordlistBlock, ext.Blocklist, ext.WithCode("WORDLIST_BLOCK"))
		if err != nil {
			return nil, configError("WordlistBlock", "invalid_token", err)
		}
		fast = append(fast, v)
	}
	if spec.LengthMax > 0 {
		v, err := ext.NewLengthValidator(0, spec.LengthMax, ext.WithCode("LENGTH_EXCEEDED"))
		if err != nil {
			return nil, configError("LengthMax", "invalid", err)
		}
		fast = append(fast, v)
	}
	if cfg.schemaSet {
		schemaV, err := jsonschemaext.NewJSONSchemaValidator(
			string(cfg.jsonSchema),
			ext.WithCode("JSON_SCHEMA_INVALID"),
		)
		if err != nil {
			return nil, configError("JSONSchema", "invalid_schema", err)
		}
		fast = append(fast, schemaV)
	}
	if cfg.outputClassifier {
		v, err := ext.NewTechnicalJSONClassifier(ext.WithCode("TECHNICAL_JSON"))
		if err != nil {
			return nil, configError("OutputClassifier", "invalid", err)
		}
		fast = append(fast, v)
	}

	return compilePolicies(spec, cfg, fast)
}

func compilePolicies(
	spec GuardSpec,
	cfg compileConfig,
	fast []guardy.Validator[string],
) (*guardy.Pipeline[string], error) {
	var policy []guardy.PolicyValidator[string]
	for _, rule := range spec.PolicyRules {
		switch rule.Kind {
		case PolicyAttributePresent:
			policy = append(policy, newPresentPolicy(rule.Key))
		case PolicyAttributeDeepEqual:
			policy = append(policy, newDeepEqualPolicy(rule.Key, rule.Value))
		default:
			return nil, configError("PolicyRules", "invalid_kind", nil)
		}
	}

	options := []guardy.PipelineOption[string]{guardy.WithSequential(fast...)}
	if len(policy) > 0 {
		options = append(options, guardy.WithPolicyValidators(policy...))
	}
	if cfg.userChannel {
		options = append(options, guardy.WithUserChannel[string]())
		if cfg.userChannelFallback != "" {
			options = append(options, guardy.WithUserChannelFallback[string](cfg.userChannelFallback))
		}
	}
	return guardy.NewPipeline(options...)
}

func configError(field, code string, cause error) error {
	return &guardy.ConfigurationError{Component: "build", Field: field, Code: code, Cause: cause}
}

func validateConfig(spec GuardSpec, cfg compileConfig) error {
	if spec.LengthMax < 0 {
		return configError("LengthMax", "negative", nil)
	}
	if cfg.fallbackSet && !cfg.userChannel {
		return configError("UserChannelFallback", "requires_user_channel", nil)
	}
	if cfg.schemaSet && len(cfg.jsonSchema) == 0 {
		return configError("JSONSchema", "empty", nil)
	}
	for i, rule := range spec.PolicyRules {
		if rule.Key == "" {
			return configError(fmt.Sprintf("PolicyRules[%d].Key", i), "required", nil)
		}
		if rule.Kind != PolicyAttributePresent && rule.Kind != PolicyAttributeDeepEqual {
			return configError(fmt.Sprintf("PolicyRules[%d].Kind", i), "invalid_kind", nil)
		}
	}
	return nil
}

func newPresentPolicy(key string) guardy.PolicyValidator[string] {
	scopeKey := guardy.NewScopeKey[any](key)
	return guardy.MustPolicyFuncWithScope[string](
		[]guardy.ScopeRequirement{scopeKey.Requirement()},
		func(_ context.Context, input string, scope guardy.ExecutionScope) (string, *guardy.Report, error) {
			if _, ok := scopeKey.Lookup(scope); !ok {
				return input, policyReport(
					"typed_attribute_present",
					guardy.CodeAttributeMissing,
					"attribute "+scopeKey.Name()+" not present",
				), nil
			}
			return input, nil, nil
		},
	)
}

func newDeepEqualPolicy(key string, want any) guardy.PolicyValidator[string] {
	scopeKey := guardy.NewScopeKey[any](key)
	return guardy.MustPolicyFuncWithScope[string](
		[]guardy.ScopeRequirement{scopeKey.Requirement()},
		func(_ context.Context, input string, scope guardy.ExecutionScope) (string, *guardy.Report, error) {
			got, ok := scopeKey.Lookup(scope)
			if !ok {
				return input, policyReport(
					"attribute_deep_equal",
					guardy.CodeAttributeMissing,
					"attribute "+scopeKey.Name()+" missing",
				), nil
			}
			if !reflect.DeepEqual(got, want) {
				return input, policyReport(
					"attribute_deep_equal",
					guardy.CodeAttributeMismatch,
					"attribute "+scopeKey.Name()+" mismatch",
				), nil
			}
			return input, nil, nil
		},
	)
}

func policyReport(validator string, code string, reason string) *guardy.Report {
	return guardy.FinishReport(&guardy.Report{
		Action:    guardy.ActionBlock,
		Validator: validator,
		Code:      code,
		Severity:  guardy.SeverityHigh,
		Reason:    reason,
	}, guardy.ControlSpec{Action: guardy.ActionBlock})
}
