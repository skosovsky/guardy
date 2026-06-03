package jsonredact

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestJSONRedactValidator_RedactsNestedEmail(t *testing.T) {
	t.Parallel()
	leaf := guardy.ValidatorFunc[string](func(_ context.Context, input string) (string, *guardy.Report, error) {
		if strings.Contains(input, "@") {
			return "[REDACTED]", &guardy.Report{Action: guardy.ActionRedact, Validator: "email"}, nil
		}
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: "email"}, nil
	})
	v := NewJSONRedactValidator(leaf, "jsonredact")
	raw := `{"user":{"email":"a@b.com"}}`
	out, rep, err := v.Validate(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionRedact {
		t.Fatalf("rep = %+v", rep)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("invalid JSON: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("output = %s", out)
	}
}

func TestJSONRedactValidator_BlockPreservesJSONSyntax(t *testing.T) {
	t.Parallel()
	leaf := guardy.ValidatorFunc[string](func(_ context.Context, input string) (string, *guardy.Report, error) {
		if input == "secret" {
			return input, &guardy.Report{Action: guardy.ActionBlock, Validator: "block", Reason: "blocked"}, nil
		}
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: "pass"}, nil
	})
	v := NewJSONRedactValidator(leaf, "jsonredact")
	_, rep, err := v.Validate(context.Background(), `{"token":"secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != guardy.ActionBlock {
		t.Fatalf("rep = %+v", rep)
	}
}
