package ext

import (
	"context"
	"regexp"

	"github.com/skosovsky/guardy"
)

// Ensure TagSanitizer implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*TagSanitizer)(nil)

// TagSanitizer is a WAF-style validator that blocks on system XML tag injection attempts
// (e.g. <system>, </system>, case-insensitive with optional whitespace).
type TagSanitizer struct {
	re   *regexp.Regexp
	name string
}

// DefaultTagPattern matches common system/prompt injection tags.
// Includes <system ...> with attributes to prevent bypass via e.g. <system role="x">.
var DefaultTagPattern = `(?i)<\s*system\b[^>]*>|<\s*/\s*system\s*>`

// NewTagSanitizer creates a validator that blocks when the pattern matches.
// If pattern is empty, DefaultTagPattern is used.
func NewTagSanitizer(pattern string) (*TagSanitizer, error) {
	if pattern == "" {
		pattern = DefaultTagPattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &TagSanitizer{re: re, name: "tag_sanitizer"}, nil
}

// MustTagSanitizer is like NewTagSanitizer but panics on invalid pattern.
func MustTagSanitizer(pattern string) *TagSanitizer {
	t, err := NewTagSanitizer(pattern)
	if err != nil {
		panic(err)
	}
	return t
}

// Name returns the validator name.
func (t *TagSanitizer) Name() string {
	if t.name != "" {
		return t.name
	}
	return "tag_sanitizer"
}

// Validate blocks when system tags are found.
func (t *TagSanitizer) Validate(ctx context.Context, input string) (string, *guardy.Report, error) {
	if t.re.MatchString(input) {
		return input, &guardy.Report{
			Action:    guardy.ActionBlock,
			Validator: t.name,
			Reason:    "system tag injection attempt",
		}, nil
	}
	return input, &guardy.Report{Action: guardy.ActionPass, Validator: t.name}, nil
}
