package ext

import (
	"context"
	"regexp"

	"github.com/skosovsky/guardy"
)

// Ensure Regex implements guardy.Validator at compile time.
var _ guardy.Validator = (*Regex)(nil)

// Regex is a validator that matches text against a regular expression.
// On match it returns the configured Action and Code; for Redact it replaces matches with Placeholder.
type Regex struct {
	re          *regexp.Regexp
	action      guardy.Action
	code        string
	placeholder string
	name        string
}

// RegexOption configures a Regex validator.
type RegexOption func(*Regex)

// WithRegexPlaceholder sets the replacement string for Redact action (default "[REDACTED]").
func WithRegexPlaceholder(s string) RegexOption {
	return func(r *Regex) {
		r.placeholder = s
	}
}

// WithRegexName sets the validator name for logging (default "regex").
func WithRegexName(name string) RegexOption {
	return func(r *Regex) {
		r.name = name
	}
}

// NewRegex creates a validator that matches text against pattern.
// If the pattern matches, it returns the given action and code.
// For Redact, matches are replaced with the placeholder.
func NewRegex(pattern string, action guardy.Action, code string, opts ...RegexOption) (*Regex, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	r := &Regex{
		re:          re,
		action:      action,
		code:        code,
		placeholder: "[REDACTED]",
		name:        "regex",
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

// MustRegex is like NewRegex but panics on invalid pattern (for init-time use).
func MustRegex(pattern string, action guardy.Action, code string, opts ...RegexOption) *Regex {
	r, err := NewRegex(pattern, action, code, opts...)
	if err != nil {
		panic(err)
	}
	return r
}

// Validate runs the regex against input.Text.
func (r *Regex) Validate(ctx context.Context, input guardy.Input) (guardy.Result, error) {
	if ctx.Err() != nil {
		return guardy.Result{}, ctx.Err()
	}
	text := input.Text
	if !r.re.MatchString(text) {
		return guardy.Result{Passed: true, Action: guardy.Pass}, nil
	}
	switch r.action {
	case guardy.Redact:
		clean := r.re.ReplaceAllString(text, r.placeholder)
		return guardy.Result{
			Passed:    false,
			Action:    guardy.Redact,
			Code:      r.code,
			Reason:    "pattern matched",
			CleanText: clean,
		}, nil
	default:
		return guardy.Result{
			Passed: false,
			Action: r.action,
			Code:   r.code,
			Reason: "pattern matched",
		}, nil
	}
}

// Name returns the validator name.
func (r *Regex) Name() string {
	return r.name
}
