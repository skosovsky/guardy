package jsonredact

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/internal/jsondoc"
)

func contractLeaf() guardy.Validator[string] {
	return guardy.ValidatorFunc[string](func(_ context.Context, input string) (string, *guardy.Report, error) {
		if input == "secret" {
			return input, &guardy.Report{Action: guardy.ActionBlock}, nil
		}
		if input == "email" {
			return "redacted", &guardy.Report{Action: guardy.ActionRedact}, nil
		}
		return input, &guardy.Report{Action: guardy.ActionPass}, nil
	})
}

func TestExactRedaction(t *testing.T) {
	// Arrange.
	raw := `{"id":9007199254740993,"int":9223372036854775807,"uint":18446744073709551615,"decimal":0.100000000000000001,"exponent":1.000000000000000001e20,"nested":["email",null,true]}`
	validator := NewJSONRedactValidator(contractLeaf(), "")
	// Act.
	out, rep, err := validator.Validate(context.Background(), raw)
	// Assert.
	if err != nil || rep.Action != guardy.ActionRedact {
		t.Fatalf("report=%+v err=%v", rep, err)
	}
	expected, err := jsondoc.Decode(strings.ReplaceAll(raw, `"email"`, `"redacted"`))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := jsondoc.Decode(out)
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("output=%s err=%v", out, err)
	}
}

func TestNilLeafFailsAtConstruction(t *testing.T) {
	for _, leaf := range []guardy.Validator[string]{nil, guardy.ValidatorFunc[string](nil)} {
		t.Run("nil", func(t *testing.T) {
			// Arrange / Assert.
			defer func() {
				if recover() == nil {
					t.Fatal("nil leaf accepted")
				}
			}()
			// Act.
			NewJSONRedactValidator(leaf, "")
		})
	}
}

func TestSiblingDecisions(t *testing.T) {
	for _, report := range []*guardy.Report{
		{Action: guardy.ActionBlock}, {Action: guardy.ActionRetry, Retryable: true}, {Action: guardy.ActionPass, Disposition: guardy.DispositionSystemFault},
		{Action: guardy.ActionBlock, ShadowMode: true}, {Action: guardy.ActionRedact, Fatal: true}, {Action: guardy.ActionRedact, PayloadKind: guardy.PayloadTechnicalPayload},
	} {
		for _, raw := range []string{`["bad","email","safe"]`, `["safe","email","bad"]`, `{"bad":"bad","good":"email"}`} {
			// Arrange.
			validator := NewJSONRedactValidator(
				guardy.ValidatorFunc[string](func(ctx context.Context, s string) (string, *guardy.Report, error) {
					if s == "bad" {
						return "changed", report, nil
					}
					return contractLeaf().Validate(ctx, s)
				}),
				"",
			)
			// Act.
			out, actual, err := validator.Validate(context.Background(), raw)
			// Assert.
			if err != nil {
				t.Fatal(err)
			}
			expected := guardy.DecisionFromReport(report)
			if guardy.DecisionFromReport(actual).Disposition != expected.Disposition ||
				actual.PayloadKind != report.PayloadKind {
				t.Fatalf("raw=%s actual=%+v expected=%+v", raw, actual, report)
			}
			if expected.Disposition != guardy.DispositionNone && out != raw {
				t.Fatalf("released partial redaction: %s", out)
			}
		}
	}
}

func FuzzRedactionPreservesValues(f *testing.F) {
	for _, raw := range []string{`{"id":9007199254740993,"email":"email"}`, `["secret","email"]`, `null`, `[18446744073709551615,0.100000000000000001,1e100]`, `{"a":{"a":1}}`} {
		f.Add(raw)
	}
	validator := NewJSONRedactValidator(contractLeaf(), "")
	f.Fuzz(func(t *testing.T, raw string) {
		// Arrange.
		original, parseErr := jsondoc.Decode(raw)
		// Act.
		out, rep, err := validator.Validate(context.Background(), raw)
		// Assert.
		if err != nil {
			t.Fatal(err)
		}
		if parseErr != nil {
			if rep.Code != guardy.CodeJSONInvalid {
				t.Fatal("invalid input accepted")
			}
			return
		}
		if hasSecret(original) {
			if !guardy.DecisionFromReport(rep).IsTerminal() || out != raw {
				t.Fatal("lost denial")
			}
			return
		}
		expected := replaceEmail(original)
		restored, err := jsondoc.Decode(out)
		if err != nil || !json.Valid([]byte(out)) || !reflect.DeepEqual(expected, restored) {
			t.Fatalf("lost JSON values: input=%s output=%s err=%v", raw, out, err)
		}
	})
}

func hasSecret(value any) bool {
	switch v := value.(type) {
	case string:
		return v == "secret"
	case []any:
		return slices.ContainsFunc(v, hasSecret)
	case map[string]any:
		for _, child := range v {
			if hasSecret(child) {
				return true
			}
		}
	}
	return false
}

func replaceEmail(value any) any {
	switch v := value.(type) {
	case string:
		if v == "email" {
			return "redacted"
		}
	case []any:
		for i, child := range v {
			v[i] = replaceEmail(child)
		}
	case map[string]any:
		for key, child := range v {
			v[key] = replaceEmail(child)
		}
	}
	return value
}
