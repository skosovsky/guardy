package guardy

import "context"

// PolicyValidator runs context-aware rules using [ExecutionScope].
type PolicyValidator[T any] interface {
	RequiredScopeKeys() []string
	Validate(ctx context.Context, input T, scope ExecutionScope) (T, *Report, error)
}

type policyFuncValidator[T any] struct {
	keys []string
	fn   func(ctx context.Context, input T, scope ExecutionScope) (T, *Report, error)
}

func (v policyFuncValidator[T]) RequiredScopeKeys() []string {
	return v.keys
}

func (v policyFuncValidator[T]) Validate(ctx context.Context, input T, scope ExecutionScope) (T, *Report, error) {
	return v.fn(ctx, input, scope)
}

// NewPolicyFunc builds a [PolicyValidator] from a function and explicit required scope keys.
// Pass nil or an empty slice when the validator does not require scope keys.
func NewPolicyFunc[T any](
	keys []string,
	fn func(ctx context.Context, input T, scope ExecutionScope) (T, *Report, error),
) PolicyValidator[T] {
	return policyFuncValidator[T]{keys: keys, fn: fn}
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

type attributeEqualsValidator[T any] struct {
	key  string
	want any
	cfg  PolicyConfig
}

func (v attributeEqualsValidator[T]) RequiredScopeKeys() []string {
	return []string{v.key}
}

func (v attributeEqualsValidator[T]) Validate(_ context.Context, input T, scope ExecutionScope) (T, *Report, error) {
	got, ok := scope.Lookup(v.key)
	if !ok {
		missingCfg := v.cfg
		missingCfg.Code = CodeAttributeMissing
		return input, policyViolationReport(missingCfg, "attribute "+v.key+" missing"), nil
	}
	if got != v.want {
		return input, policyViolationReport(v.cfg, "attribute "+v.key+" mismatch"), nil
	}
	return input, nil, nil
}

// NewAttributeEquals blocks when scope[key] != want (deep equality via == for comparable values).
func NewAttributeEquals[T any](key string, want any, opts ...PolicyOption) PolicyValidator[T] {
	cfg := applyPolicyConfig(PolicyConfig{
		Name:     "attribute_equals",
		Code:     CodeAttributeMismatch,
		Severity: SeverityHigh,
	}, opts...)
	return attributeEqualsValidator[T]{key: key, want: want, cfg: cfg}
}

type attributePresentValidator[T any] struct {
	key string
	cfg PolicyConfig
}

func (v attributePresentValidator[T]) RequiredScopeKeys() []string {
	return []string{v.key}
}

func (v attributePresentValidator[T]) Validate(_ context.Context, input T, scope ExecutionScope) (T, *Report, error) {
	if _, ok := scope.Lookup(v.key); !ok {
		return input, policyViolationReport(v.cfg, "attribute "+v.key+" not present"), nil
	}
	return input, nil, nil
}

// NewAttributePresent blocks when scope does not contain key.
func NewAttributePresent[T any](key string, opts ...PolicyOption) PolicyValidator[T] {
	cfg := applyPolicyConfig(PolicyConfig{
		Name:     "attribute_present",
		Code:     CodeAttributeMissing,
		Severity: SeverityHigh,
	}, opts...)
	return attributePresentValidator[T]{key: key, cfg: cfg}
}

// policyValidatorAdapter wraps PolicyValidator as Validator[T] using scope from Run.
type policyValidatorAdapter[T any] struct {
	p     PolicyValidator[T]
	scope ExecutionScope
}

func (a policyValidatorAdapter[T]) Validate(ctx context.Context, input T) (T, *Report, error) {
	return a.p.Validate(ctx, input, a.scope)
}
