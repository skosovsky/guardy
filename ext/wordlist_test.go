package ext

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
)

func ExampleMustWordlistValidator() {
	w := MustWordlistValidator([]string{"spam", "bad"}, Blocklist, WithCode("SPAM"))
	ctx := context.Background()
	_, rep, _ := w.Validate(ctx, "this is spam")
	if rep.Action == guardy.ActionBlock {
		fmt.Println(rep.Reason)
	}
	// Output:
	// blocklisted word found
}

func TestWordlist_Blocklist_NoMatch_Pass(t *testing.T) {
	w := MustWordlistValidator([]string{"spam", "bad"}, Blocklist, WithCode("SPAM"))
	ctx := context.Background()
	_, rep, err := w.Validate(ctx, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Errorf("expected Pass, got Action=%v", rep.Action)
	}
}

func TestWordlist_Blocklist_NoMatch_PassPreservesMetadata(t *testing.T) {
	w := MustWordlistValidator(
		[]string{"spam", "bad"},
		Blocklist,
		WithCode("SPAM"),
		WithSeverity(guardy.SeverityMedium),
	)
	_, rep, err := w.Validate(context.Background(), "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Fatalf("action = %v", rep.Action)
	}
	if rep.Code != "SPAM" {
		t.Fatalf("code = %q", rep.Code)
	}
	if rep.Severity != guardy.SeverityMedium {
		t.Fatalf("severity = %q", rep.Severity)
	}
}

func TestWordlist_Blocklist_Match_Block(t *testing.T) {
	w := MustWordlistValidator([]string{"spam", "bad"}, Blocklist, WithCode("SPAM"))
	ctx := context.Background()
	_, rep, err := w.Validate(ctx, "this is spam")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock || rep.Validator != "wordlist_validator" {
		t.Errorf("got Action=%v Validator=%s", rep.Action, rep.Validator)
	}
	if rep.Code != "SPAM" {
		t.Errorf("Code = %q, want SPAM", rep.Code)
	}
	if rep.Retryable {
		t.Error("block must not be Retryable")
	}
}

func TestWordlist_Blocklist_Lowercase(t *testing.T) {
	w := MustWordlistValidator([]string{"Spam"}, Blocklist, WithCode("X"), WithLowercase(true))
	ctx := context.Background()
	_, rep, err := w.Validate(ctx, "SPAM here")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Error("expected block (case-insensitive match)")
	}
}

func TestWordlist_Allowlist_AllAllowed_Pass(t *testing.T) {
	w := MustWordlistValidator([]string{"hello", "world"}, Allowlist, WithCode("OFF_TOPIC"))
	ctx := context.Background()
	_, rep, err := w.Validate(ctx, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Errorf("expected Pass, got Action=%v", rep.Action)
	}
}

func TestWordlist_Allowlist_NotAllowed_Block(t *testing.T) {
	w := MustWordlistValidator([]string{"hello", "world"}, Allowlist, WithCode("OFF_TOPIC"))
	ctx := context.Background()
	_, rep, err := w.Validate(ctx, "hello foo world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Errorf("got Action=%v", rep.Action)
	}
}

func TestWordlist_Allowlist_EmptyText_Block(t *testing.T) {
	w := MustWordlistValidator([]string{"a"}, Allowlist, WithCode("X"))
	ctx := context.Background()
	_, rep, err := w.Validate(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Error("empty text with allowlist should block")
	}
}

func TestWordlist_WithName(t *testing.T) {
	w := MustWordlistValidator([]string{"a"}, Blocklist, WithCode("X"), WithName("my-wordlist"))
	_, rep, err := w.Validate(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Validator != "my-wordlist" {
		t.Errorf("Validator = %q, want my-wordlist", rep.Validator)
	}
}

func TestWordlist_WithRedactionReplacement_ReturnsRedactAndCleanText(t *testing.T) {
	w := MustWordlistValidator(
		[]string{"spam", "bad"},
		Blocklist,
		WithAction(guardy.ActionRedact),
		WithCode("X"),
		WithRedactionReplacement("***"),
	)
	ctx := context.Background()
	_, rep, err := w.Validate(ctx, "this is spam")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Errorf("got Action=%v, want redact", rep.Action)
	}
	if rep.MutatedText == "" {
		t.Error("MutatedText should be set")
	}
	if rep.MutatedText != "this is ***" {
		t.Errorf("MutatedText = %q, want this is ***", rep.MutatedText)
	}
}

// TestWordlist_PunctuationBypass_Block prevents bypass via "bad.word" or "bad!" when "bad" is blocklisted.
func TestWordlist_PunctuationBypass_Block(t *testing.T) {
	w := MustWordlistValidator([]string{"bad"}, Blocklist, WithCode("X"))
	ctx := context.Background()
	for _, input := range []string{"bad.word", "bad!", "x.bad.y", "bad, comma", "x bad y"} {
		_, rep, err := w.Validate(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Action != guardy.ActionBlock {
			t.Errorf("input %q: got Pass, want Block (punctuation bypass)", input)
		}
	}
}

// TestWordlist_RedactPreservesFormatting ensures whitespace/newlines are preserved on redact.
func TestWordlist_RedactPreservesFormatting(t *testing.T) {
	w := MustWordlistValidator(
		[]string{"bad"},
		Blocklist,
		WithAction(guardy.ActionRedact),
		WithCode("X"),
		WithRedactionReplacement("[X]"),
	)
	ctx := context.Background()
	_, rep, err := w.Validate(ctx, "a  bad\tb\nc")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Fatal("expected redact")
	}
	want := "a  [X]\tb\nc"
	if rep.MutatedText != want {
		t.Errorf("MutatedText = %q, want %q", rep.MutatedText, want)
	}
}

func TestWordlist_WithTokenVault(t *testing.T) {
	vault := NewInMemoryTokenVault()
	w := MustWordlistValidator(
		[]string{"secret"},
		Blocklist,
		WithAction(guardy.ActionRedact),
		WithTokenVault(vault),
	)
	_, rep, err := w.Validate(context.Background(), "my secret text")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Fatalf("action = %v", rep.Action)
	}
	if rep.MutatedText == "my secret text" {
		t.Fatalf("expected redaction, got %q", rep.MutatedText)
	}
	restored := UnredactText(rep.MutatedText, vault)
	if restored != "my secret text" {
		t.Fatalf("restored = %q", restored)
	}
}

func TestWordlist_WithTypedNilTokenVault_FallbackReplacement(t *testing.T) {
	var vault *InMemoryTokenVault
	w := MustWordlistValidator(
		[]string{"secret"},
		Blocklist,
		WithAction(guardy.ActionRedact),
		WithTokenVault(vault),
		WithRedactionReplacement("[X]"),
	)
	_, rep, err := w.Validate(context.Background(), "my secret text")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Fatalf("action = %v", rep.Action)
	}
	if rep.MutatedText != "my [X] text" {
		t.Fatalf("mutated = %q, want %q", rep.MutatedText, "my [X] text")
	}
}

func TestNewWordlistValidatorConfigurationErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		words   []string
		mode    WordlistMode
		options []Option
		field   string
	}{
		{name: "empty", words: []string{""}, field: "words[0]"},
		{name: "phrase", words: []string{"secret value"}, field: "words[0]"},
		{name: "mode", words: []string{"secret"}, mode: WordlistMode(-1), field: "mode"},
		{name: "option", words: []string{"secret"}, options: []Option{nil}, field: "options[0]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			words, mode := tc.words, tc.mode
			// Act.
			v, err := NewWordlistValidator(words, mode, tc.options...)
			// Assert.
			var cfg *guardy.ConfigurationError
			if v != nil || !errors.Is(err, guardy.ErrConfiguration) || !errors.As(err, &cfg) || cfg.Field != tc.field ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("%v %v", v, err)
			}
		})
	}
}

func ExampleNewWordlistValidator() {
	v, err := NewWordlistValidator([]string{"spam"}, Blocklist)
	if err != nil {
		panic(err)
	}
	_, report, err := v.Validate(context.Background(), "spam")
	if err != nil {
		panic(err)
	}
	fmt.Println(report.Action)
	// Output:
	// block
}
