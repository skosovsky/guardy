package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func BenchmarkRegex_NoMatch(b *testing.B) {
	r, err := NewRegex(`\b(inject|ignore)\b`, guardy.Block, "INJECT")
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	in := guardy.Input{Text: "hello world"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Validate(ctx, in)
	}
}

func BenchmarkRegex_MatchRedact(b *testing.B) {
	r, err := NewRegex(`\d{3}-\d{3}-\d{4}`, guardy.Redact, "PII", WithRegexPlaceholder("[PHONE]"))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	in := guardy.Input{Text: "Call me at 555-123-4567"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Validate(ctx, in)
	}
}

func BenchmarkWordlist_Blocklist_NoMatch(b *testing.B) {
	w := NewWordlist([]string{"spam", "bad"}, Blocklist, guardy.Block, "SPAM")
	ctx := context.Background()
	in := guardy.Input{Text: "hello world"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = w.Validate(ctx, in)
	}
}

func BenchmarkWordlist_Blocklist_Match(b *testing.B) {
	w := NewWordlist([]string{"spam", "bad"}, Blocklist, guardy.Block, "SPAM")
	ctx := context.Background()
	in := guardy.Input{Text: "this is spam"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = w.Validate(ctx, in)
	}
}

func BenchmarkLength_WithinRange(b *testing.B) {
	l := NewLength(1, 10000, guardy.Block, "LENGTH")
	ctx := context.Background()
	in := guardy.Input{Text: "hello"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = l.Validate(ctx, in)
	}
}

func BenchmarkLength_TooLong(b *testing.B) {
	l := NewLength(0, 3, guardy.Block, "LENGTH")
	ctx := context.Background()
	in := guardy.Input{Text: "hello world"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = l.Validate(ctx, in)
	}
}

func BenchmarkJSON_Valid(b *testing.B) {
	j := NewJSON(nil, guardy.Block, "JSON")
	ctx := context.Background()
	in := guardy.Input{Text: `{"a":1,"b":"x"}`}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = j.Validate(ctx, in)
	}
}

func BenchmarkJSON_RequiredKeys(b *testing.B) {
	j := NewJSON([]string{"id", "name"}, guardy.Block, "JSON")
	ctx := context.Background()
	in := guardy.Input{Text: `{"id": 1, "name": "x"}`}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = j.Validate(ctx, in)
	}
}
