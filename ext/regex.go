package ext

import (
	"context"
	"regexp"

	"github.com/skosovsky/guardy"
)

// Ensure Regex implements guardy.Validator at compile time.
var _ guardy.Validator = (*Regex)(nil)

// Regex matches text against a regular expression; on match returns block or redact (if placeholder set).
type Regex struct {
	re          *regexp.Regexp
	action      guardy.Action
	code        string
	placeholder string
	name        string
}

// RegexOption configures a Regex validator.
type RegexOption func(*Regex)

// WithRegexRedaction sets the replacement for matches; when set, validator returns ActionRedact with MutatedText.
func WithRegexRedaction(replacement string) RegexOption {
	return func(r *Regex) {
		r.placeholder = replacement
	}
}

// WithRegexPlaceholder is an alias for WithRegexRedaction.
func WithRegexPlaceholder(s string) RegexOption {
	return WithRegexRedaction(s)
}

// WithRegexName sets the validator name (default "regex").
func WithRegexName(name string) RegexOption {
	return func(r *Regex) {
		r.name = name
	}
}

// NewRegex creates a validator that matches pattern. action is used when placeholder is not set (block).
func NewRegex(pattern string, action guardy.Action, code string, opts ...RegexOption) (*Regex, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	r := &Regex{re: re, action: action, code: code, name: "regex"}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

// MustRegex is like NewRegex but panics on invalid pattern.
func MustRegex(pattern string, action guardy.Action, code string, opts ...RegexOption) *Regex {
	r, err := NewRegex(pattern, action, code, opts...)
	if err != nil {
		panic(err)
	}
	return r
}

// Name returns the validator name.
func (r *Regex) Name() string { return r.name }

// Validate runs the regex against text.
func (r *Regex) Validate(ctx context.Context, text string) (guardy.Report, error) {
	if !r.re.MatchString(text) {
		return guardy.Report{Action: guardy.ActionPass, Validator: r.name}, nil
	}
	if r.placeholder != "" {
		clean := r.re.ReplaceAllString(text, r.placeholder)
		return guardy.Report{
			Action:      guardy.ActionRedact,
			Validator:   r.name,
			Reason:      "pattern matched",
			MutatedText: clean,
		}, nil
	}
	return guardy.Report{
		Action:    r.action,
		Validator: r.name,
		Reason:    "pattern matched",
	}, nil
}
