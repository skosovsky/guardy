package ext

import (
	"context"
	"regexp"

	"github.com/skosovsky/guardy"
)

type regexValidator struct {
	re  *regexp.Regexp
	cfg RuleConfig
}

// Ensure regex validator implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*regexValidator)(nil)

const defaultRegexValidatorName = "regex_validator"

// NewRegexValidator creates a regex validator.
func NewRegexValidator(pattern string, opts ...Option) (guardy.Validator[string], error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, ruleConfigurationError("regex", "pattern", "invalid", err)
	}
	cfg, err := applyOptions("regex", RuleConfig{
		Action:               guardy.ActionBlock,
		Severity:             guardy.SeverityHigh,
		Name:                 defaultRegexValidatorName,
		RedactionReplacement: defaultRedactionReplacement,
	}, opts...)
	if err != nil {
		return nil, err
	}
	if err := validateRuleOptions("regex", cfg); err != nil {
		return nil, err
	}

	return &regexValidator{re: re, cfg: cfg}, nil
}

// MustRegexValidator is like NewRegexValidator but panics on invalid pattern.
func MustRegexValidator(pattern string, opts ...Option) guardy.Validator[string] {
	v, err := NewRegexValidator(pattern, opts...)
	if err != nil {
		panic(err)
	}
	return v
}

func (r *regexValidator) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	if !r.re.MatchString(input) {
		return input, passReport(r.cfg), nil
	}
	if r.cfg.Action == guardy.ActionRedact {
		clean := r.re.ReplaceAllLiteralString(input, r.cfg.RedactionReplacement)
		rep := violationReport(r.cfg, guardy.ActionRedact, "pattern matched")
		rep.MutatedText = clean
		return clean, rep, nil
	}

	return input, violationReport(r.cfg, guardy.ActionBlock, "pattern matched"), nil
}
