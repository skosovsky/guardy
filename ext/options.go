package ext

import "github.com/skosovsky/guardy"

const defaultRedactionReplacement = "[REDACTED]"

// RuleConfig contains shared policy metadata and behavior for built-in validators.
type RuleConfig struct {
	Action               guardy.Action
	Code                 string
	Severity             guardy.Severity
	Reason               string
	Name                 string
	RedactionReplacement string
	Lowercase            bool
	TokenVault           TokenVault
	Retryable            *bool
	Fatal                bool
	SafeUserMessage      string
}

// Option configures RuleConfig for built-in validators.
type Option func(*RuleConfig)

// ValidatorOption is a semantic alias used by v2 docs/contracts.
type ValidatorOption = Option

// WithAction sets validator action behavior.
func WithAction(action guardy.Action) Option {
	return func(c *RuleConfig) {
		c.Action = action
	}
}

// WithCode sets machine-readable rule code.
func WithCode(code string) Option {
	return func(c *RuleConfig) {
		c.Code = code
	}
}

// WithSeverity sets rule severity for telemetry and alerting.
func WithSeverity(severity guardy.Severity) Option {
	return func(c *RuleConfig) {
		c.Severity = severity
	}
}

// WithReason sets custom report reason for violations.
func WithReason(reason string) Option {
	return func(c *RuleConfig) {
		c.Reason = reason
	}
}

// WithName sets validator name.
func WithName(name string) Option {
	return func(c *RuleConfig) {
		c.Name = name
	}
}

// WithRedactionReplacement sets replacement text for redaction mode.
func WithRedactionReplacement(replacement string) Option {
	return func(c *RuleConfig) {
		c.RedactionReplacement = replacement
	}
}

// WithLowercase enables lowercase normalization in validators that support it.
func WithLowercase(lower bool) Option {
	return func(c *RuleConfig) {
		c.Lowercase = lower
	}
}

// WithTokenVault enables reversible token redaction using the provided vault.
// Built-in validators pass explicit namespaces (for example PII, WORDLIST).
func WithTokenVault(vault TokenVault) Option {
	return func(c *RuleConfig) {
		c.TokenVault = vault
	}
}

// WithRetryable overrides the default retryability for the report action.
func WithRetryable(retryable bool) Option {
	return func(c *RuleConfig) {
		v := retryable
		c.Retryable = &v
	}
}

// WithFatal marks the violation as a hard escalation (stop upstream flow).
func WithFatal(fatal bool) Option {
	return func(c *RuleConfig) {
		c.Fatal = fatal
	}
}

// WithSafeUserMessage sets an end-user safe message (no internal details).
func WithSafeUserMessage(msg string) Option {
	return func(c *RuleConfig) {
		c.SafeUserMessage = msg
	}
}

func applyOptions(defaults RuleConfig, opts ...Option) RuleConfig {
	cfg := defaults
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func passReport(cfg RuleConfig) *guardy.Report {
	rep := &guardy.Report{
		Action:    guardy.ActionPass,
		Validator: cfg.Name,
		Code:      cfg.Code,
		Severity:  cfg.Severity,
	}
	finalizeReport(rep, cfg, guardy.ActionPass)
	return rep
}

func violationReport(cfg RuleConfig, action guardy.Action, fallbackReason string) *guardy.Report {
	reason := fallbackReason
	if cfg.Reason != "" {
		reason = cfg.Reason
	}
	rep := &guardy.Report{
		Action:    action,
		Validator: cfg.Name,
		Code:      cfg.Code,
		Severity:  cfg.Severity,
		Reason:    reason,
	}
	finalizeReport(rep, cfg, action)
	return rep
}

func finalizeReport(rep *guardy.Report, cfg RuleConfig, action guardy.Action) {
	FinalizeRuleReport(rep, cfg, action)
}

// FinalizeRuleReport applies RuleConfig control-flow fields to a manually built report.
func FinalizeRuleReport(rep *guardy.Report, cfg RuleConfig, action guardy.Action) {
	guardy.ApplyControlDefaults(rep, guardy.ControlSpec{
		Action:          action,
		Retryable:       cfg.Retryable,
		Fatal:           cfg.Fatal,
		SafeUserMessage: cfg.SafeUserMessage,
	})
}
