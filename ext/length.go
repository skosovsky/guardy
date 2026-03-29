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

// NewLengthValidator creates a validator that blocks when rune count is outside [minLen, maxLen].
func NewLengthValidator(minLen, maxLen int, opts ...Option) guardy.Validator[string] {
	cfg := applyOptions(RuleConfig{
		Action:   guardy.ActionBlock,
		Severity: guardy.SeverityMedium,
		Name:     defaultLengthValidatorName,
	}, opts...)
	cfg.Action = guardy.ActionBlock
	return &lengthValidator{
		min: minLen,
		max: maxLen,
		cfg: cfg,
	}
}

// MustLengthValidator is like NewLengthValidator but panics if minLen > maxLen (both > 0).
func MustLengthValidator(minLen, maxLen int, opts ...Option) guardy.Validator[string] {
	if minLen > 0 && maxLen > 0 && minLen > maxLen {
		panic("ext: length validator: minLen > maxLen")
	}
	return NewLengthValidator(minLen, maxLen, opts...)
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
