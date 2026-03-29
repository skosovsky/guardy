package ext

import (
	"context"
	"regexp"

	"github.com/skosovsky/guardy"
)

type piiValidator struct {
	patterns []*regexp.Regexp
	cfg      RuleConfig
}

// Ensure PII validator implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*piiValidator)(nil)

// Common PII patterns (simplified; extend as needed).
var (
	emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	phonePattern = regexp.MustCompile(`(?:\+?1?[-.]?)?\(?\d{3}\)?[-.]?\d{3}[-.]?\d{4}`)
	cardPattern  = regexp.MustCompile(`\b(?:4\d{12}(?:\d{3})?|5[1-5]\d{14})\b`)
)

const defaultPIIValidatorName = "pii_validator"

// NewPIIValidator creates a validator for common PII patterns (email, phone, card).
func NewPIIValidator(opts ...Option) guardy.Validator[string] {
	cfg := applyOptions(RuleConfig{
		Action:               guardy.ActionRedact,
		Severity:             guardy.SeverityHigh,
		Name:                 defaultPIIValidatorName,
		RedactionReplacement: "[REDACTED]",
	}, opts...)
	if cfg.Action != guardy.ActionBlock && cfg.Action != guardy.ActionRedact {
		cfg.Action = guardy.ActionRedact
	}
	return &piiValidator{
		patterns: []*regexp.Regexp{emailPattern, phonePattern, cardPattern},
		cfg:      cfg,
	}
}

func (p *piiValidator) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	if p.cfg.Action == guardy.ActionBlock {
		for _, re := range p.patterns {
			if re.MatchString(input) {
				return input, violationReport(p.cfg, guardy.ActionBlock, "PII detected"), nil
			}
		}
		return input, passReport(p.cfg), nil
	}

	out := input
	changed := false
	for _, re := range p.patterns {
		if p.cfg.TokenVault != nil {
			out = re.ReplaceAllStringFunc(out, func(match string) string {
				changed = true
				return storeTokenOrFallback(
					p.cfg.TokenVault,
					TokenNamespacePII,
					match,
					p.cfg.RedactionReplacement,
				)
			})
			continue
		}
		if re.MatchString(out) {
			changed = true
		}
		out = re.ReplaceAllString(out, p.cfg.RedactionReplacement)
	}
	if !changed {
		return input, passReport(p.cfg), nil
	}

	rep := violationReport(p.cfg, guardy.ActionRedact, "PII detected")
	rep.MutatedText = out
	return out, rep, nil
}
