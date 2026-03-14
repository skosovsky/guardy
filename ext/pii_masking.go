package ext

import (
	"context"
	"regexp"

	"github.com/skosovsky/guardy"
)

// Ensure PIIMasking implements guardy.Validator at compile time.
var _ guardy.Validator = (*PIIMasking)(nil)

// PIIMasking redacts PII (email, phone, credit card) using regex patterns.
type PIIMasking struct {
	patterns    []*regexp.Regexp
	replacement string
	name        string
}

// Common PII patterns (simplified; extend as needed).
var (
	emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	phonePattern = regexp.MustCompile(`(?:\+?1?[-.]?)?\(?\d{3}\)?[-.]?\d{3}[-.]?\d{4}`)
	cardPattern  = regexp.MustCompile(`\b(?:4\d{12}(?:\d{3})?|5[1-5]\d{14})\b`)
)

// PIIMaskingOption configures PIIMasking.
type PIIMaskingOption func(*PIIMasking)

// WithPIIReplacement sets the replacement string (default "[REDACTED]").
func WithPIIReplacement(s string) PIIMaskingOption {
	return func(p *PIIMasking) {
		p.replacement = s
	}
}

// WithPIIName sets the validator name.
func WithPIIName(name string) PIIMaskingOption {
	return func(p *PIIMasking) {
		p.name = name
	}
}

// NewPIIMasking creates a validator that redacts email, phone, and credit card numbers.
func NewPIIMasking(opts ...PIIMaskingOption) *PIIMasking {
	p := &PIIMasking{
		patterns:    []*regexp.Regexp{emailPattern, phonePattern, cardPattern},
		replacement: "[REDACTED]",
		name:        "pii_masking",
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns the validator name.
func (p *PIIMasking) Name() string { return p.name }

// Validate redacts PII in text and returns the mutated string.
func (p *PIIMasking) Validate(ctx context.Context, text string) (guardy.Report, error) {
	out := text
	changed := false
	for _, re := range p.patterns {
		if re.MatchString(out) {
			out = re.ReplaceAllString(out, p.replacement)
			changed = true
		}
	}
	if changed {
		return guardy.Report{
			Action:      guardy.ActionRedact,
			Validator:   p.name,
			Reason:      "PII detected",
			MutatedText: out,
		}, nil
	}
	return guardy.Report{Action: guardy.ActionPass, Validator: p.name}, nil
}
