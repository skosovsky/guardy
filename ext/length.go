package ext

import (
	"context"
	"unicode/utf8"

	"github.com/skosovsky/guardy"
)

// Ensure Length implements guardy.Validator at compile time.
var _ guardy.Validator = (*Length)(nil)

// Length is a validator that enforces min/max length of input text (in runes).
type Length struct {
	min    int
	max    int
	action guardy.Action
	code   string
	name   string
}

// LengthOption configures a Length validator.
type LengthOption func(*Length)

// WithLengthName sets the validator name (default "length").
func WithLengthName(name string) LengthOption {
	return func(l *Length) {
		l.name = name
	}
}

// NewLength creates a validator that blocks when len(runes(text)) < minLen or > maxLen.
// Use 0 for minLen or maxLen to skip that check.
// If both minLen and maxLen are positive and minLen > maxLen, the validator will always block
// (no text can satisfy both constraints); use MustLength for init-time validation that panics on invalid config.
func NewLength(minLen, maxLen int, action guardy.Action, code string, opts ...LengthOption) *Length {
	l := &Length{
		min:    minLen,
		max:    maxLen,
		action: action,
		code:   code,
		name:   "length",
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// MustLength is like NewLength but panics if both minLen and maxLen are positive and minLen > maxLen (for init-time use).
func MustLength(minLen, maxLen int, action guardy.Action, code string, opts ...LengthOption) *Length {
	if minLen > 0 && maxLen > 0 && minLen > maxLen {
		panic("ext: length validator: minLen > maxLen")
	}
	return NewLength(minLen, maxLen, action, code, opts...)
}

// Validate checks the rune length of input.Text.
func (l *Length) Validate(ctx context.Context, input guardy.Input) (guardy.Result, error) {
	if ctx.Err() != nil {
		return guardy.Result{}, ctx.Err()
	}
	n := utf8.RuneCountInString(input.Text)
	if l.min > 0 && n < l.min {
		return guardy.Result{
			Passed: false,
			Action: l.action,
			Code:   l.code,
			Reason: "text too short",
		}, nil
	}
	if l.max > 0 && n > l.max {
		return guardy.Result{
			Passed: false,
			Action: l.action,
			Code:   l.code,
			Reason: "text too long",
		}, nil
	}
	return guardy.Result{Passed: true, Action: guardy.Pass}, nil
}

// Name returns the validator name.
func (l *Length) Name() string {
	return l.name
}
