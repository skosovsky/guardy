package guardy

import "context"

// PolicyValidator runs context-aware rules using [Attributes] from ctx.
type PolicyValidator[T any] interface {
	Validate(ctx context.Context, input T, attrs Attributes) (T, *Report, error)
}

// PolicyFunc is a function adapter for [PolicyValidator].
type PolicyFunc[T any] func(ctx context.Context, input T, attrs Attributes) (T, *Report, error)

// Validate implements [PolicyValidator].
func (f PolicyFunc[T]) Validate(ctx context.Context, input T, attrs Attributes) (T, *Report, error) {
	return f(ctx, input, attrs)
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
	ApplyControlDefaults(rep, ControlSpec{
		Action:          ActionBlock,
		Retryable:       cfg.Retryable,
		Fatal:           cfg.Fatal,
		SafeUserMessage: cfg.SafeUserMessage,
	})
	return rep
}

// NewAttributeEquals blocks when attrs[key] != want (deep equality via == for comparable values).
// When attrs are missing from ctx, the validator is a no-op (pass-through).
func NewAttributeEquals[T any](key string, want any, opts ...PolicyOption) PolicyFunc[T] {
	cfg := applyPolicyConfig(PolicyConfig{
		Name:     "attribute_equals",
		Code:     CodeAttributeMismatch,
		Severity: SeverityHigh,
	}, opts...)
	return func(_ context.Context, input T, attrs Attributes) (T, *Report, error) {
		if attrs == nil {
			return input, nil, nil
		}
		got, ok := attrs[key]
		if !ok {
			missingCfg := cfg
			missingCfg.Code = CodeAttributeMissing
			return input, policyViolationReport(missingCfg, "attribute "+key+" missing"), nil
		}
		if got != want {
			return input, policyViolationReport(cfg, "attribute "+key+" mismatch"), nil
		}
		return input, nil, nil
	}
}

// NewAttributePresent blocks when attrs does not contain key.
// When attrs are missing from ctx, the validator is a no-op.
func NewAttributePresent[T any](key string, opts ...PolicyOption) PolicyFunc[T] {
	cfg := applyPolicyConfig(PolicyConfig{
		Name:     "attribute_present",
		Code:     CodeAttributeMissing,
		Severity: SeverityHigh,
	}, opts...)
	return func(_ context.Context, input T, attrs Attributes) (T, *Report, error) {
		if attrs == nil {
			return input, nil, nil
		}
		if _, ok := attrs[key]; !ok {
			return input, policyViolationReport(cfg, "attribute "+key+" not present"), nil
		}
		return input, nil, nil
	}
}

// policyValidatorAdapter wraps PolicyValidator as Validator[T] using ctx attributes.
type policyValidatorAdapter[T any] struct {
	p PolicyValidator[T]
}

func (a policyValidatorAdapter[T]) Validate(ctx context.Context, input T) (T, *Report, error) {
	attrs, ok := AttributesFromContext(ctx)
	if !ok {
		return input, nil, nil
	}
	return a.p.Validate(ctx, input, attrs)
}
