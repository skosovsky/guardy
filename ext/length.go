package ext

import (
	"context"
	"unicode/utf8"

	"github.com/skosovsky/guardy"
)

type lengthValidator struct {
	min int
	max int
	cfg RuleConfig
}

// Ensure length validator implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*lengthValidator)(nil)

const defaultLengthValidatorName = "length_validator"

// NewLengthValidator blocks rune counts outside the enabled bounds.
// Bounds must be nonnegative; zero disables that side. Positive min must not exceed max.
func NewLengthValidator(minLen, maxLen int, opts ...Option) (guardy.Validator[string], error) {
	if minLen < 0 || maxLen < 0 || (minLen > 0 && maxLen > 0 && minLen > maxLen) {
		return nil, ruleConfigurationError("length", "bounds", "invalid", nil)
	}
	cfg, err := applyOptions("length", RuleConfig{
		Action:   guardy.ActionBlock,
		Severity: guardy.SeverityMedium,
		Name:     defaultLengthValidatorName,
	}, opts...)
	if err != nil {
		return nil, err
	}
	if err := validateRuleOptions("length", cfg); err != nil {
		return nil, err
	}

	return &lengthValidator{
		min: minLen,
		max: maxLen,
		cfg: cfg,
	}, nil
}

// MustLengthValidator panics on any configuration rejected by NewLengthValidator.
func MustLengthValidator(minLen, maxLen int, opts ...Option) guardy.Validator[string] {
	v, err := NewLengthValidator(minLen, maxLen, opts...)
	if err != nil {
		panic(err)
	}
	return v
}

func (l *lengthValidator) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	n := utf8.RuneCountInString(input)
	if l.min > 0 && n < l.min {
		return input, violationReport(l.cfg, guardy.ActionBlock, "text too short"), nil
	}
	if l.max > 0 && n > l.max {
		return input, violationReport(l.cfg, guardy.ActionBlock, "text too long"), nil
	}
	return input, passReport(l.cfg), nil
}
