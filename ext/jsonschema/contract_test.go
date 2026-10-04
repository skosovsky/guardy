package jsonschema

import (
	"context"
	"fmt"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestDialectContracts(t *testing.T) {
	tests := []struct{ name, schema, valid, invalid string }{
		{
			"unevaluated",
			`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"ok":true},"unevaluatedProperties":false}`,
			`{"ok":1}`,
			`{"unexpected":true}`,
		},
		{
			"prefix",
			`{"$schema":"https://json-schema.org/draft/2020-12/schema","prefixItems":[{"const":"ok"},{"type":"integer"}],"items":false}`,
			`["ok",1]`,
			`["wrong",1]`,
		},
		{
			"nested reference",
			`{"$defs":{"leaf":{"type":"integer"},"node":{"type":"object","properties":{"id":{"$ref":"#/$defs/leaf"}},"required":["id"]}},"$ref":"#/$defs/node"}`,
			`{"id":1}`,
			`{"id":"1"}`,
		},
		{"null", `{"type":"null"}`, `null`, `1`},
		{"annotation", `{"unknownAnnotation":{"anything":true},"type":"string"}`, `"ok"`, `false`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			validator, err := NewJSONSchemaValidator(tc.schema)
			if err != nil {
				t.Fatal(err)
			}
			// Act / Assert.
			for raw, action := range map[string]guardy.Action{tc.valid: guardy.ActionPass, tc.invalid: guardy.ActionRetry} {
				_, report, err := validator.Validate(context.Background(), raw)
				if err != nil || report.Action != action {
					t.Fatalf("%s: report=%+v err=%v", raw, report, err)
				}
			}
		})
	}
	for _, dialect := range []string{"draft-04", "draft-06", "draft-07", "draft/2019-09", "draft/2020-12"} {
		t.Run(dialect, func(t *testing.T) {
			// Arrange / Act.
			validator, err := NewJSONSchemaValidator(
				fmt.Sprintf(`{"$schema":"https://json-schema.org/%s/schema","type":"boolean"}`, dialect),
			)
			// Assert.
			if err != nil {
				t.Fatal(err)
			}
			_, rep, err := validator.Validate(context.Background(), `1`)
			if err != nil || rep.Action != guardy.ActionRetry {
				t.Fatalf("report=%+v err=%v", rep, err)
			}
		})
	}
}

func TestExactNumericConstraints(t *testing.T) {
	for _, exact := range []string{"9007199254740993", "9223372036854775807", "18446744073709551615", "0.100000000000000001", "1.000000000000000001e20"} {
		t.Run(exact, func(t *testing.T) {
			// Arrange: schema bounds must retain the same precision as the input.
			validator, err := NewJSONSchemaValidator(`{"minimum":` + exact + `,"maximum":` + exact + `}`)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			_, pass, err := validator.Validate(context.Background(), exact)
			if err != nil {
				t.Fatal(err)
			}
			_, deny, err := validator.Validate(context.Background(), `0`)
			// Assert.
			if err != nil || pass.Action != guardy.ActionPass || deny.Action != guardy.ActionRetry {
				t.Fatalf("pass=%+v deny=%+v err=%v", pass, deny, err)
			}
		})
	}
	// Arrange.
	validator, err := NewJSONSchemaValidator(`{"minimum":9007199254740993,"multipleOf":0.1}`)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	_, rep, err := validator.Validate(context.Background(), `9007199254740992`)
	// Assert.
	if err != nil || rep.Action != guardy.ActionRetry {
		t.Fatalf("report=%+v err=%v", rep, err)
	}
}

func TestSchemaResourcesAndCompilationFailures(t *testing.T) {
	// Arrange.
	meta := `{"$schema":"https://json-schema.org/draft/2020-12/schema","$vocabulary":{"https://json-schema.org/draft/2020-12/vocab/core":true,"urn:unknown:vocab":true}}`
	resources := map[string]string{"urn:test:meta": meta, "urn:test:leaf": `{"type":"integer"}`}
	// Act / Assert: custom required vocabulary cannot be silently ignored.
	if _, err := NewJSONSchemaValidatorWithResources(`{"$schema":"urn:test:meta"}`, resources); err == nil {
		t.Fatal("unknown required vocabulary compiled")
	}
	validator, err := NewJSONSchemaValidatorWithResources(`{"$ref":"urn:test:leaf"}`, resources)
	if err != nil {
		t.Fatal(err)
	}
	_, rep, err := validator.Validate(context.Background(), `"wrong"`)
	if err != nil || rep.Action != guardy.ActionRetry {
		t.Fatalf("report=%+v err=%v", rep, err)
	}
	for _, schema := range []string{`{"$schema":"https://json-schema.org/draft/2099-01/schema"}`, `{"$ref":"https://example.invalid/schema"}`, `{"$ref":"file:///tmp/schema.json"}`, `{"type":"string","type":"number"}`, `true false`} {
		if _, err := NewJSONSchemaValidator(schema); err == nil {
			t.Fatalf("compiled %s", schema)
		}
	}
}

func TestSchemaRejectsAmbiguousDocuments(t *testing.T) {
	// Arrange.
	validator, err := NewJSONSchemaValidator(`true`)
	if err != nil {
		t.Fatal(err)
	}
	// Act / Assert.
	for _, raw := range []string{`{"a":1,"a":2}`, `{"nested":{"a":1,"\u0061":2}}`, `null false`} {
		_, report, err := validator.Validate(context.Background(), raw)
		if err != nil || report.Code != guardy.CodeJSONInvalid {
			t.Fatalf("raw=%s report=%+v err=%v", raw, report, err)
		}
	}
}

func TestUnrepresentableSchemaNumbersFailClosed(t *testing.T) {
	// Arrange.
	const huge = `1e100000000000000000000000`
	// Act / Assert.
	for _, schema := range []string{`{"minimum":` + huge + `}`, `{"maximum":-` + huge + `}`, `{"const":{"id":` + huge + `}}`, `{"properties":{"id":{"minimum":` + huge + `}}}`} {
		if _, err := NewJSONSchemaValidator(schema); err == nil {
			t.Fatalf("compiled unrepresentable assertion: %s", schema)
		}
	}
	validator, err := NewJSONSchemaValidator(`{"minimum":0,"annotation":` + huge + `}`)
	if err != nil {
		t.Fatal(err)
	}
	_, report, err := validator.Validate(context.Background(), huge)
	if err != nil || report.Action != guardy.ActionRetry {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestAdjacentExactValuesAreDistinct(t *testing.T) {
	for _, tc := range []struct{ schema, value string }{
		{`{"minimum":0.100000000000000002}`, `0.100000000000000001`},
		{`{"maximum":1.000000000000000001e20}`, `1.000000000000000002e20`},
		{`{"maximum":18446744073709551614}`, `18446744073709551615`},
		{`{"multipleOf":0.1}`, `0.100000000000000001`},
	} {
		// Arrange.
		validator, err := NewJSONSchemaValidator(tc.schema)
		if err != nil {
			t.Fatal(err)
		}
		// Act.
		_, report, err := validator.Validate(context.Background(), tc.value)
		// Assert.
		if err != nil || report.Action != guardy.ActionRetry {
			t.Fatalf("%s: report=%+v err=%v", tc.schema, report, err)
		}
	}
}

func TestSchemaCountsCannotOverflow(t *testing.T) {
	// Arrange / Act / Assert.
	for _, keyword := range []string{"minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties", "minContains", "maxContains"} {
		if _, err := NewJSONSchemaValidator(
			fmt.Sprintf(`{"contains":true,"%s":18446744073709551616}`, keyword),
		); err == nil {
			t.Fatalf("count overflow compiled: %s", keyword)
		}
	}
}

func TestReferencedAnnotationsUseActualAssertions(t *testing.T) {
	// Arrange / Act / Assert.
	for _, schema := range []string{
		`{"$ref":"#/custom","custom":{"minimum":1e100000000000000000000000}}`,
		`{"$ref":"#/custom/0","custom":[{"maximum":-1e100000000000000000000000}]}`,
	} {
		if _, err := NewJSONSchemaValidator(schema); err == nil {
			t.Fatalf("unsafe referenced assertion compiled: %s", schema)
		}
	}
	for _, keyword := range []string{"minContains", "maxContains", "const"} {
		schema := fmt.Sprintf(
			`{"$schema":"https://json-schema.org/draft-04/schema","type":"string","%s":1e100000000000000000000000}`,
			keyword,
		)
		if _, err := NewJSONSchemaValidator(schema); err != nil {
			t.Fatalf("rejected draft4 annotation: %s: %v", keyword, err)
		}
	}
}

func TestLargeExactEnumsDoNotCollapseDuringCompilation(t *testing.T) {
	// Arrange.
	validator, err := NewJSONSchemaValidator(`{"enum":[18446744073709551615,18446744073709551616]}`)
	if err != nil {
		t.Fatal(err)
	}
	// Act / Assert.
	for _, raw := range []string{`18446744073709551615`, `18446744073709551616`} {
		_, report, err := validator.Validate(context.Background(), raw)
		if err != nil || report.Action != guardy.ActionPass {
			t.Fatalf("report=%+v err=%v", report, err)
		}
	}
}

func TestResourceURIsCannotEvadeNumericChecks(t *testing.T) {
	// Arrange / Act / Assert.
	for _, uri := range []string{"relative.json", "urn:resource#fragment", "urn:resource#"} {
		if _, err := NewJSONSchemaValidatorWithResources(
			`true`,
			map[string]string{uri: `{"minimum":1e100000000000000000000000}`},
		); err == nil {
			t.Fatalf("invalid resource URI accepted: %s", uri)
		}
	}
	if _, err := NewJSONSchemaValidatorWithResources(
		`{"$ref":"https://example.invalid/a%20b"}`,
		map[string]string{"https://example.invalid/a b": `{"minimum":1e100000000000000000000000}`},
	); err == nil {
		t.Fatal("normalized URI lost original assertion")
	}
}
