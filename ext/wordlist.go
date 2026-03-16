package ext

import (
	"context"
	"regexp"
	"strings"

	"github.com/skosovsky/guardy"
)

// Ensure Wordlist implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*Wordlist)(nil)

// WordlistMode defines whether the list is a blocklist or allowlist.
type WordlistMode int

// Wordlist mode constants.
const (
	Blocklist WordlistMode = iota // Block when any listed word is found
	Allowlist                     // Block when any word is not in the list
)

// Wordlist checks text against a set of words; returns block or redact per configuration.
type Wordlist struct {
	words             map[string]struct{}
	mode              WordlistMode
	action            guardy.Action // ActionBlock or ActionRedact
	code              string
	name              string
	lowercase         bool
	redactReplacement string
}

// WordlistOption configures a Wordlist validator.
type WordlistOption func(*Wordlist)

// WithWordlistName sets the validator name (default "wordlist").
func WithWordlistName(name string) WordlistOption {
	return func(w *Wordlist) {
		w.name = name
	}
}

// WithWordlistLowercase normalizes text and words to lowercase before matching.
func WithWordlistLowercase(lower bool) WordlistOption {
	return func(w *Wordlist) {
		w.lowercase = lower
	}
}

// WithWordlistRedaction sets the replacement for redact mode (default "[REDACTED]").
func WithWordlistRedaction(replacement string) WordlistOption {
	return func(w *Wordlist) {
		w.redactReplacement = replacement
	}
}

// NewWordlist creates a blocklist or allowlist validator.
// action must be guardy.ActionBlock or guardy.ActionRedact; for redact use WithWordlistRedaction or default "[REDACTED]".
func NewWordlist(words []string, mode WordlistMode, action guardy.Action, code string, opts ...WordlistOption) *Wordlist {
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		set[w] = struct{}{}
	}
	wl := &Wordlist{
		words:             set,
		mode:              mode,
		action:            action,
		code:              code,
		name:              "wordlist",
		redactReplacement: "[REDACTED]",
	}
	for _, opt := range opts {
		opt(wl)
	}
	if wl.lowercase {
		newSet := make(map[string]struct{}, len(set))
		for k := range set {
			newSet[strings.ToLower(k)] = struct{}{}
		}
		wl.words = newSet
	}
	return wl
}

// Name returns the validator name.
func (w *Wordlist) Name() string {
	if w.name != "" {
		return w.name
	}
	return "wordlist"
}

// Validate checks text against the word list and returns Report.
func (w *Wordlist) Validate(ctx context.Context, input string) (string, *guardy.Report, error) {
	run := input
	if w.lowercase {
		run = strings.ToLower(input)
	}
	tokens := tokenize(run)
	switch w.mode {
	case Blocklist:
		for _, t := range tokens {
			if _, ok := w.words[t]; ok {
				if w.action == guardy.ActionRedact && w.redactReplacement != "" {
					clean := replaceWordsInText(input, w.words, w.redactReplacement, w.lowercase)
					return clean, &guardy.Report{
						Action:      guardy.ActionRedact,
						Validator:   w.name,
						Reason:      "blocklisted word found",
						MutatedText: clean,
					}, nil
				}
				return input, &guardy.Report{
					Action:    guardy.ActionBlock,
					Validator: w.name,
					Reason:    "blocklisted word found",
				}, nil
			}
		}
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: w.name}, nil
	case Allowlist:
		if len(tokens) == 0 {
			if w.action == guardy.ActionRedact {
				return w.redactReplacement, &guardy.Report{
					Action:      guardy.ActionRedact,
					Validator:   w.name,
					Reason:      "no tokens",
					MutatedText: w.redactReplacement,
				}, nil
			}
			return input, &guardy.Report{Action: guardy.ActionBlock, Validator: w.name, Reason: "no tokens"}, nil
		}
		for _, t := range tokens {
			if _, ok := w.words[t]; !ok {
				if w.action == guardy.ActionRedact {
					clean := replaceAllowlistViolations(input, w.words, w.redactReplacement, w.lowercase)
					return clean, &guardy.Report{
						Action:      guardy.ActionRedact,
						Validator:   w.name,
						Reason:      "word not in allowlist",
						MutatedText: clean,
					}, nil
				}
				return input, &guardy.Report{
					Action:    guardy.ActionBlock,
					Validator: w.name,
					Reason:    "word not in allowlist",
				}, nil
			}
		}
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: w.name}, nil
	default:
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: w.name}, nil
	}
}

// wordBoundaryRE matches sequences of word chars (letters, digits, underscore).
var wordBoundaryRE = regexp.MustCompile(`\b[\p{L}\p{N}_]+\b`)

// tokenize extracts words (sequences bounded by non-word chars) to prevent punctuation bypass.
func tokenize(s string) []string {
	matches := wordBoundaryRE.FindAllString(s, -1)
	return matches
}

// replaceWordsInText replaces blocklisted words with replacement, preserving formatting and punctuation.
func replaceWordsInText(text string, words map[string]struct{}, replacement string, lowercase bool) string {
	if len(words) == 0 {
		return text
	}
	// Build \b(word1|word2|...)\b pattern with escaped words.
	var parts []string
	for w := range words {
		parts = append(parts, regexp.QuoteMeta(w))
	}
	pat := `\b(` + strings.Join(parts, "|") + `)\b`
	if lowercase {
		pat = `(?i)` + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return text
	}
	return re.ReplaceAllString(text, replacement)
}

// replaceAllowlistViolations replaces words not in allowlist, preserving formatting.
func replaceAllowlistViolations(text string, words map[string]struct{}, replacement string, lowercase bool) string {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return replacement
	}
	allowedPat := regexp.MustCompile(`\b[\p{L}\p{N}_]+\b`)
	return allowedPat.ReplaceAllStringFunc(text, func(m string) string {
		key := m
		if lowercase {
			key = strings.ToLower(m)
		}
		if _, ok := words[key]; ok {
			return m
		}
		return replacement
	})
}
