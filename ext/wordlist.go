package ext

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/skosovsky/guardy"
)

// WordlistMode defines whether the list is a blocklist or allowlist.
type WordlistMode int

// Wordlist mode constants.
const (
	Blocklist WordlistMode = iota // Block when any listed word is found
	Allowlist                     // Block when any word is not in the list
)

type wordlistValidator struct {
	words map[string]struct{}
	mode  WordlistMode
	cfg   RuleConfig
}

// Ensure wordlist validator implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*wordlistValidator)(nil)

const defaultWordlistValidatorName = "wordlist_validator"
const wordlistComponent = "wordlist"

// wordBoundaryRE matches maximal Unicode token runs.
var wordBoundaryRE = regexp.MustCompile(`[\p{L}\p{N}\p{M}_]+`)

// NewWordlistValidator creates a blocklist or allowlist validator.
// Tokens are maximal runs of Unicode letters, numbers, combining marks and underscore.
// Other characters delimit tokens. WithLowercase applies [strings.ToLower] to both
// listed tokens and matches, without normalization or rewriting unmatched text.
// Each entry must be exactly one non-empty token; invalid entries or modes return
// safe ConfigurationError identifiers without wordlist contents.
// Replacements are literal strings; generated replacements are not re-examined.
func NewWordlistValidator(words []string, mode WordlistMode, opts ...Option) (guardy.Validator[string], error) {
	if mode != Blocklist && mode != Allowlist {
		return nil, &guardy.ConfigurationError{Component: wordlistComponent, Field: "mode", Code: "invalid", Cause: nil}
	}
	cfg, err := applyOptions("wordlist", RuleConfig{
		Action:               guardy.ActionBlock,
		Severity:             guardy.SeverityHigh,
		Name:                 defaultWordlistValidatorName,
		RedactionReplacement: defaultRedactionReplacement,
	}, opts...)
	if err != nil {
		return nil, err
	}
	if err := validateRuleOptions("wordlist", cfg); err != nil {
		return nil, err
	}

	set := make(map[string]struct{}, len(words))
	for i, w := range words {
		if match := wordBoundaryRE.FindStringIndex(w); match == nil || match[0] != 0 || match[1] != len(w) {
			return nil, &guardy.ConfigurationError{
				Component: wordlistComponent,
				Field:     fmt.Sprintf("words[%d]", i),
				Code:      "single_token",
				Cause:     nil,
			}
		}
		key := w
		if cfg.Lowercase {
			key = strings.ToLower(key)
		}
		set[key] = struct{}{}
	}

	return &wordlistValidator{words: set, mode: mode, cfg: cfg}, nil
}

// MustWordlistValidator is for static configuration and panics when construction fails.
func MustWordlistValidator(words []string, mode WordlistMode, opts ...Option) guardy.Validator[string] {
	v, err := NewWordlistValidator(words, mode, opts...)
	if err != nil {
		panic(err)
	}
	return v
}

func (w *wordlistValidator) Validate(ctx context.Context, input string) (string, *guardy.Report, error) {
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	matches := wordBoundaryRE.FindAllStringIndex(input, -1)
	spans := make([]textSpan, 0, len(matches))
	for _, match := range matches {
		token := input[match[0]:match[1]]
		if w.cfg.Lowercase {
			token = strings.ToLower(token)
		}
		_, listed := w.words[token]
		if (w.mode == Blocklist && listed) || (w.mode == Allowlist && !listed) {
			spans = append(spans, textSpan{start: match[0], end: match[1]})
		}
	}
	if len(spans) == 0 && (w.mode == Blocklist || len(matches) > 0) {
		return input, passReport(w.cfg), nil
	}
	reason := "blocklisted word found"
	if w.mode == Allowlist {
		reason = "word not in allowlist"
		if len(matches) == 0 {
			reason = "no tokens"
		}
	}
	if w.cfg.Action == guardy.ActionBlock {
		return input, violationReport(w.cfg, guardy.ActionBlock, reason), nil
	}
	var clean string
	var err error
	if len(matches) == 0 {
		clean, err = storeRedaction(w.cfg.TokenVault, TokenNamespaceWordlist, input, w.cfg.RedactionReplacement)
	} else {
		clean, err = replaceSpans(input, spans, func(original string) (string, error) {
			return storeRedaction(w.cfg.TokenVault, TokenNamespaceWordlist, original, w.cfg.RedactionReplacement)
		})
	}
	if fault := errors.Join(err, ctx.Err()); fault != nil {
		return input, nil, fault
	}
	report := violationReport(w.cfg, guardy.ActionRedact, reason)
	report.MutatedText = clean
	return clean, report, nil
}
