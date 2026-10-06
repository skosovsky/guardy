package ext

import (
	"context"
	"regexp"

	"github.com/skosovsky/guardy"
)

// TagPatternValidator matches text patterns, not parsed XML or instructions.
type TagPatternValidator struct {
	re  *regexp.Regexp
	cfg RuleConfig
}

// Ensure tag sanitizer implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*TagPatternValidator)(nil)

// DefaultTagPattern matches opening/closing XML-like system tags.
const DefaultTagPattern = `(?i)<\s*system\b[^>]*>|<\s*/\s*system\s*>`

const defaultTagPatternName = "tag_pattern_validator"

// NewTagPatternValidator creates a validator that blocks on tag pattern match.
// It is a text matcher, not an XML parser, WAF or general instruction detector.
func NewTagPatternValidator(pattern string, opts ...Option) (guardy.Validator[string], error) {
	if pattern == "" {
		pattern = DefaultTagPattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, ruleConfigurationError("tag_pattern", "pattern", "invalid", err)
	}
	cfg, err := applyOptions("tag_pattern", RuleConfig{
		Action:   guardy.ActionBlock,
		Severity: guardy.SeverityHigh,
		Name:     defaultTagPatternName,
	}, opts...)
	if err != nil {
		return nil, err
	}
	if err := validateRuleOptions("tag_pattern", cfg); err != nil {
		return nil, err
	}

	return &TagPatternValidator{re: re, cfg: cfg}, nil
}

// MustTagPatternValidator panics on any configuration rejected by NewTagPatternValidator.
func MustTagPatternValidator(pattern string, opts ...Option) guardy.Validator[string] {
	v, err := NewTagPatternValidator(pattern, opts...)
	if err != nil {
		panic(err)
	}
	return v
}

func (t *TagPatternValidator) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	if t.re.MatchString(input) {
		return input, violationReport(t.cfg, guardy.ActionBlock, "system tag pattern matched"), nil
	}
	return input, passReport(t.cfg), nil
}
