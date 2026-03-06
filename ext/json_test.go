package ext

import (
	"context"
	"fmt"
	"testing"

	"github.com/skosovsky/guardy"
)

func ExampleJSONSchema_Validate_retry() {
	j := MustJSONSchema(emptySchema, "JSON")
	ctx := context.Background()
	res, _ := j.Validate(ctx, &guardy.Input{Data: "not json"})
	if !res.Passed && res.Action == guardy.Retry {
		fmt.Println(res.Reason)
	}
	// Output:
	// invalid JSON
}

const emptySchema = "{}"
const objectRequiredSchema = `{"type":"object","required":["id","name"]}`

func TestJSONSchema_Valid_Pass(t *testing.T) {
	j, err := NewJSONSchema(emptySchema, "JSON")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := j.Validate(ctx, &guardy.Input{Data: `{"a":1}`})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Error("expected Pass for valid JSON object")
	}
}

func TestJSONSchema_Valid_Array_Pass(t *testing.T) {
	j, err := NewJSONSchema(emptySchema, "JSON")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := j.Validate(ctx, &guardy.Input{Data: `[1, 2, 3]`})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Error("expected Pass for valid JSON array")
	}
}

func TestJSONSchema_Invalid_Retry(t *testing.T) {
	j, err := NewJSONSchema(emptySchema, "JSON")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := j.Validate(ctx, &guardy.Input{Data: "not json"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Reason != "invalid JSON" {
		t.Errorf("got Passed=%v Reason=%s", res.Passed, res.Reason)
	}
	if res.Action != guardy.Retry {
		t.Errorf("Action = %s, want Retry", res.Action)
	}
}

// TestJSONSchema_SchemaMismatch_Retry checks that missing required keys in schema yields Retry with code.
func TestJSONSchema_SchemaMismatch_Retry(t *testing.T) {
	j, err := NewJSONSchema(objectRequiredSchema, "JSON")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := j.Validate(ctx, &guardy.Input{Data: `{"id": 1}`})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Code != "JSON" {
		t.Errorf("got Passed=%v Code=%s", res.Passed, res.Code)
	}
	if res.Action != guardy.Retry {
		t.Errorf("Action = %s, want Retry", res.Action)
	}
}

// TestJSONSchema_ObjectSchema_Valid_Pass checks that object conforming to required keys passes.
func TestJSONSchema_ObjectSchema_Valid_Pass(t *testing.T) {
	j, err := NewJSONSchema(objectRequiredSchema, "JSON")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := j.Validate(ctx, &guardy.Input{Data: `{"id": 1, "name": "x"}`})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Error("expected Pass when object conforms to schema")
	}
}

func TestJSONSchema_WithJSONSchemaName(t *testing.T) {
	j, err := NewJSONSchema(emptySchema, "JSON", WithJSONSchemaName("my-json"))
	if err != nil {
		t.Fatal(err)
	}
	if j.Name() != "my-json" {
		t.Errorf("Name() = %q, want my-json", j.Name())
	}
}

func TestJSONSchema_WithJSONName(t *testing.T) {
	j, err := NewJSONSchema(emptySchema, "JSON", WithJSONName("my-json-alias"))
	if err != nil {
		t.Fatal(err)
	}
	if j.Name() != "my-json-alias" {
		t.Errorf("Name() = %q, want my-json-alias", j.Name())
	}
}

func TestJSONSchema_GuidanceOnSchemaFailure(t *testing.T) {
	j, err := NewJSONSchema(objectRequiredSchema, "SCHEMA")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := j.Validate(ctx, &guardy.Input{Data: `{"id": 1}`})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed {
		t.Fatal("expected failure for schema mismatch")
	}
	if res.Guidance == "" {
		t.Error("expected Guidance set on schema failure")
	}
	if res.Action != guardy.Retry {
		t.Errorf("Action = %s, want Retry", res.Action)
	}
}
