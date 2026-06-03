package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestTagSanitizer_NoTag_Pass(t *testing.T) {
	tag, err := NewTagSanitizerValidator("")
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

func TestTagSanitizer_NoTag_PassPreservesMetadata(t *testing.T) {
	tag, err := NewTagSanitizerValidator("", WithCode("SAFE"), WithSeverity(guardy.SeverityLow))
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

func TestTagSanitizer_SystemTag_Block(t *testing.T) {
	tag, err := NewTagSanitizerValidator("", WithCode("TAG_INJECTION"))
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
	if rep.Validator != defaultTagSanitizerName || rep.Reason != "system tag injection attempt" {
		t.Errorf("got Validator=%s Reason=%v", rep.Validator, rep.Reason)
	}
	if rep.Code != "TAG_INJECTION" {
		t.Errorf("Code = %q, want TAG_INJECTION", rep.Code)
	}
	if rep.Retryable {
		t.Error("block must not be Retryable")
	}
}

func TestTagSanitizer_ClosingTag_Block(t *testing.T) {
	tag, err := NewTagSanitizerValidator("")
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
	_, err := NewTagSanitizerValidator(`[invalid`)
	if err == nil {
		t.Error("expected error for invalid pattern")
	}
}

func TestMustTagSanitizerValidator_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustTagSanitizerValidator should panic on invalid pattern")
		}
	}()
	MustTagSanitizerValidator(`[invalid`)
}

func TestTagSanitizer_Name(t *testing.T) {
	tag := MustTagSanitizerValidator("")
	_, rep, err := tag.Validate(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Validator != defaultTagSanitizerName {
		t.Errorf("Validator = %q, want %s", rep.Validator, defaultTagSanitizerName)
	}
}

// TestTagSanitizer_SystemTagWithAttributes_Block prevents bypass via <system role="x"> etc.
func TestTagSanitizer_SystemTagWithAttributes_Block(t *testing.T) {
	tag, err := NewTagSanitizerValidator("")
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
