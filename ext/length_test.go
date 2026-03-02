package ext

import (
	"context"
	"fmt"
	"testing"

	"github.com/skosovsky/guardy"
)

func ExampleLength_Validate_block() {
	l := NewLength(5, 10, guardy.Block, "LENGTH")
	ctx := context.Background()
	res, _ := l.Validate(ctx, guardy.Input{Text: "hi"})
	if !res.Passed {
		fmt.Println(res.Reason)
	}
	// Output:
	// text too short
}

func TestLength_WithinRange_Pass(t *testing.T) {
	l := NewLength(1, 10, guardy.Block, "LENGTH")
	ctx := context.Background()
	res, err := l.Validate(ctx, guardy.Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Error("expected Pass")
	}
}

func TestLength_TooShort_Block(t *testing.T) {
	l := NewLength(5, 100, guardy.Block, "LENGTH")
	ctx := context.Background()
	res, err := l.Validate(ctx, guardy.Input{Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Reason != "text too short" {
		t.Errorf("got Passed=%v Reason=%s", res.Passed, res.Reason)
	}
}

func TestLength_TooLong_Block(t *testing.T) {
	l := NewLength(0, 3, guardy.Block, "LENGTH")
	ctx := context.Background()
	res, err := l.Validate(ctx, guardy.Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Reason != "text too long" {
		t.Errorf("got Passed=%v Reason=%s", res.Passed, res.Reason)
	}
}

func TestLength_ZeroMinMax_Pass(t *testing.T) {
	l := NewLength(0, 0, guardy.Block, "X")
	ctx := context.Background()
	res, err := l.Validate(ctx, guardy.Input{Text: "anything"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Error("Min=0 Max=0 should not block")
	}
}

func TestLength_UnicodeRunes(t *testing.T) {
	l := NewLength(2, 2, guardy.Block, "X")
	ctx := context.Background()
	res, err := l.Validate(ctx, guardy.Input{Text: "аб"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Errorf("2 runes should pass, got Passed=%v", res.Passed)
	}
	res2, _ := l.Validate(ctx, guardy.Input{Text: "а"})
	if res2.Passed {
		t.Error("1 rune should block")
	}
}

func TestLength_WithLengthName(t *testing.T) {
	l := NewLength(0, 10, guardy.Block, "X", WithLengthName("my-length"))
	if l.Name() != "my-length" {
		t.Errorf("Name() = %q, want my-length", l.Name())
	}
}
