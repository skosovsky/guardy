package ext

import (
	"context"
	"fmt"
	"testing"

	"github.com/skosovsky/guardy"
)

func ExampleNewLengthValidator() {
	l := NewLengthValidator(5, 10, WithCode("LENGTH"))
	ctx := context.Background()
	_, rep, _ := l.Validate(ctx, "hi")
	if rep != nil && rep.Action == guardy.ActionBlock {
		fmt.Println(rep.Reason)
	}
	// Output:
	// text too short
}

func TestLength_WithinRange_Pass(t *testing.T) {
	l := NewLengthValidator(1, 10, WithCode("LENGTH"))
	ctx := context.Background()
	_, rep, err := l.Validate(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Error("expected Pass")
	}
}

func TestLength_WithinRange_PassPreservesMetadata(t *testing.T) {
	l := NewLengthValidator(1, 10, WithCode("LENGTH_OK"), WithSeverity(guardy.SeverityLow))
	_, rep, err := l.Validate(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Fatalf("action = %v", rep.Action)
	}
	if rep.Code != "LENGTH_OK" {
		t.Fatalf("code = %q", rep.Code)
	}
	if rep.Severity != guardy.SeverityLow {
		t.Fatalf("severity = %q", rep.Severity)
	}
}

func TestLength_TooShort_Block(t *testing.T) {
	l := NewLengthValidator(5, 100, WithCode("LENGTH"))
	ctx := context.Background()
	_, rep, err := l.Validate(ctx, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock || rep.Reason != "text too short" {
		t.Errorf("got Action=%v Reason=%s", rep.Action, rep.Reason)
	}
	if rep.Code != "LENGTH" {
		t.Errorf("Code = %q, want LENGTH", rep.Code)
	}
	if rep.Retryable {
		t.Error("block should not be Retryable")
	}
}

func TestLength_TooLong_Block(t *testing.T) {
	l := NewLengthValidator(0, 3, WithCode("LENGTH"))
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
	l := NewLengthValidator(0, 0, WithCode("X"))
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
	l := NewLengthValidator(2, 2, WithCode("X"))
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

func TestLength_WithName(t *testing.T) {
	l := NewLengthValidator(0, 10, WithCode("X"), WithName("my-length"))
	_, rep, err := l.Validate(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Validator != "my-length" {
		t.Errorf("Validator = %q, want my-length", rep.Validator)
	}
}
