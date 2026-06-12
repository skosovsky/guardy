// Package build provides declarative GuardSpec compilation into guardy pipelines.
// It imports guardy core and extensions; the core package stays free of ext/jsonschema.
package build

import (
	"fmt"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
	jsonschemaext "github.com/skosovsky/guardy/ext/jsonschema"
)

// PolicyRuleKind selects built-in policy rule builders.
type PolicyRuleKind int

const (
	PolicyAttributePresent PolicyRuleKind = iota
	PolicyAttributeEquals
)

// PolicyRuleSpec describes one policy validator in a GuardSpec.
type PolicyRuleSpec struct {
	Kind  PolicyRuleKind
	Key   string
	Value any // used for PolicyAttributeEquals
}

// GuardSpec describes intent for a string guard pipeline (MVP).
type GuardSpec struct {
	WordlistBlock []string
	PIIRedact     bool
	LengthMax     int
	PolicyRules   []PolicyRuleSpec
	Sensitivity   SensitivityLevel
}

// SensitivityLevel adjusts default guard strictness when compiling a pipeline.
type SensitivityLevel int

const (
	// SensitivityNormal uses GuardSpec fields as-is (default).
	SensitivityNormal SensitivityLevel = iota
	// SensitivityStrict enables PII redaction and tightens LengthMax when set.
	SensitivityStrict
	// SensitivityPermissive keeps only explicit wordlist and policy rules.
	SensitivityPermissive
)

const (
	strictLengthNumerator   = 3
	strictLengthDenominator = 4
)

func applySensitivity(spec GuardSpec) GuardSpec {
	s := spec
	switch spec.Sensitivity {
	case SensitivityNormal:
		// use spec fields as-is
	case SensitivityStrict:
		s.PIIRedact = true
		if s.LengthMax > 0 {
			s.LengthMax = max(1, s.LengthMax*strictLengthNumerator/strictLengthDenominator)
		}
	case SensitivityPermissive:
		s.PIIRedact = false
		s.LengthMax = 0
	}
	return s
}

// CompileOption configures [CompileStringGuard].
type CompileOption func(*compileConfig)

type compileConfig struct {
	jsonSchema          []byte
	userChannel         bool
	userChannelFallback string
	outputClassifier    bool
}

// WithJSONSchema adds JSON Schema validation via ext/jsonschema (optional).
func WithJSONSchema(raw []byte) CompileOption {
	return func(c *compileConfig) {
		c.jsonSchema = raw
	}
}

// WithUserChannel enables terminal filtering for compiled output guards.
func WithUserChannel() CompileOption {
	return func(c *compileConfig) {
		c.userChannel = true
	}
}

// WithUserChannelFallback sets the public message when user channel blocks technical output.
func WithUserChannelFallback(msg string) CompileOption {
	return func(c *compileConfig) {
		c.userChannelFallback = msg
	}
}

// WithOutputClassifier adds ext.NewTechnicalJSONClassifier to the fast-path (for output guards).
func WithOutputClassifier() CompileOption {
	return func(c *compileConfig) {
		c.outputClassifier = true
	}
}

// CompileStringGuard builds a string pipeline from spec.
// Fast-path order: PII (optional) → wordlist → length → JSON schema (optional) → output classifier (optional).
func CompileStringGuard(spec GuardSpec, opts ...CompileOption) (*guardy.Pipeline[string], error) {
	spec = applySensitivity(spec)
	cfg := compileConfig{
		jsonSchema:          nil,
		userChannel:         false,
		userChannelFallback: "",
		outputClassifier:    false,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	var fast []guardy.Validator[string]

	if spec.PIIRedact {
		fast = append(fast, ext.NewPIIValidator(ext.WithCode("PII_DETECTED")))
	}
	if len(spec.WordlistBlock) > 0 {
		fast = append(fast, ext.NewWordlistValidator(spec.WordlistBlock, ext.Blocklist, ext.WithCode("WORDLIST_BLOCK")))
	}
	if spec.LengthMax > 0 {
		fast = append(fast, ext.NewLengthValidator(0, spec.LengthMax, ext.WithCode("LENGTH_EXCEEDED")))
	}
	if len(cfg.jsonSchema) > 0 {
		schemaV, err := jsonschemaext.NewJSONSchemaValidator(
			string(cfg.jsonSchema),
			ext.WithCode("JSON_SCHEMA_INVALID"),
		)
		if err != nil {
			return nil, fmt.Errorf("json schema validator: %w", err)
		}
		fast = append(fast, schemaV)
	}
	if cfg.outputClassifier {
		fast = append(fast, ext.NewTechnicalJSONClassifier(ext.WithCode("TECHNICAL_JSON")))
	}

	var policy []guardy.PolicyValidator[string]
	for _, rule := range spec.PolicyRules {
		switch rule.Kind {
		case PolicyAttributePresent:
			policy = append(policy, guardy.NewAttributePresent[string](
				rule.Key,
				guardy.WithPolicyCode(guardy.CodeAttributeMissing),
			))
		case PolicyAttributeEquals:
			policy = append(policy, guardy.NewAttributeEquals[string](
				rule.Key,
				rule.Value,
				guardy.WithPolicyCode(guardy.CodeAttributeMismatch),
			))
		default:
			return nil, fmt.Errorf("unknown policy rule kind %v", rule.Kind)
		}
	}

	options := []guardy.PipelineOption[string]{guardy.WithFastPath(fast...)}
	if len(policy) > 0 {
		options = append(options, guardy.WithPolicyValidators(policy...))
	}
	if cfg.userChannel {
		options = append(options, guardy.WithUserChannel[string]())
		if cfg.userChannelFallback != "" {
			options = append(options, guardy.WithUserChannelFallback[string](cfg.userChannelFallback))
		}
	}
	return guardy.NewPipeline(options...), nil
}
