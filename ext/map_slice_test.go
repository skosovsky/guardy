package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

type chatMessage struct {
	Content string
}

func TestMapSlice_Pass(t *testing.T) {
	v := guardy.ValidatorFunc[string](func(_ context.Context, input string) (string, *guardy.Report, error) {
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: "pass"}, nil
	})
	mapped := MapSlice(
		func(m chatMessage) string { return m.Content },
		func(m chatMessage, s string) chatMessage {
			m.Content = s
			return m
		},
		v,
	)
	in := []chatMessage{{Content: "hello"}}
	out, rep, err := mapped.Validate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Fatalf("action = %v", rep.Action)
	}
	if out[0].Content != "hello" {
		t.Fatalf("out = %+v", out)
	}
}

func TestMapSlice_Redact(t *testing.T) {
	v := guardy.ValidatorFunc[string](func(_ context.Context, input string) (string, *guardy.Report, error) {
		if input == "bad" {
			return "clean", &guardy.Report{
				Action:      guardy.ActionRedact,
				Validator:   "redactor",
				Reason:      "mutated",
				MutatedText: "clean",
			}, nil
		}
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: "pass"}, nil
	})
	mapped := MapSlice(
		func(m chatMessage) string { return m.Content },
		func(m chatMessage, s string) chatMessage {
			m.Content = s
			return m
		},
		v,
	)
	in := []chatMessage{{Content: "ok"}, {Content: "bad"}}
	out, rep, err := mapped.Validate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Fatalf("action = %v", rep.Action)
	}
	if out[0].Content != "ok" || out[1].Content != "clean" {
		t.Fatalf("out = %+v", out)
	}
}

func TestMapSlice_Block_NoPartialCommit(t *testing.T) {
	v := guardy.ValidatorFunc[string](func(_ context.Context, input string) (string, *guardy.Report, error) {
		switch input {
		case "mutate":
			return "clean", &guardy.Report{Action: guardy.ActionRedact, Validator: "r", Reason: "mutate"}, nil
		case "block":
			return input, &guardy.Report{Action: guardy.ActionBlock, Validator: "b", Reason: "blocked"}, nil
		default:
			return input, &guardy.Report{Action: guardy.ActionPass, Validator: "p"}, nil
		}
	})
	mapped := MapSlice(
		func(m chatMessage) string { return m.Content },
		func(m chatMessage, s string) chatMessage {
			m.Content = s
			return m
		},
		v,
	)
	in := []chatMessage{{Content: "mutate"}, {Content: "block"}}
	out, rep, err := mapped.Validate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Fatalf("action = %v", rep.Action)
	}
	if out[0].Content != "mutate" {
		t.Fatalf("expected original output on block, got %+v", out)
	}
	if rep.Reason == "" || rep.Reason[:7] != "item[1]" {
		t.Fatalf("reason must include item index, got %q", rep.Reason)
	}
}

func TestMapSlice_Retry_BlocksCollection(t *testing.T) {
	v := guardy.ValidatorFunc[string](func(_ context.Context, input string) (string, *guardy.Report, error) {
		if input == "retry" {
			return input, &guardy.Report{
				Action:    guardy.ActionRetry,
				Validator: "retry",
				Reason:    "bad output",
				Feedback:  "fix",
			}, nil
		}
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: "pass"}, nil
	})
	mapped := MapSlice(
		func(m chatMessage) string { return m.Content },
		func(m chatMessage, s string) chatMessage {
			m.Content = s
			return m
		},
		v,
	)
	in := []chatMessage{{Content: "ok"}, {Content: "retry"}}
	out, rep, err := mapped.Validate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRetry {
		t.Fatalf("action = %v", rep.Action)
	}
	if out[1].Content != "retry" {
		t.Fatalf("output must remain original on retry: %+v", out)
	}
}
