package ext

import (
	"context"
	"unicode/utf8"

	"github.com/skosovsky/guardy"
)

// Ensure Length implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*Length)(nil)

// Length enforces min/max rune length; use 0 to skip a bound.
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

// NewLength creates a validator that blocks when rune count is outside [minLen, maxLen].
func NewLength(minLen, maxLen int, action guardy.Action, code string, opts ...LengthOption) *Length {
	l := &Length{min: minLen, max: maxLen, action: action, code: code, name: "length"}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// MustLength is like NewLength but panics if minLen > maxLen (both > 0).
func MustLength(minLen, maxLen int, action guardy.Action, code string, opts ...LengthOption) *Length {
	if minLen > 0 && maxLen > 0 && minLen > maxLen {
		panic("ext: length validator: minLen > maxLen")
	}
	return NewLength(minLen, maxLen, action, code, opts...)
}

// Name returns the validator name.
func (l *Length) Name() string {
	if l.name != "" {
		return l.name
	}
	return "length"
}

// Validate checks rune length of text.
func (l *Length) Validate(ctx context.Context, input string) (string, *guardy.Report, error) {
	n := utf8.RuneCountInString(input)
	if l.min > 0 && n < l.min {
		return input, &guardy.Report{
			Action:    l.action,
			Validator: l.name,
			Reason:    "text too short",
		}, nil
	}
	if l.max > 0 && n > l.max {
		return input, &guardy.Report{
			Action:    l.action,
			Validator: l.name,
			Reason:    "text too long",
		}, nil
	}
	return input, &guardy.Report{Action: guardy.ActionPass, Validator: l.name}, nil
}
