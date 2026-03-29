package ext

import (
	"context"
	"regexp"

	"github.com/skosovsky/guardy"
)

type tagSanitizerValidator struct {
	re  *regexp.Regexp
	cfg RuleConfig
}

// Ensure tag sanitizer implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*tagSanitizerValidator)(nil)

// DefaultTagPattern matches common system/prompt injection tags.
const DefaultTagPattern = `(?i)<\s*system\b[^>]*>|<\s*/\s*system\s*>`

const defaultTagSanitizerName = "tag_sanitizer_validator"

// NewTagSanitizerValidator creates a validator that blocks on tag pattern match.
func NewTagSanitizerValidator(pattern string, opts ...Option) (guardy.Validator[string], error) {
	if pattern == "" {
		pattern = DefaultTagPattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	cfg := applyOptions(RuleConfig{
		Action:   guardy.ActionBlock,
		Severity: guardy.SeverityHigh,
		Name:     defaultTagSanitizerName,
	}, opts...)
	cfg.Action = guardy.ActionBlock
	return &tagSanitizerValidator{re: re, cfg: cfg}, nil
}

// MustTagSanitizerValidator is like NewTagSanitizerValidator but panics on invalid pattern.
func MustTagSanitizerValidator(pattern string, opts ...Option) guardy.Validator[string] {
	v, err := NewTagSanitizerValidator(pattern, opts...)
	if err != nil {
		panic(err)
	}
	return v
}

func (t *tagSanitizerValidator) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	if t.re.MatchString(input) {
		return input, violationReport(t.cfg, guardy.ActionBlock, "system tag injection attempt"), nil
	}
	return input, passReport(t.cfg), nil
}
