package ext

import (
	"context"
	"fmt"
	"testing"

	"github.com/skosovsky/guardy"
)

func ExampleNewWordlistValidator() {
	w := NewWordlistValidator([]string{"spam", "bad"}, Blocklist, WithCode("SPAM"))
	ctx := context.Background()
	_, rep, _ := w.Validate(ctx, "this is spam")
	if rep.Action == guardy.ActionBlock {
		fmt.Println(rep.Reason)
	}
	// Output:
	// blocklisted word found
}

func TestWordlist_Blocklist_NoMatch_Pass(t *testing.T) {
	w := NewWordlistValidator([]string{"spam", "bad"}, Blocklist, WithCode("SPAM"))
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
	w := NewWordlistValidator(
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
	w := NewWordlistValidator([]string{"spam", "bad"}, Blocklist, WithCode("SPAM"))
	ctx := context.Background()
	_, rep, err := w.Validate(ctx, "this is spam")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock || rep.Validator != "wordlist_validator" {
		t.Errorf("got Action=%v Validator=%s", rep.Action, rep.Validator)
	}
}

func TestWordlist_Blocklist_Lowercase(t *testing.T) {
	w := NewWordlistValidator([]string{"Spam"}, Blocklist, WithCode("X"), WithLowercase(true))
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
	w := NewWordlistValidator([]string{"hello", "world"}, Allowlist, WithCode("OFF_TOPIC"))
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
	w := NewWordlistValidator([]string{"hello", "world"}, Allowlist, WithCode("OFF_TOPIC"))
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
	w := NewWordlistValidator([]string{"a"}, Allowlist, WithCode("X"))
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
	w := NewWordlistValidator([]string{"a"}, Blocklist, WithCode("X"), WithName("my-wordlist"))
	_, rep, err := w.Validate(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Validator != "my-wordlist" {
		t.Errorf("Validator = %q, want my-wordlist", rep.Validator)
	}
}

func TestWordlist_WithRedactionReplacement_ReturnsRedactAndCleanText(t *testing.T) {
	w := NewWordlistValidator(
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
	w := NewWordlistValidator([]string{"bad"}, Blocklist, WithCode("X"))
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
	w := NewWordlistValidator(
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
	w := NewWordlistValidator(
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
	w := NewWordlistValidator(
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
