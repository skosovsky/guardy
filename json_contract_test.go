package guardy_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
	"github.com/skosovsky/guardy/ext/jsonredact"
	"github.com/skosovsky/guardy/ext/jsonschema"
)

func TestJSONNumberSurvivesActualPIIPipelineAndArgs(t *testing.T) {
	// Arrange.
	redactor := jsonredact.NewJSONRedactValidator(ext.NewPIIValidator(), "pii-json")
	raw := `{"id":9007199254740993,"profile":{"email":"alice@example.com"}}`
	pipeline := guardy.NewPipeline(guardy.WithFastPath(redactor))
	typed := guardy.MustCompileArgs[map[string]any](pipeline)
	dynamic := guardy.MustCompileJSONArgs(pipeline, nil)
	// Act.
	direct, rep, err := redactor.Validate(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := typed.Validate(context.Background(), guardy.NewScope(), raw)
	if err != nil {
		t.Fatal(err)
	}
	object, err := dynamic.Validate(context.Background(), guardy.NewScope(), raw)
	if err != nil {
		t.Fatal(err)
	}
	// Assert.
	if rep.Action != guardy.ActionRedact {
		t.Fatalf("report=%+v", rep)
	}
	for _, payload := range []string{direct, bound.SanitizedRaw, object.SanitizedRaw} {
		if !strings.Contains(payload, `9007199254740993`) || strings.Contains(payload, `alice@example.com`) {
			t.Fatalf("output=%s", payload)
		}
	}
	if bound.Value["id"] != json.Number("9007199254740993") || object.Object["id"] != json.Number("9007199254740993") {
		t.Fatalf("typed=%v dynamic=%v", bound.Value, object.Object)
	}
}

type schemaContractArgs struct {
	Profile struct {
		Role string `json:"role,omitempty"`
	} `json:"profile"`
}

func TestFinalSchemaPreventsHandlerAfterTransformation(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"profile":{"type":"object","required":["role"],"properties":{"role":{"type":"string"}}}}}`,
		`{"type":"object","properties":{"profile":{"type":"object","properties":{"role":{"enum":["admin"]}}}}}`,
	} {
		t.Run(schema, func(t *testing.T) {
			// Arrange: typed binding omits the emptied required field; dynamic binding changes its enum value.
			leaf := guardy.ValidatorFunc[string](func(_ context.Context, s string) (string, *guardy.Report, error) {
				if s == "admin" {
					return "changed", &guardy.Report{Action: guardy.ActionRedact}, nil
				}
				return s, &guardy.Report{Action: guardy.ActionPass}, nil
			})
			redactor := jsonredact.NewJSONRedactValidator(leaf, "role")
			rawGuard := guardy.NewPipeline(guardy.WithFastPath(redactor))
			if strings.Contains(schema, `"required"`) {
				removeField := guardy.ValidatorFunc[string](
					func(_ context.Context, raw string) (string, *guardy.Report, error) {
						var object map[string]any
						if err := json.Unmarshal([]byte(raw), &object); err != nil {
							return raw, nil, err
						}
						delete(object["profile"].(map[string]any), "role")
						encoded, err := json.Marshal(object)
						return string(encoded), &guardy.Report{Action: guardy.ActionRedact}, err
					},
				)
				rawGuard = guardy.NewPipeline(guardy.WithFastPath(redactor, removeField))
			}
			checker, err := jsonschema.NewJSONSchemaValidator(schema)
			if err != nil {
				t.Fatal(err)
			}
			final := guardy.NewPipeline(guardy.WithFastPath(checker))
			typed := guardy.MustCompileArgs[schemaContractArgs](
				rawGuard,
				guardy.WithRequiredArgsFinalGuard[schemaContractArgs](final),
			)
			calls := 0
			wrapped := guardy.WrapArgs(
				typed,
				nil,
				func(_ context.Context, _ schemaContractArgs) (string, error) { calls++; return "ok", nil },
			)
			dynamic := guardy.MustCompileJSONArgs(rawGuard, nil, guardy.WithJSONArgsFinalGuard(final))
			wrappedJSON := guardy.WrapGuardedJSONArgs(
				dynamic,
				nil,
				func(_ context.Context, _ guardy.GuardedJSONArgs) (string, error) { calls++; return "ok", nil },
			)
			// Act.
			_, _, typedErr := wrapped(context.Background(), `{"profile":{"role":"admin"}}`)
			_, _, dynamicErr := wrappedJSON(context.Background(), `{"profile":{"role":"admin"}}`)
			// Assert.
			if !errors.Is(typedErr, guardy.ErrRetryRequested) || !errors.Is(dynamicErr, guardy.ErrRetryRequested) ||
				calls != 0 {
				t.Fatalf("typed=%v dynamic=%v calls=%d", typedErr, dynamicErr, calls)
			}
		})
	}
}

func TestArgsRejectDuplicateKeysAndTrailingDocuments(t *testing.T) {
	// Arrange.
	pipeline := guardy.NewPipeline[string]()
	typed := guardy.MustCompileArgs[map[string]any](pipeline)
	dynamic := guardy.MustCompileJSONArgs(pipeline, nil)
	// Act / Assert.
	for _, raw := range []string{`{"profile":{"role":"user","role":"admin"}}`, `{} {}`} {
		_, typedErr := typed.Validate(context.Background(), guardy.NewScope(), raw)
		_, dynamicErr := dynamic.Validate(context.Background(), guardy.NewScope(), raw)
		if !errors.Is(typedErr, guardy.ErrRetryRequested) || !errors.Is(dynamicErr, guardy.ErrRetryRequested) {
			t.Fatalf("%s: %v / %v", raw, typedErr, dynamicErr)
		}
	}
}
