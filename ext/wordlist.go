package ext

import (
	"context"
	"regexp"
	"sort"
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
	words            map[string]struct{}
	mode             WordlistMode
	cfg              RuleConfig
	blocklistReplace *regexp.Regexp
}

// Ensure wordlist validator implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*wordlistValidator)(nil)

const defaultWordlistValidatorName = "wordlist_validator"

// wordBoundaryRE matches sequences of word chars (letters, digits, underscore).
var wordBoundaryRE = regexp.MustCompile(`\b[\p{L}\p{N}_]+\b`)

// NewWordlistValidator creates a blocklist or allowlist validator.
func NewWordlistValidator(words []string, mode WordlistMode, opts ...Option) guardy.Validator[string] {
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
		key := w
		if cfg.Lowercase {
			key = strings.ToLower(key)
		}
		set[key] = struct{}{}
	}

	var blocklistRE *regexp.Regexp
	if mode == Blocklist && cfg.Action == guardy.ActionRedact && len(set) > 0 {
		blocklistRE = compileWordAlternation(set, cfg.Lowercase)
	}

	return &wordlistValidator{
		words:            set,
		mode:             mode,
		cfg:              cfg,
		blocklistReplace: blocklistRE,
	}
}

func (w *wordlistValidator) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	run := input
	if w.cfg.Lowercase {
		run = strings.ToLower(input)
	}
	tokens := tokenize(run)

	switch w.mode {
	case Blocklist:
		for _, t := range tokens {
			if _, ok := w.words[t]; ok {
				if w.cfg.Action == guardy.ActionRedact {
					clean := w.redactBlocklist(input)
					rep := violationReport(w.cfg, guardy.ActionRedact, "blocklisted word found")
					rep.MutatedText = clean
					return clean, rep, nil
				}
				return input, violationReport(w.cfg, guardy.ActionBlock, "blocklisted word found"), nil
			}
		}
		return input, passReport(w.cfg), nil
	case Allowlist:
		if len(tokens) == 0 {
			if w.cfg.Action == guardy.ActionRedact {
				clean := w.cfg.RedactionReplacement
				rep := violationReport(w.cfg, guardy.ActionRedact, "no tokens")
				rep.MutatedText = clean
				return clean, rep, nil
			}
			return input, violationReport(w.cfg, guardy.ActionBlock, "no tokens"), nil
		}
		for _, t := range tokens {
			if _, ok := w.words[t]; !ok {
				if w.cfg.Action == guardy.ActionRedact {
					clean := w.redactAllowlist(input)
					rep := violationReport(w.cfg, guardy.ActionRedact, "word not in allowlist")
					rep.MutatedText = clean
					return clean, rep, nil
				}
				return input, violationReport(w.cfg, guardy.ActionBlock, "word not in allowlist"), nil
			}
		}
		return input, passReport(w.cfg), nil
	default:
		return input, passReport(w.cfg), nil
	}
}

// tokenize extracts words (sequences bounded by non-word chars) to prevent punctuation bypass.
func tokenize(s string) []string {
	return wordBoundaryRE.FindAllString(s, -1)
}

func compileWordAlternation(words map[string]struct{}, caseInsensitive bool) *regexp.Regexp {
	parts := make([]string, 0, len(words))
	for w := range words {
		parts = append(parts, regexp.QuoteMeta(w))
	}
	sort.Strings(parts)
	pat := `\b(` + strings.Join(parts, "|") + `)\b`
	if caseInsensitive {
		pat = `(?i)` + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil
	}
	return re
}

func (w *wordlistValidator) redactBlocklist(text string) string {
	replacement := w.cfg.RedactionReplacement
	if w.blocklistReplace == nil {
		return text
	}
	if w.cfg.TokenVault == nil {
		return w.blocklistReplace.ReplaceAllString(text, replacement)
	}
	return w.blocklistReplace.ReplaceAllStringFunc(text, func(match string) string {
		return storeTokenOrFallback(
			w.cfg.TokenVault,
			TokenNamespaceWordlist,
			match,
			replacement,
		)
	})
}

func (w *wordlistValidator) redactAllowlist(text string) string {
	replacement := w.cfg.RedactionReplacement
	return wordBoundaryRE.ReplaceAllStringFunc(text, func(match string) string {
		key := match
		if w.cfg.Lowercase {
			key = strings.ToLower(match)
		}
		if _, ok := w.words[key]; ok {
			return match
		}
		if w.cfg.TokenVault != nil {
			return storeTokenOrFallback(
				w.cfg.TokenVault,
				TokenNamespaceWordlist,
				match,
				replacement,
			)
		}
		return replacement
	})
}
