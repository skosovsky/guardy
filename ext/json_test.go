package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestJSON_Valid_Pass(t *testing.T) {
	j := NewJSON(nil, guardy.Block, "JSON")
	ctx := context.Background()
	res, err := j.Validate(ctx, guardy.Input{Text: `{"a":1}`})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Error("expected Pass for valid JSON object")
	}
}

func TestJSON_Valid_Array_Pass(t *testing.T) {
	j := NewJSON(nil, guardy.Block, "JSON")
	ctx := context.Background()
	res, err := j.Validate(ctx, guardy.Input{Text: `[1, 2, 3]`})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Error("expected Pass for valid JSON array")
	}
}

func TestJSON_Invalid_Block(t *testing.T) {
	j := NewJSON(nil, guardy.Block, "JSON")
	ctx := context.Background()
	res, err := j.Validate(ctx, guardy.Input{Text: "not json"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Reason != "invalid JSON" {
		t.Errorf("got Passed=%v Reason=%s", res.Passed, res.Reason)
	}
}

func TestJSON_RequiredKeys_Missing_Block(t *testing.T) {
	j := NewJSON([]string{"id", "name"}, guardy.Block, "JSON")
	ctx := context.Background()
	res, err := j.Validate(ctx, guardy.Input{Text: `{"id": 1}`})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || res.Code != "JSON" {
		t.Errorf("got Passed=%v Code=%s", res.Passed, res.Code)
	}
}

func TestJSON_RequiredKeys_Present_Pass(t *testing.T) {
	j := NewJSON([]string{"id", "name"}, guardy.Block, "JSON")
	ctx := context.Background()
	res, err := j.Validate(ctx, guardy.Input{Text: `{"id": 1, "name": "x"}`})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Error("expected Pass when all required keys present")
	}
}

func TestJSON_WithJSONName(t *testing.T) {
	j := NewJSON(nil, guardy.Block, "JSON", WithJSONName("my-json"))
	if j.Name() != "my-json" {
		t.Errorf("Name() = %q, want my-json", j.Name())
	}
}

func TestJSON_ContextCancelled(t *testing.T) {
	j := NewJSON(nil, guardy.Block, "JSON")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := j.Validate(ctx, guardy.Input{Text: `{"a":1}`})
	if err == nil {
		t.Error("expected error when context cancelled")
	}
}
