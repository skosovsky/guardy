package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

// DummyState is a test struct for Lens mutation tests (value semantics).
type DummyState struct {
	Text string
}

// DummyStatePtr is a test struct for Lens mutation tests (pointer semantics).
type DummyStatePtr struct {
	Text string
}

// TestMap_LensMutation_ValueType verifies that Map correctly mutates T when T is a struct by value.
func TestMap_LensMutation_ValueType(t *testing.T) {
	regexV, err := NewRegexValidator(
		`(?i)bad`,
		WithAction(guardy.ActionRedact),
		WithCode("X"),
		WithRedactionReplacement("[REDACTED]"),
	)
	if err != nil {
		t.Fatal(err)
	}
	extract := func(d DummyState) string { return d.Text }
	inject := func(d DummyState, s string) DummyState {
		d.Text = s
		return d
	}
	mapped := guardy.Map(regexV, extract, inject)
	ctx := context.Background()
	input := DummyState{Text: "hello bad world"}
	out, rep, err := mapped.Validate(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionRedact {
		t.Fatalf("expected ActionRedact, got rep=%v", rep)
	}
	if out.Text != "hello [REDACTED] world" {
		t.Errorf("out.Text = %q, want hello [REDACTED] world", out.Text)
	}
}

// TestMap_LensMutation_PointerType verifies that Map correctly mutates T when T is a pointer.
func TestMap_LensMutation_PointerType(t *testing.T) {
	regexV, err := NewRegexValidator(
		`(?i)bad`,
		WithAction(guardy.ActionRedact),
		WithCode("X"),
		WithRedactionReplacement("[REDACTED]"),
	)
	if err != nil {
		t.Fatal(err)
	}
	extract := func(d *DummyStatePtr) string { return d.Text }
	inject := func(d *DummyStatePtr, s string) *DummyStatePtr {
		d.Text = s
		return d
	}
	mapped := guardy.Map(regexV, extract, inject)
	ctx := context.Background()
	input := &DummyStatePtr{Text: "hello bad world"}
	out, rep, err := mapped.Validate(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionRedact {
		t.Fatalf("expected ActionRedact, got rep=%v", rep)
	}
	if out.Text != "hello [REDACTED] world" {
		t.Errorf("out.Text = %q, want hello [REDACTED] world", out.Text)
	}
}
