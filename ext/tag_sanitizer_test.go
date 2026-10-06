package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestTagPattern_NoTag_Pass(t *testing.T) {
	tag, err := NewTagPatternValidator("")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, rep, err := tag.Validate(ctx, "Hello world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Errorf("got Action=%v", rep.Action)
	}
}

func TestTagPattern_NoTag_PassPreservesMetadata(t *testing.T) {
	tag, err := NewTagPatternValidator("", WithCode("SAFE"), WithSeverity(guardy.SeverityLow))
	if err != nil {
		t.Fatal(err)
	}
	_, rep, err := tag.Validate(context.Background(), "Hello world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Fatalf("action = %v", rep.Action)
	}
	if rep.Code != "SAFE" {
		t.Fatalf("code = %q", rep.Code)
	}
	if rep.Severity != guardy.SeverityLow {
		t.Fatalf("severity = %q", rep.Severity)
	}
}

func TestTagPattern_SystemTag_Block(t *testing.T) {
	tag, err := NewTagPatternValidator("", WithCode("TAG_INJECTION"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, rep, err := tag.Validate(ctx, "Before <system> instructions")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Errorf("got Action=%v", rep.Action)
	}
	if rep.Validator != defaultTagPatternName || rep.Reason != "system tag pattern matched" {
		t.Errorf("got Validator=%s Reason=%v", rep.Validator, rep.Reason)
	}
	if rep.Code != "TAG_INJECTION" {
		t.Errorf("Code = %q, want TAG_INJECTION", rep.Code)
	}
	if rep.Retryable {
		t.Error("block must not be Retryable")
	}
}

func TestTagPattern_ClosingTag_Block(t *testing.T) {
	tag, err := NewTagPatternValidator("")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, rep, err := tag.Validate(ctx, "End </system> here")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Errorf("got Action=%v", rep.Action)
	}
}

func TestTagPattern_InvalidPattern(t *testing.T) {
	_, err := NewTagPatternValidator(`[invalid`)
	if err == nil {
		t.Error("expected error for invalid pattern")
	}
}

func TestMustTagPatternValidator_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustTagPatternValidator should panic on invalid pattern")
		}
	}()
	MustTagPatternValidator(`[invalid`)
}

func TestTagPattern_Name(t *testing.T) {
	tag := MustTagPatternValidator("")
	_, rep, err := tag.Validate(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Validator != defaultTagPatternName {
		t.Errorf("Validator = %q, want %s", rep.Validator, defaultTagPatternName)
	}
}

// TestTagPattern_SystemTagWithAttributes_Block prevents bypass via <system role="x"> etc.
func TestTagPattern_SystemTagWithAttributes_Block(t *testing.T) {
	tag, err := NewTagPatternValidator("")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, input := range []string{
		`<system role="assistant">`,
		`<system type="prompt">`,
		`<SYSTEM attr="x">`,
	} {
		_, rep, err := tag.Validate(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		if rep.Action != guardy.ActionBlock {
			t.Errorf("input %q: got Action=%v, want Block", input, rep.Action)
		}
	}
}
