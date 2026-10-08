package ext

import (
	"fmt"
	"reflect"

	"github.com/skosovsky/guardy"
)

const defaultRedactionReplacement = "[REDACTED]"

type ruleOptions uint8

const (
	ruleReplacement ruleOptions = 1 << iota
	ruleLowercase
	ruleVault
)

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
	specified            ruleOptions
}

// Option configures RuleConfig for built-in validators.
type Option func(*RuleConfig)

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
		c.specified |= ruleReplacement
		c.RedactionReplacement = replacement
	}
}

// WithLowercase enables case mapping for matching in validators that support it.
// It does not normalize Unicode or rewrite unmatched text.
func WithLowercase(lower bool) Option {
	return func(c *RuleConfig) {
		c.specified |= ruleLowercase
		c.Lowercase = lower
	}
}

// WithTokenVault enables reversible token redaction using the provided vault.
// Built-in validators pass explicit namespaces (for example PII, WORDLIST).
func WithTokenVault(vault TokenVault) Option {
	return func(c *RuleConfig) {
		c.specified |= ruleVault
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

func applyOptions(component string, defaults RuleConfig, opts ...Option) (RuleConfig, error) {
	cfg := defaults
	for i, opt := range opts {
		if opt == nil {
			return cfg, ruleConfigurationError(component, fmt.Sprintf("options[%d]", i), "required", nil)
		}
		opt(&cfg)
	}
	return cfg, nil
}

func ruleConfigurationError(component, field, code string, cause error) error {
	return &guardy.ConfigurationError{Component: component, Field: field, Code: code, Cause: cause}
}

func nilComponent(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	case reflect.Invalid,
		reflect.Bool,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr,
		reflect.Float32,
		reflect.Float64,
		reflect.Complex64,
		reflect.Complex128,
		reflect.Array,
		reflect.String,
		reflect.Struct,
		reflect.UnsafePointer:
		return false
	default:
		return false
	}
}

// validateRuleOptions keeps per-validator capabilities small and explicit.
func validateRuleOptions(component string, cfg RuleConfig) error {
	redactable := component == "regex" || component == wordlistComponent || component == "pii"
	validAction := cfg.Action == guardy.ActionBlock || (redactable && cfg.Action == guardy.ActionRedact)
	if component == "technical_json" {
		validAction = cfg.Action == guardy.ActionPass
	}
	if !validAction {
		return ruleConfigurationError(component, "action", "unsupported", nil)
	}
	if component != wordlistComponent && (cfg.specified&ruleLowercase != 0 || cfg.Lowercase) {
		return ruleConfigurationError(component, "lowercase", "unsupported", nil)
	}
	defaultReplacement := ""
	if redactable {
		defaultReplacement = defaultRedactionReplacement
	}
	if (cfg.specified&ruleReplacement != 0 || cfg.RedactionReplacement != defaultReplacement) &&
		(!redactable || cfg.Action != guardy.ActionRedact) {
		return ruleConfigurationError(component, "replacement", "unsupported", nil)
	}
	return validateVaultOptions(component, cfg, defaultReplacement)
}

func validateVaultOptions(component string, cfg RuleConfig, defaultReplacement string) error {
	if cfg.specified&ruleVault != 0 || cfg.TokenVault != nil {
		if (component != wordlistComponent && component != "pii") || cfg.Action != guardy.ActionRedact {
			return ruleConfigurationError(component, "vault", "unsupported", nil)
		}
		if cfg.specified&ruleReplacement != 0 || cfg.RedactionReplacement != defaultReplacement {
			return ruleConfigurationError(component, "replacement", "unsupported_with_vault", nil)
		}
		if nilComponent(cfg.TokenVault) {
			return ruleConfigurationError(component, "vault", "required", nil)
		}
	}
	return nil
}

func passReport(cfg RuleConfig) *guardy.Report {
	rep := &guardy.Report{
		Action:    guardy.ActionPass,
		Validator: cfg.Name,
		Code:      cfg.Code,
		Severity:  cfg.Severity,
	}
	guardy.FinishReport(rep, guardy.ControlSpec{Action: guardy.ActionPass})
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
	guardy.FinishReport(rep, guardy.ControlSpec{
		Action:          action,
		Retryable:       cfg.Retryable,
		Fatal:           cfg.Fatal,
		SafeUserMessage: cfg.SafeUserMessage,
	})
}
