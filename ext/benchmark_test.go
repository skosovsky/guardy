package ext

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
)

//nolint:gochecknoglobals // benchmark sink; prevents compiler eliding hot paths
var benchSinkString string
var frozenWordBoundaryREA16279e = regexp.MustCompile(`\b[\p{L}\p{N}_]+\b`)

func BenchmarkRegex_NoMatch(b *testing.B) {
	r, err := NewRegexValidator(`\b(inject|ignore)\b`, WithCode("INJECT"))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	text := "hello world"
	b.ResetTimer()
	for range b.N {
		_, _, _ = r.Validate(ctx, text)
	}
}

func BenchmarkRegex_MatchRedact(b *testing.B) {
	r, err := NewRegexValidator(
		`\d{3}-\d{3}-\d{4}`,
		WithAction(guardy.ActionRedact),
		WithCode("PII"),
		WithRedactionReplacement("[PHONE]"),
	)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	text := "Call me at 555-123-4567"
	b.ResetTimer()
	for range b.N {
		_, _, _ = r.Validate(ctx, text)
	}
}

func BenchmarkWordlist_Blocklist_NoMatch(b *testing.B) {
	w := NewWordlistValidator([]string{"spam", "bad"}, Blocklist, WithCode("SPAM"))
	ctx := context.Background()
	text := "hello world"
	b.ResetTimer()
	for range b.N {
		_, _, _ = w.Validate(ctx, text)
	}
}

func BenchmarkWordlist_Blocklist_Match(b *testing.B) {
	w := NewWordlistValidator([]string{"spam", "bad"}, Blocklist, WithCode("SPAM"))
	ctx := context.Background()
	text := "this is spam"
	b.ResetTimer()
	for range b.N {
		_, _, _ = w.Validate(ctx, text)
	}
}

func BenchmarkWordlist_Blocklist_RedactMatch(b *testing.B) {
	w := NewWordlistValidator(
		[]string{"spam", "bad"},
		Blocklist,
		WithAction(guardy.ActionRedact),
		WithCode("SPAM"),
		WithRedactionReplacement("[X]"),
	)
	ctx := context.Background()
	text := "this is spam"
	b.ResetTimer()
	for range b.N {
		_, _, _ = w.Validate(ctx, text)
	}
}

func BenchmarkWordlist_Allowlist_RedactMatch(b *testing.B) {
	w := NewWordlistValidator(
		[]string{"hello", "world"},
		Allowlist,
		WithAction(guardy.ActionRedact),
		WithCode("OFF_TOPIC"),
		WithRedactionReplacement("[X]"),
	)
	ctx := context.Background()
	text := "hello unknown world"
	b.ResetTimer()
	for range b.N {
		_, _, _ = w.Validate(ctx, text)
	}
}

func BenchmarkWordlist_Blocklist_Redact_BaselineComparison(b *testing.B) {
	words := make([]string, 0, 260)
	for i := range 256 {
		words = append(words, fmt.Sprintf("forbidden_%03d", i))
	}
	words = append(words, "spam", "bad", "secret", "leak")
	text := "this is forbidden_123 content with secret markers"
	ctx := context.Background()
	v2 := NewWordlistValidator(
		words,
		Blocklist,
		WithAction(guardy.ActionRedact),
		WithCode("SPAM"),
		WithRedactionReplacement("[X]"),
	)

	b.Run("baseline_a16279e_runtime_compile", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			benchSinkString = frozenWordlistRedactBlocklistA16279e(text, words, false)
		}
	})

	b.Run("v2_precompiled", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			out, _, _ := v2.Validate(ctx, text)
			benchSinkString = out
		}
	})
}

func BenchmarkLength_WithinRange(b *testing.B) {
	l := NewLengthValidator(1, 10000, WithCode("LENGTH"))
	ctx := context.Background()
	text := "hello"
	b.ResetTimer()
	for range b.N {
		_, _, _ = l.Validate(ctx, text)
	}
}

// frozenWordlistRedactBlocklistA16279e is a frozen pre-v2 baseline copied from
// commit a16279e (legacy runtime regex compilation in redact path).
func frozenWordlistRedactBlocklistA16279e(text string, words []string, lowercase bool) string {
	if len(words) == 0 {
		return text
	}
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		key := w
		if lowercase {
			key = strings.ToLower(key)
		}
		set[key] = struct{}{}
	}
	run := text
	if lowercase {
		run = strings.ToLower(text)
	}
	for _, tok := range frozenTokenizeA16279e(run) {
		if _, ok := set[tok]; ok {
			return frozenReplaceWordsInTextA16279e(text, set, "[X]", lowercase)
		}
	}
	return text
}

func frozenTokenizeA16279e(s string) []string {
	matches := frozenWordBoundaryREA16279e.FindAllString(s, -1)
	return matches
}

func frozenReplaceWordsInTextA16279e(
	text string,
	words map[string]struct{},
	replacement string,
	lowercase bool,
) string {
	if len(words) == 0 {
		return text
	}
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

func BenchmarkLength_TooLong(b *testing.B) {
	l := NewLengthValidator(0, 3, WithCode("LENGTH"))
	ctx := context.Background()
	text := "hello world"
	b.ResetTimer()
	for range b.N {
		_, _, _ = l.Validate(ctx, text)
	}
}
