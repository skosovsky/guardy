package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func BenchmarkRegex_NoMatch(b *testing.B) {
	r, err := NewRegex(`\b(inject|ignore)\b`, guardy.ActionBlock, "INJECT")
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	text := "hello world"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Validate(ctx, text)
	}
}

func BenchmarkRegex_MatchRedact(b *testing.B) {
	r, err := NewRegex(`\d{3}-\d{3}-\d{4}`, guardy.ActionRedact, "PII", WithRegexPlaceholder("[PHONE]"))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	text := "Call me at 555-123-4567"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Validate(ctx, text)
	}
}

func BenchmarkWordlist_Blocklist_NoMatch(b *testing.B) {
	w := NewWordlist([]string{"spam", "bad"}, Blocklist, guardy.ActionBlock, "SPAM")
	ctx := context.Background()
	text := "hello world"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = w.Validate(ctx, text)
	}
}

func BenchmarkWordlist_Blocklist_Match(b *testing.B) {
	w := NewWordlist([]string{"spam", "bad"}, Blocklist, guardy.ActionBlock, "SPAM")
	ctx := context.Background()
	text := "this is spam"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = w.Validate(ctx, text)
	}
}

func BenchmarkLength_WithinRange(b *testing.B) {
	l := NewLength(1, 10000, guardy.ActionBlock, "LENGTH")
	ctx := context.Background()
	text := "hello"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = l.Validate(ctx, text)
	}
}

func BenchmarkLength_TooLong(b *testing.B) {
	l := NewLength(0, 3, guardy.ActionBlock, "LENGTH")
	ctx := context.Background()
	text := "hello world"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = l.Validate(ctx, text)
	}
}
