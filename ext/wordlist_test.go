package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestWordlist_Blocklist_NoMatch_Pass(t *testing.T) {
	w := NewWordlist([]string{"spam", "bad"}, Blocklist, guardy.Block, "SPAM")
	ctx := context.Background()
	res, err := w.Validate(ctx, guardy.Input{Text: "hello world"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Errorf("expected Pass")
	}
}

func TestWordlist_Blocklist_Match_Block(t *testing.T) {
	w := NewWordlist([]string{"spam", "bad"}, Blocklist, guardy.Block, "SPAM")
	ctx := context.Background()
	res, err := w.Validate(ctx, guardy.Input{Text: "this is spam"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Action != guardy.Block || res.Code != "SPAM" {
		t.Errorf("got Passed=%v Action=%s Code=%s", res.Passed, res.Action, res.Code)
	}
}

func TestWordlist_Blocklist_Lowercase(t *testing.T) {
	w := NewWordlist([]string{"Spam"}, Blocklist, guardy.Block, "X", WithWordlistLowercase(true))
	ctx := context.Background()
	res, err := w.Validate(ctx, guardy.Input{Text: "SPAM here"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed {
		t.Error("expected block (case-insensitive match)")
	}
}

func TestWordlist_Allowlist_AllAllowed_Pass(t *testing.T) {
	w := NewWordlist([]string{"hello", "world"}, Allowlist, guardy.Block, "OFF_TOPIC")
	ctx := context.Background()
	res, err := w.Validate(ctx, guardy.Input{Text: "hello world"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Errorf("expected Pass")
	}
}

func TestWordlist_Allowlist_NotAllowed_Block(t *testing.T) {
	w := NewWordlist([]string{"hello", "world"}, Allowlist, guardy.Block, "OFF_TOPIC")
	ctx := context.Background()
	res, err := w.Validate(ctx, guardy.Input{Text: "hello foo world"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Code != "OFF_TOPIC" {
		t.Errorf("got Passed=%v Code=%s", res.Passed, res.Code)
	}
}

func TestWordlist_Allowlist_EmptyText_Block(t *testing.T) {
	w := NewWordlist([]string{"a"}, Allowlist, guardy.Block, "X")
	ctx := context.Background()
	res, err := w.Validate(ctx, guardy.Input{Text: ""})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed {
		t.Error("empty text with allowlist should block")
	}
}

func TestWordlist_WithWordlistName(t *testing.T) {
	w := NewWordlist([]string{"a"}, Blocklist, guardy.Block, "X", WithWordlistName("my-wordlist"))
	if w.Name() != "my-wordlist" {
		t.Errorf("Name() = %q, want my-wordlist", w.Name())
	}
}

func TestWordlist_ContextCancelled(t *testing.T) {
	w := NewWordlist([]string{"spam"}, Blocklist, guardy.Block, "X")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := w.Validate(ctx, guardy.Input{Text: "hello"})
	if err == nil {
		t.Error("expected error when context cancelled")
	}
}
