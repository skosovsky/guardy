package ext

import (
	"context"
	"fmt"
	"testing"

	"github.com/skosovsky/guardy"
)

func ExampleLength_Validate_block() {
	l := NewLength(5, 10, guardy.ActionBlock, "LENGTH")
	ctx := context.Background()
	_, rep, _ := l.Validate(ctx, "hi")
	if rep != nil && rep.Action == guardy.ActionBlock {
		fmt.Println(rep.Reason)
	}
	// Output:
	// text too short
}

func TestLength_WithinRange_Pass(t *testing.T) {
	l := NewLength(1, 10, guardy.ActionBlock, "LENGTH")
	ctx := context.Background()
	_, rep, err := l.Validate(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Error("expected Pass")
	}
}

func TestLength_TooShort_Block(t *testing.T) {
	l := NewLength(5, 100, guardy.ActionBlock, "LENGTH")
	ctx := context.Background()
	_, rep, err := l.Validate(ctx, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock || rep.Reason != "text too short" {
		t.Errorf("got Action=%v Reason=%s", rep.Action, rep.Reason)
	}
}

func TestLength_TooLong_Block(t *testing.T) {
	l := NewLength(0, 3, guardy.ActionBlock, "LENGTH")
	ctx := context.Background()
	_, rep, err := l.Validate(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock || rep.Reason != "text too long" {
		t.Errorf("got Action=%v Reason=%s", rep.Action, rep.Reason)
	}
}

func TestLength_ZeroMinMax_Pass(t *testing.T) {
	l := NewLength(0, 0, guardy.ActionBlock, "X")
	ctx := context.Background()
	_, rep, err := l.Validate(ctx, "anything")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Error("Min=0 Max=0 should not block")
	}
}

func TestLength_UnicodeRunes(t *testing.T) {
	l := NewLength(2, 2, guardy.ActionBlock, "X")
	ctx := context.Background()
	_, rep, err := l.Validate(ctx, "аб")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Errorf("2 runes should pass, got Action=%v", rep.Action)
	}
	_, rep2, _ := l.Validate(ctx, "а")
	if rep2.Action != guardy.ActionBlock {
		t.Error("1 rune should block")
	}
}

func TestLength_WithLengthName(t *testing.T) {
	l := NewLength(0, 10, guardy.ActionBlock, "X", WithLengthName("my-length"))
	if l.Name() != "my-length" {
		t.Errorf("Name() = %q, want my-length", l.Name())
	}
}
