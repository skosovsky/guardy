package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestNewTechnicalJSONClassifier_ToolCallPayload(t *testing.T) {
	t.Parallel()
	v := NewTechnicalJSONClassifier(WithCode("TECHNICAL_JSON"))
	_, rep, err := v.Validate(context.Background(), `{"tool":"search","arguments":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.PayloadKind != guardy.PayloadTechnicalPayload {
		t.Fatalf("PayloadKind = %v", rep.PayloadKind)
	}
}

func TestNewTechnicalJSONClassifier_SafeText(t *testing.T) {
	t.Parallel()
	v := NewTechnicalJSONClassifier()
	_, rep, err := v.Validate(context.Background(), "Hello, user!")
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.PayloadKind != guardy.PayloadSafeUserText {
		t.Fatalf("PayloadKind = %v", rep.PayloadKind)
	}
}

func TestNewTechnicalJSONClassifier_JSONWithoutToolKeys(t *testing.T) {
	t.Parallel()
	v := NewTechnicalJSONClassifier()
	_, rep, err := v.Validate(context.Background(), `{"name":"Ada"}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.PayloadKind != guardy.PayloadSafeUserText {
		t.Fatalf("PayloadKind = %v", rep.PayloadKind)
	}
}

func TestNewTechnicalJSONClassifier_ToolCallsArray(t *testing.T) {
	t.Parallel()
	v := NewTechnicalJSONClassifier()
	_, rep, err := v.Validate(context.Background(), `{"tool_calls":[{"function":{"name":"search"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.PayloadKind != guardy.PayloadTechnicalPayload {
		t.Fatalf("PayloadKind = %v", rep.PayloadKind)
	}
}
