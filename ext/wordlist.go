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

const (
	// Blocklist blocks when any word in the list is found.
	Blocklist WordlistMode = iota
	// Allowlist blocks when the text contains words not in the list (or when no allowed words).
	Allowlist
)

// Wordlist is a validator that checks text against a set of words.
type Wordlist struct {
	words     map[string]struct{}
	mode      WordlistMode
	action    guardy.Action
	code      string
	name      string
	lowercase bool
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

// NewWordlist creates a blocklist or allowlist validator.
// For Blocklist: returns action/code when any word in words is found in text.
// For Allowlist: returns action/code when text contains tokens not in words (or is empty of allowed tokens).
func NewWordlist(words []string, mode WordlistMode, action guardy.Action, code string, opts ...WordlistOption) *Wordlist {
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		set[w] = struct{}{}
	}
	wl := &Wordlist{
		words:     set,
		mode:      mode,
		action:    action,
		code:      code,
		name:      "wordlist",
		lowercase: false,
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

// Validate checks input.Text against the word list.
func (w *Wordlist) Validate(ctx context.Context, input guardy.Input) (guardy.Result, error) {
	if ctx.Err() != nil {
		return guardy.Result{}, ctx.Err()
	}
	text := input.Text
	if w.lowercase {
		text = strings.ToLower(text)
	}
	tokens := tokenize(text)
	switch w.mode {
	case Blocklist:
		for _, t := range tokens {
			if _, ok := w.words[t]; ok {
				return guardy.Result{
					Passed: false,
					Action: w.action,
					Code:   w.code,
					Reason: "blocklisted word found",
				}, nil
			}
		}
		return guardy.Result{Passed: true, Action: guardy.Pass}, nil
	case Allowlist:
		if len(tokens) == 0 {
			return guardy.Result{
				Passed: false,
				Action: w.action,
				Code:   w.code,
				Reason: "no tokens",
			}, nil
		}
		for _, t := range tokens {
			if _, ok := w.words[t]; !ok {
				return guardy.Result{
					Passed: false,
					Action: w.action,
					Code:   w.code,
					Reason: "word not in allowlist",
				}, nil
			}
		}
		return guardy.Result{Passed: true, Action: guardy.Pass}, nil
	default:
		return guardy.Result{Passed: true, Action: guardy.Pass}, nil
	}
}

// Name returns the validator name.
func (w *Wordlist) Name() string {
	return w.name
}

func tokenize(s string) []string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return nil
	}
	return f
}
