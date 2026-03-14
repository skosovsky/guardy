package ext

import (
	"context"
	"fmt"
	"testing"

	"github.com/skosovsky/guardy"
)

func ExampleWordlist_Validate_block() {
	w := NewWordlist([]string{"spam", "bad"}, Blocklist, guardy.ActionBlock, "SPAM")
	ctx := context.Background()
	rep, _ := w.Validate(ctx, "this is spam")
	if rep.Action == guardy.ActionBlock {
		fmt.Println(rep.Reason)
	}
	// Output:
	// blocklisted word found
}

func TestWordlist_Blocklist_NoMatch_Pass(t *testing.T) {
	w := NewWordlist([]string{"spam", "bad"}, Blocklist, guardy.ActionBlock, "SPAM")
	ctx := context.Background()
	rep, err := w.Validate(ctx, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Errorf("expected Pass, got Action=%s", rep.Action)
	}
}

func TestWordlist_Blocklist_Match_Block(t *testing.T) {
	w := NewWordlist([]string{"spam", "bad"}, Blocklist, guardy.ActionBlock, "SPAM")
	ctx := context.Background()
	rep, err := w.Validate(ctx, "this is spam")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock || rep.Validator != "wordlist" {
		t.Errorf("got Action=%s Validator=%s", rep.Action, rep.Validator)
	}
}

func TestWordlist_Blocklist_Lowercase(t *testing.T) {
	w := NewWordlist([]string{"Spam"}, Blocklist, guardy.ActionBlock, "X", WithWordlistLowercase(true))
	ctx := context.Background()
	rep, err := w.Validate(ctx, "SPAM here")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Error("expected block (case-insensitive match)")
	}
}

func TestWordlist_Allowlist_AllAllowed_Pass(t *testing.T) {
	w := NewWordlist([]string{"hello", "world"}, Allowlist, guardy.ActionBlock, "OFF_TOPIC")
	ctx := context.Background()
	rep, err := w.Validate(ctx, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Errorf("expected Pass, got Action=%s", rep.Action)
	}
}

func TestWordlist_Allowlist_NotAllowed_Block(t *testing.T) {
	w := NewWordlist([]string{"hello", "world"}, Allowlist, guardy.ActionBlock, "OFF_TOPIC")
	ctx := context.Background()
	rep, err := w.Validate(ctx, "hello foo world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Errorf("got Action=%s", rep.Action)
	}
}

func TestWordlist_Allowlist_EmptyText_Block(t *testing.T) {
	w := NewWordlist([]string{"a"}, Allowlist, guardy.ActionBlock, "X")
	ctx := context.Background()
	rep, err := w.Validate(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Error("empty text with allowlist should block")
	}
}

func TestWordlist_WithWordlistName(t *testing.T) {
	w := NewWordlist([]string{"a"}, Blocklist, guardy.ActionBlock, "X", WithWordlistName("my-wordlist"))
	if w.Name() != "my-wordlist" {
		t.Errorf("Name() = %q, want my-wordlist", w.Name())
	}
}

func TestWordlist_WithWordlistRedaction_ReturnsRedactAndCleanText(t *testing.T) {
	w := NewWordlist([]string{"spam", "bad"}, Blocklist, guardy.ActionRedact, "X", WithWordlistRedaction("***"))
	ctx := context.Background()
	rep, err := w.Validate(ctx, "this is spam")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Errorf("got Action=%s, want redact", rep.Action)
	}
	if rep.MutatedText == "" {
		t.Error("MutatedText should be set")
	}
	if rep.MutatedText != "this is ***" {
		t.Errorf("MutatedText = %q, want this is ***", rep.MutatedText)
	}
}
