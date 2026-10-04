package ext

import (
	"context"
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

// wordBoundaryRE matches maximal Unicode token runs.
var wordBoundaryRE = regexp.MustCompile(`[\p{L}\p{N}\p{M}_]+`)

// NewWordlistValidator creates a blocklist or allowlist validator.
// Tokens are maximal runs of Unicode letters, numbers, combining marks and underscore.
// Other characters delimit tokens. WithLowercase applies [strings.ToLower] to both
// listed tokens and matches, without normalization or rewriting unmatched text.
// Each entry must be exactly one non-empty token; an invalid entry or mode panics.
// Replacements are literal strings; generated replacements are not re-examined.
func NewWordlistValidator(words []string, mode WordlistMode, opts ...Option) guardy.Validator[string] {
	if mode != Blocklist && mode != Allowlist {
		panic("ext: invalid wordlist mode")
	}
	cfg := applyOptions(RuleConfig{
		Action:               guardy.ActionBlock,
		Severity:             guardy.SeverityHigh,
		Name:                 defaultWordlistValidatorName,
		RedactionReplacement: defaultRedactionReplacement,
	}, opts...)
	if cfg.Action != guardy.ActionRedact {
		cfg.Action = guardy.ActionBlock
	}

	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		if match := wordBoundaryRE.FindStringIndex(w); match == nil || match[0] != 0 || match[1] != len(w) {
			panic("ext: wordlist entries must be non-empty single tokens")
		}
		key := w
		if cfg.Lowercase {
			key = strings.ToLower(key)
		}
		set[key] = struct{}{}
	}

	return &wordlistValidator{words: set, mode: mode, cfg: cfg}
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
	clean := w.cfg.RedactionReplacement
	if len(matches) > 0 {
		clean = replaceSpans(input, spans, func(original string) string {
			return storeTokenOrFallback(w.cfg.TokenVault, TokenNamespaceWordlist, original, w.cfg.RedactionReplacement)
		})
	}
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	report := violationReport(w.cfg, guardy.ActionRedact, reason)
	report.MutatedText = clean
	return clean, report, nil
}
