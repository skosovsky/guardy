package guardy_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	g "github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

type piiArguments struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (v *piiArguments) ValidatePostBind(context.Context) error {
	v.Name = strings.ToUpper(v.Name)
	return nil
}

func TestTypedAndDynamicPIICanonicalAuthoritativeArguments(t *testing.T) {
	// Arrange.
	raw := `{"name":"Ada","email":"alice@example.com"}`
	guard := g.NewPipeline(g.WithFastPath(ext.NewPIIValidator()))
	typed := g.MustCompileArgs[piiArguments](guard)
	typedCalls, dynamicCalls := 0, 0
	typedHandler := g.WrapArgs(
		typed,
		nil,
		func(_ context.Context, value piiArguments) (piiArguments, error) { typedCalls++; return value, nil },
	)
	dynamic := g.MustCompileJSONArgs(guard, nil)
	dynamicHandler := g.WrapGuardedJSONArgs(
		dynamic,
		nil,
		func(_ context.Context, value g.GuardedJSONArgs) (map[string]any, error) {
			dynamicCalls++
			return value.Object, nil
		},
	)
	// Act.
	typedValue, typedBoundary, typedErr := typedHandler(context.Background(), raw)
	dynamicValue, dynamicBoundary, dynamicErr := dynamicHandler(context.Background(), raw)
	typedCanonical, typedEncodeErr := json.Marshal(typedValue)
	dynamicCanonical, dynamicEncodeErr := json.Marshal(dynamicValue)
	// Assert: handlers receive sanitized values; canonical raw reflects hooks too.
	if typedErr != nil || dynamicErr != nil || typedEncodeErr != nil || dynamicEncodeErr != nil || typedCalls != 1 ||
		dynamicCalls != 1 {
		t.Fatalf(
			"typed=%v dynamic=%v encoded=%v %v calls=%d %d",
			typedErr,
			dynamicErr,
			typedEncodeErr,
			dynamicEncodeErr,
			typedCalls,
			dynamicCalls,
		)
	}
	if typedValue.Email != "[REDACTED]" || typedValue.Name != "ADA" || dynamicValue["email"] != "[REDACTED]" ||
		dynamicValue["name"] != "Ada" {
		t.Fatalf("typed=%+v dynamic=%+v", typedValue, dynamicValue)
	}
	if typedBoundary.SanitizedRaw != string(typedCanonical) ||
		dynamicBoundary.SanitizedRaw != string(dynamicCanonical) ||
		strings.Contains(typedBoundary.SanitizedRaw+dynamicBoundary.SanitizedRaw, "alice@example.com") {
		t.Fatalf("canonical data mismatch: %+v %+v", typedBoundary, dynamicBoundary)
	}
}

func TestDynamicJSONLargeIntegerCanonicalPrecision(t *testing.T) {
	// Arrange / Act.
	p := g.MustCompileJSONArgs(g.NewPipeline[string](), nil)
	boundary, err := p.Validate(context.Background(), nil, ` { "id": 9007199254740993 } `)
	// Assert: decode and re-encode cannot silently change the executable argument.
	if err != nil || fmt.Sprint(boundary.Object["id"]) != "9007199254740993" ||
		boundary.SanitizedRaw != `{"id":9007199254740993}` {
		t.Fatalf("%+v %v", boundary, err)
	}
}
