package ext

import (
	"context"
	"regexp"

	"github.com/skosovsky/guardy"
)

// Ensure Regex implements guardy.Validator at compile time.
var _ guardy.Validator = (*Regex)(nil)

// Regex is a validator that matches text against a regular expression.
// On match: if WithRegexRedaction was set, returns Redact and CleanText; otherwise returns Block (or configured action).
type Regex struct {
	re          *regexp.Regexp
	action      guardy.Action
	code        string
	placeholder string // non-empty when WithRegexRedaction is used
	name        string
}

// RegexOption configures a Regex validator.
type RegexOption func(*Regex)

// WithRegexRedaction sets the replacement string for matches; when set, validator returns ActionRedact with CleanText instead of Block.
func WithRegexRedaction(replacement string) RegexOption {
	return func(r *Regex) {
		r.placeholder = replacement
	}
}

// WithRegexPlaceholder is an alias for WithRegexRedaction (same behavior).
func WithRegexPlaceholder(s string) RegexOption {
	return WithRegexRedaction(s)
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
		re:     re,
		action: action,
		code:   code,
		name:   "regex",
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

// Validate runs the regex against input.Data.
func (r *Regex) Validate(ctx context.Context, input *guardy.Input) (guardy.Result, error) {
	text := ""
	if input != nil {
		text = input.Data
	}
	if !r.re.MatchString(text) {
		return guardy.Result{Passed: true, Action: guardy.Pass}, nil
	}
	if r.placeholder != "" {
		clean := r.re.ReplaceAllString(text, r.placeholder)
		return guardy.Result{
			Passed:    false,
			Action:    guardy.Redact,
			Code:      r.code,
			Reason:    "pattern matched",
			CleanText: clean,
		}, nil
	}
	return guardy.Result{
		Passed: false,
		Action: r.action,
		Code:   r.code,
		Reason: "pattern matched",
	}, nil
}

// Name returns the validator name.
func (r *Regex) Name() string {
	return r.name
}
