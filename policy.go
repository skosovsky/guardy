package guardy

import (
	"context"
	"errors"
	"reflect"
)

// ErrAttributeIncomparable identifies equality operands that cannot safely use Go ==.
var ErrAttributeIncomparable = errors.New("guardy: attribute equality requires comparable values")

// AttributeComparisonError identifies the scope key of an invalid equality check.
// It contains no operand values and is a system fault when returned through Pipeline.
type AttributeComparisonError struct{ Key string }

func (e *AttributeComparisonError) Error() string { return ErrAttributeIncomparable.Error() }
func (e *AttributeComparisonError) Unwrap() error { return ErrAttributeIncomparable }

// PolicyValidator runs context-aware rules using [ExecutionScope].
type PolicyValidator[T any] interface {
	RequiredScope() []ScopeRequirement
	Validate(ctx context.Context, input T, scope ExecutionScope) (T, *Report, error)
}

type policyFuncValidator[T any] struct {
	requirements []ScopeRequirement
	fn           func(ctx context.Context, input T, scope ExecutionScope) (T, *Report, error)
}

func (v policyFuncValidator[T]) RequiredScope() []ScopeRequirement {
	return append([]ScopeRequirement(nil), v.requirements...)
}

func (v policyFuncValidator[T]) Validate(ctx context.Context, input T, scope ExecutionScope) (T, *Report, error) {
	return v.fn(ctx, input, scope)
}

// NewPolicyFunc builds a [PolicyValidator] from a function and explicit required scope keys.
// Pass nil or an empty slice when the validator does not require scope keys.
//
// Deprecated: use [NewPolicyFuncWithScope] with typed [ScopeRequirement] values.
func NewPolicyFunc[T any](
	keys []string,
	fn func(ctx context.Context, input T, scope ExecutionScope) (T, *Report, error),
) PolicyValidator[T] {
	return policyFuncValidator[T]{
		requirements: scopeRequirementsFromKeys(keys),
		fn:           fn,
	}
}

// NewPolicyFuncWithScope builds a [PolicyValidator] from typed scope requirements.
func NewPolicyFuncWithScope[T any](
	requirements []ScopeRequirement,
	fn func(ctx context.Context, input T, scope ExecutionScope) (T, *Report, error),
) PolicyValidator[T] {
	return policyFuncValidator[T]{
		requirements: append([]ScopeRequirement(nil), requirements...),
		fn:           fn,
	}
}

// PolicyConfig configures built-in policy validators.
type PolicyConfig struct {
	Name            string
	Code            string
	Severity        Severity
	Reason          string
	Retryable       *bool
	Fatal           bool
	SafeUserMessage string
}

// PolicyOption configures [PolicyConfig].
type PolicyOption func(*PolicyConfig)

// WithPolicyName sets the validator name on policy reports.
func WithPolicyName(name string) PolicyOption {
	return func(c *PolicyConfig) { c.Name = name }
}

// WithPolicyCode sets the machine-readable code.
func WithPolicyCode(code string) PolicyOption {
	return func(c *PolicyConfig) { c.Code = code }
}

// WithPolicySeverity sets severity.
func WithPolicySeverity(sev Severity) PolicyOption {
	return func(c *PolicyConfig) { c.Severity = sev }
}

// WithPolicyReason sets the report reason.
func WithPolicyReason(reason string) PolicyOption {
	return func(c *PolicyConfig) { c.Reason = reason }
}

// WithPolicyRetryable overrides default retryability.
func WithPolicyRetryable(retryable bool) PolicyOption {
	return func(c *PolicyConfig) {
		v := retryable
		c.Retryable = &v
	}
}

// WithPolicyFatal marks a hard escalation.
func WithPolicyFatal(fatal bool) PolicyOption {
	return func(c *PolicyConfig) { c.Fatal = fatal }
}

// WithPolicySafeUserMessage sets an end-user safe message.
func WithPolicySafeUserMessage(msg string) PolicyOption {
	return func(c *PolicyConfig) { c.SafeUserMessage = msg }
}

func applyPolicyConfig(defaults PolicyConfig, opts ...PolicyOption) PolicyConfig {
	cfg := defaults
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func policyViolationReport(cfg PolicyConfig, reason string) *Report {
	if cfg.Reason != "" {
		reason = cfg.Reason
	}
	rep := &Report{
		Action:    ActionBlock,
		Validator: cfg.Name,
		Code:      cfg.Code,
		Severity:  cfg.Severity,
		Reason:    reason,
	}
	return FinishReport(rep, ControlSpec{
		Action:          ActionBlock,
		Retryable:       cfg.Retryable,
		Fatal:           cfg.Fatal,
		SafeUserMessage: cfg.SafeUserMessage,
	})
}

type typedAttributeEqualsValidator[T any, V comparable] struct {
	key  ScopeKey[V]
	want V
	cfg  PolicyConfig
}

func (v typedAttributeEqualsValidator[T, V]) RequiredScope() []ScopeRequirement {
	return []ScopeRequirement{v.key.Requirement()}
}

func (v typedAttributeEqualsValidator[T, V]) Validate(
	_ context.Context,
	input T,
	scope ExecutionScope,
) (T, *Report, error) {
	got, ok := v.key.Lookup(scope)
	if !ok {
		if scope != nil {
			if _, exists := scope.Lookup(v.key.Name()); exists {
				typeCfg := v.cfg
				typeCfg.Code = CodeAttributeTypeMismatch
				return input, policyViolationReport(typeCfg, "attribute "+v.key.Name()+" type mismatch"), nil
			}
		}
		missingCfg := v.cfg
		missingCfg.Code = CodeAttributeMissing
		return input, policyViolationReport(missingCfg, "attribute "+v.key.Name()+" missing"), nil
	}
	if !reflect.ValueOf(&got).Elem().Comparable() || !reflect.ValueOf(&v.want).Elem().Comparable() {
		return input, nil, &AttributeComparisonError{Key: v.key.Name()}
	}
	if got != v.want {
		return input, policyViolationReport(v.cfg, "attribute "+v.key.Name()+" mismatch"), nil
	}
	return input, nil, nil
}

// NewTypedAttributeEquals blocks when typed scope[key] != want using Go == semantics.
// Dynamically incomparable operands (including interface fields containing slices
// or maps) return [ErrAttributeIncomparable], rather than panic or a policy mismatch.
func NewTypedAttributeEquals[T any, V comparable](key ScopeKey[V], want V, opts ...PolicyOption) PolicyValidator[T] {
	cfg := applyPolicyConfig(PolicyConfig{
		Name:     "typed_attribute_equals",
		Code:     CodeAttributeMismatch,
		Severity: SeverityHigh,
	}, opts...)
	return typedAttributeEqualsValidator[T, V]{key: key, want: want, cfg: cfg}
}

type attributePresentValidator[T any] struct {
	key string
	cfg PolicyConfig
}

func (v attributePresentValidator[T]) RequiredScope() []ScopeRequirement {
	return scopeRequirementsFromKeys([]string{v.key})
}

func (v attributePresentValidator[T]) Validate(_ context.Context, input T, scope ExecutionScope) (T, *Report, error) {
	if _, ok := scope.Lookup(v.key); !ok {
		return input, policyViolationReport(v.cfg, "attribute "+v.key+" not present"), nil
	}
	return input, nil, nil
}

// NewAttributePresent blocks when scope does not contain key.
//
// Deprecated: use [NewTypedAttributePresent] with [ScopeKey].
func NewAttributePresent[T any](key string, opts ...PolicyOption) PolicyValidator[T] {
	cfg := applyPolicyConfig(PolicyConfig{
		Name:     "attribute_present",
		Code:     CodeAttributeMissing,
		Severity: SeverityHigh,
	}, opts...)
	return attributePresentValidator[T]{key: key, cfg: cfg}
}

type typedAttributePresentValidator[T any, V any] struct {
	key ScopeKey[V]
	cfg PolicyConfig
}

func (v typedAttributePresentValidator[T, V]) RequiredScope() []ScopeRequirement {
	return []ScopeRequirement{v.key.Requirement()}
}

func (v typedAttributePresentValidator[T, V]) Validate(
	_ context.Context,
	input T,
	scope ExecutionScope,
) (T, *Report, error) {
	if _, ok := v.key.Lookup(scope); !ok {
		if scope != nil {
			if _, exists := scope.Lookup(v.key.Name()); exists {
				typeCfg := v.cfg
				typeCfg.Code = CodeAttributeTypeMismatch
				return input, policyViolationReport(typeCfg, "attribute "+v.key.Name()+" type mismatch"), nil
			}
		}
		return input, policyViolationReport(v.cfg, "attribute "+v.key.Name()+" not present"), nil
	}
	return input, nil, nil
}

// NewTypedAttributePresent blocks when a typed scope key is absent or has the wrong type.
func NewTypedAttributePresent[T any, V any](key ScopeKey[V], opts ...PolicyOption) PolicyValidator[T] {
	cfg := applyPolicyConfig(PolicyConfig{
		Name:     "typed_attribute_present",
		Code:     CodeAttributeMissing,
		Severity: SeverityHigh,
	}, opts...)
	return typedAttributePresentValidator[T, V]{key: key, cfg: cfg}
}

// policyValidatorAdapter wraps PolicyValidator as Validator[T] using scope from Run.
type policyValidatorAdapter[T any] struct {
	p     PolicyValidator[T]
	scope ExecutionScope
}

func (a policyValidatorAdapter[T]) Validate(ctx context.Context, input T) (T, *Report, error) {
	return a.p.Validate(ctx, input, a.scope)
}
