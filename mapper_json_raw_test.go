package guardy

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

type toolArgsDTO struct {
	ToolArgs json.RawMessage `json:"tool_args"`
}

func TestMapJSONRawMessage_EmptyBypass(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	inner := ValidatorFunc[string](func(context.Context, string) (string, *Report, error) {
		calls.Add(1)
		t.Fatal("inner validator must not run for empty raw message")
		return "", nil, nil
	})
	mapped := MapJSONRawMessage(
		inner,
		func(d *toolArgsDTO) json.RawMessage { return d.ToolArgs },
		func(d *toolArgsDTO, raw json.RawMessage) *toolArgsDTO {
			d.ToolArgs = raw
			return d
		},
	)

	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{"nil", nil},
		{"empty", json.RawMessage{}},
		{"null_literal", json.RawMessage("null")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := toolArgsDTO{ToolArgs: tt.raw}
			out, rep, err := mapped.Validate(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if rep == nil || rep.Action != ActionPass {
				t.Fatalf("rep = %+v", rep)
			}
			if calls.Load() != 0 {
				t.Fatal("inner validator was invoked")
			}
			if string(out.ToolArgs) != string(tt.raw) {
				t.Fatalf("ToolArgs = %q, want %q", out.ToolArgs, tt.raw)
			}
		})
	}
}

func TestMapJSONRawMessage_Redact_ValidJSON(t *testing.T) {
	t.Parallel()
	inner := ValidatorFunc[string](func(_ context.Context, _ string) (string, *Report, error) {
		out := `{"email":"[REDACTED]"}`
		return out, &Report{
			Action:      ActionRedact,
			Validator:   "test",
			Code:        "PII",
			MutatedText: out,
		}, nil
	})
	mapped := MapJSONRawMessage(
		inner,
		func(d *toolArgsDTO) json.RawMessage { return d.ToolArgs },
		func(d *toolArgsDTO, raw json.RawMessage) *toolArgsDTO {
			d.ToolArgs = raw
			return d
		},
	)
	in := toolArgsDTO{ToolArgs: json.RawMessage(`{"email":"a@b.com"}`)}
	out, rep, err := mapped.Validate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != ActionRedact {
		t.Fatalf("rep = %+v", rep)
	}
	if !json.Valid(out.ToolArgs) {
		t.Fatalf("invalid JSON after redact: %s", out.ToolArgs)
	}
	if string(out.ToolArgs) != `{"email":"[REDACTED]"}` {
		t.Fatalf("ToolArgs = %s", out.ToolArgs)
	}
}

func TestMapJSONRawMessage_Redact_BreaksJSON(t *testing.T) {
	t.Parallel()
	inner := ValidatorFunc[string](func(_ context.Context, _ string) (string, *Report, error) {
		return "{broken", &Report{
			Action:      ActionRedact,
			Validator:   "test",
			MutatedText: "{broken",
		}, nil
	})
	mapped := MapJSONRawMessage(
		inner,
		func(d *toolArgsDTO) json.RawMessage { return d.ToolArgs },
		func(d *toolArgsDTO, raw json.RawMessage) *toolArgsDTO {
			d.ToolArgs = raw
			return d
		},
	)
	original := json.RawMessage(`{"email":"a@b.com"}`)
	in := toolArgsDTO{ToolArgs: original}
	out, rep, err := mapped.Validate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != ActionRetry {
		t.Fatalf("rep = %+v, want ActionRetry", rep)
	}
	if rep.Code != CodeJSONRedactCorrupted {
		t.Fatalf("Code = %q, want %q", rep.Code, CodeJSONRedactCorrupted)
	}
	if !rep.Retryable {
		t.Fatal("expected Retryable")
	}
	if string(out.ToolArgs) != string(original) {
		t.Fatalf("input must not be mutated on invalid JSON redact, got %s", out.ToolArgs)
	}
}

func TestMapJSONRawMessage_Block_Passthrough(t *testing.T) {
	t.Parallel()
	inner := ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, FinishReport(&Report{
			Action:    ActionBlock,
			Validator: "inner",
			Code:      "FORBIDDEN",
			Reason:    "blocked",
		}, ControlSpec{Action: ActionBlock}), nil
	})
	mapped := MapJSONRawMessage(
		inner,
		func(d *toolArgsDTO) json.RawMessage { return d.ToolArgs },
		func(d *toolArgsDTO, raw json.RawMessage) *toolArgsDTO {
			d.ToolArgs = raw
			return d
		},
	)
	original := json.RawMessage(`{"x":1}`)
	out, rep, err := mapped.Validate(context.Background(), toolArgsDTO{ToolArgs: original})
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != ActionBlock || rep.Code != "FORBIDDEN" {
		t.Fatalf("rep = %+v", rep)
	}
	if string(out.ToolArgs) != string(original) {
		t.Fatal("block must not inject mutated raw message")
	}
}

func TestMapJSONRawMessage_WhitespaceNull_NotBypass(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	inner := ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		calls.Add(1)
		return input, FinishReport(&Report{
			Action:    ActionPass,
			Validator: "inner",
		}, ControlSpec{Action: ActionPass}), nil
	})
	mapped := MapJSONRawMessage(
		inner,
		func(d *toolArgsDTO) json.RawMessage { return d.ToolArgs },
		func(d *toolArgsDTO, raw json.RawMessage) *toolArgsDTO {
			d.ToolArgs = raw
			return d
		},
	)
	raw := json.RawMessage(" null ")
	_, _, err := mapped.Validate(context.Background(), toolArgsDTO{ToolArgs: raw})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("inner validator must run for whitespace-padded null literal")
	}
}

func TestMapJSONRawMessage_InjectReturnsNil_Panics(t *testing.T) {
	t.Parallel()
	inner := ValidatorFunc[string](func(_ context.Context, _ string) (string, *Report, error) {
		out := `{"x":1}`
		return out, &Report{
			Action:      ActionRedact,
			Validator:   "test",
			MutatedText: out,
		}, nil
	})
	mapped := MapJSONRawMessage(
		inner,
		func(d *toolArgsDTO) json.RawMessage { return d.ToolArgs },
		func(*toolArgsDTO, json.RawMessage) *toolArgsDTO { return nil },
	)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when inject returns nil after redact")
		}
	}()
	_, _, _ = mapped.Validate(context.Background(), toolArgsDTO{
		ToolArgs: json.RawMessage(`{"email":"a@b.com"}`),
	})
}

func TestMapJSONRawMessage_PanicsOnNilInject(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil inject")
		}
	}()
	_ = MapJSONRawMessage(
		ValidatorFunc[string](func(context.Context, string) (string, *Report, error) {
			return "", nil, nil
		}),
		func(d *toolArgsDTO) json.RawMessage { return d.ToolArgs },
		nil,
	)
}

func TestMapJSONRawMessage_Retry_Passthrough(t *testing.T) {
	t.Parallel()
	inner := ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		return input, FinishReport(&Report{
			Action:    ActionRetry,
			Validator: "inner",
			Code:      "RETRY_ME",
			Feedback:  "fix args",
		}, ControlSpec{Action: ActionRetry}), nil
	})
	mapped := MapJSONRawMessage(
		inner,
		func(d *toolArgsDTO) json.RawMessage { return d.ToolArgs },
		func(d *toolArgsDTO, raw json.RawMessage) *toolArgsDTO {
			d.ToolArgs = raw
			return d
		},
	)
	original := json.RawMessage(`{"x":1}`)
	out, rep, err := mapped.Validate(context.Background(), toolArgsDTO{ToolArgs: original})
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != ActionRetry {
		t.Fatalf("rep = %+v", rep)
	}
	if !rep.Retryable {
		t.Fatal("expected Retryable")
	}
	if string(out.ToolArgs) != string(original) {
		t.Fatal("retry must not inject mutated raw message")
	}
}
