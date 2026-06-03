package guardy_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

type toolArgsDTO struct {
	ToolArgs json.RawMessage `json:"tool_args"`
}

func TestMapJSONRawMessage_Redact_ValidJSON_PII(t *testing.T) {
	t.Parallel()
	piiV := ext.NewPIIValidator(ext.WithRedactionReplacement("[REDACTED]"))
	mapped := guardy.MapJSONRawMessage(
		piiV,
		func(d *toolArgsDTO) json.RawMessage { return d.ToolArgs },
		func(d *toolArgsDTO, raw json.RawMessage) *toolArgsDTO {
			d.ToolArgs = raw
			return d
		},
	)
	in := toolArgsDTO{ToolArgs: json.RawMessage(`{"email":"user@example.com"}`)}
	out, rep, err := mapped.Validate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionRedact {
		t.Fatalf("rep = %+v", rep)
	}
	if !json.Valid(out.ToolArgs) {
		t.Fatalf("invalid JSON after PII redact: %s", out.ToolArgs)
	}
	if string(out.ToolArgs) == string(in.ToolArgs) {
		t.Fatal("expected redacted tool args")
	}
}

func TestMapJSONRawMessage_Redact_BreaksJSON_Regex(t *testing.T) {
	t.Parallel()
	// Regex redact that strips double-quotes — realistic way ext validators can corrupt JSON.
	regexV, err := ext.NewRegexValidator(
		`"`,
		ext.WithAction(guardy.ActionRedact),
		ext.WithCode("QUOTE_STRIP"),
		ext.WithRedactionReplacement(""),
	)
	if err != nil {
		t.Fatal(err)
	}
	mapped := guardy.MapJSONRawMessage(
		regexV,
		func(d *toolArgsDTO) json.RawMessage { return d.ToolArgs },
		func(d *toolArgsDTO, raw json.RawMessage) *toolArgsDTO {
			d.ToolArgs = raw
			return d
		},
	)
	original := json.RawMessage(`{"email":"user@example.com"}`)
	in := toolArgsDTO{ToolArgs: original}
	out, rep, err := mapped.Validate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionRetry {
		t.Fatalf("rep = %+v, want ActionRetry", rep)
	}
	if rep.Code != guardy.CodeJSONRedactCorrupted {
		t.Fatalf("Code = %q, want %q", rep.Code, guardy.CodeJSONRedactCorrupted)
	}
	if !rep.Retryable {
		t.Fatal("expected Retryable")
	}
	if string(out.ToolArgs) != string(original) {
		t.Fatalf("input must not be mutated on invalid JSON redact, got %s", out.ToolArgs)
	}
}
