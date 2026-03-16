package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestTagSanitizer_NoTag_Pass(t *testing.T) {
	tag, err := NewTagSanitizer("")
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

func TestTagSanitizer_SystemTag_Block(t *testing.T) {
	tag, err := NewTagSanitizer("")
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
	if rep.Validator != "tag_sanitizer" || rep.Reason != "system tag injection attempt" {
		t.Errorf("got Validator=%s Reason=%v", rep.Validator, rep.Reason)
	}
}

func TestTagSanitizer_ClosingTag_Block(t *testing.T) {
	tag, err := NewTagSanitizer("")
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

func TestTagSanitizer_InvalidPattern(t *testing.T) {
	_, err := NewTagSanitizer(`[invalid`)
	if err == nil {
		t.Error("expected error for invalid pattern")
	}
}

func TestMustTagSanitizer_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustTagSanitizer should panic on invalid pattern")
		}
	}()
	MustTagSanitizer(`[invalid`)
}

func TestTagSanitizer_Name(t *testing.T) {
	tag := MustTagSanitizer("")
	if tag.Name() != "tag_sanitizer" {
		t.Errorf("Name() = %q, want tag_sanitizer", tag.Name())
	}
}

// TestTagSanitizer_SystemTagWithAttributes_Block prevents bypass via <system role="x"> etc.
func TestTagSanitizer_SystemTagWithAttributes_Block(t *testing.T) {
	tag, err := NewTagSanitizer("")
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
