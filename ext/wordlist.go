package ext

import (
	"context"
	"strings"

	"github.com/skosovsky/guardy"
)

// Ensure Wordlist implements guardy.Validator at compile time.
var _ guardy.Validator = (*Wordlist)(nil)

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
func (w *Wordlist) Name() string { return w.name }

// Validate checks text against the word list and returns Report.
func (w *Wordlist) Validate(ctx context.Context, text string) (guardy.Report, error) {
	run := text
	if w.lowercase {
		run = strings.ToLower(text)
	}
	tokens := tokenize(run)
	switch w.mode {
	case Blocklist:
		for _, t := range tokens {
			if _, ok := w.words[t]; ok {
				if w.action == guardy.ActionRedact && w.redactReplacement != "" {
					clean := replaceWordsInText(text, w.words, w.redactReplacement, w.lowercase)
					return guardy.Report{
						Action:      guardy.ActionRedact,
						Validator:   w.name,
						Reason:      "blocklisted word found",
						MutatedText: clean,
					}, nil
				}
				return guardy.Report{
					Action:    guardy.ActionBlock,
					Validator: w.name,
					Reason:    "blocklisted word found",
				}, nil
			}
		}
		return guardy.Report{Action: guardy.ActionPass, Validator: w.name}, nil
	case Allowlist:
		if len(tokens) == 0 {
			if w.action == guardy.ActionRedact {
				return guardy.Report{
					Action:      guardy.ActionRedact,
					Validator:   w.name,
					Reason:      "no tokens",
					MutatedText: w.redactReplacement,
				}, nil
			}
			return guardy.Report{Action: guardy.ActionBlock, Validator: w.name, Reason: "no tokens"}, nil
		}
		for _, t := range tokens {
			if _, ok := w.words[t]; !ok {
				if w.action == guardy.ActionRedact {
					clean := replaceAllowlistViolations(text, w.words, w.redactReplacement, w.lowercase)
					return guardy.Report{
						Action:      guardy.ActionRedact,
						Validator:   w.name,
						Reason:      "word not in allowlist",
						MutatedText: clean,
					}, nil
				}
				return guardy.Report{
					Action:    guardy.ActionBlock,
					Validator: w.name,
					Reason:    "word not in allowlist",
				}, nil
			}
		}
		return guardy.Report{Action: guardy.ActionPass, Validator: w.name}, nil
	default:
		return guardy.Report{Action: guardy.ActionPass, Validator: w.name}, nil
	}
}

func tokenize(s string) []string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return nil
	}
	return f
}

func replaceWordsInText(text string, words map[string]struct{}, replacement string, lowercase bool) string {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return text
	}
	var out []string
	for _, t := range tokens {
		key := t
		if lowercase {
			key = strings.ToLower(t)
		}
		if _, ok := words[key]; ok {
			out = append(out, replacement)
		} else {
			out = append(out, t)
		}
	}
	return strings.Join(out, " ")
}

func replaceAllowlistViolations(text string, words map[string]struct{}, replacement string, lowercase bool) string {
	tokens := tokenize(text)
	if len(tokens) == 0 {
		return replacement
	}
	var out []string
	for _, t := range tokens {
		key := t
		if lowercase {
			key = strings.ToLower(t)
		}
		if _, ok := words[key]; ok {
			out = append(out, t)
		} else {
			out = append(out, replacement)
		}
	}
	return strings.Join(out, " ")
}
