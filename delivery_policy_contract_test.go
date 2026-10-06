package guardy_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	g "github.com/skosovsky/guardy"
)

type customDeliveryString string

func (customDeliveryString) MarshalJSON() ([]byte, error) {
	panic("UserText must never call custom serialization")
}

func TestGenericDeliveryRequiresExplicitContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		policy g.DeliveryPolicy
	}{
		{"zero", g.DeliveryPolicy{}},
		{"missing_kinds", g.NewDeliveryPolicy("host", g.WithDeliveryClassifier(g.UserTextClassifier))},
		{"missing_classifier", g.NewDeliveryPolicy("host", g.WithDeliveryAllowedKinds(g.PayloadSafeUserText))},
		{"invalid_kind", g.NewUserTextPolicy("host", g.WithDeliveryAllowedKinds(g.PayloadKind(255)))},
		{"fallback_type", g.NewUserTextPolicy("host", g.WithDeliveryFallback(123))},
		{"typed_nil_mismatch", g.NewUserTextPolicy("host", g.WithDeliveryFallback((*string)(nil)))},
		{"nil_option", g.NewUserTextPolicy("host", nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			calls := 0
			pipeline := g.MustNewPipeline(g.WithSequential(g.ValidatorFunc[string](
				func(_ context.Context, value string) (string, *g.Report, error) {
					calls++
					return value, nil, nil
				})))
			// Act.
			delivery, err := pipeline.GuardDelivery(context.Background(), nil, tc.policy, "payload")
			// Assert.
			var failure *g.PolicyFailure
			if calls != 0 || !errors.Is(err, g.ErrConfiguration) || !errors.As(err, &failure) ||
				failure.Decision != delivery.Decision || !delivery.Decision.IsSystemFault() || delivery.Deliverable {
				t.Fatalf("calls=%d delivery=%+v error=%v", calls, delivery, err)
			}
		})
	}
}

func TestGenericDeliveryUsesCallerRepresentationClassifier(t *testing.T) {
	t.Parallel()
	// Arrange: this host classifies its custom wire representation explicitly.
	calls := 0
	policy := g.NewDeliveryPolicy("wire",
		g.WithDeliveryAllowedKinds(g.PayloadTechnicalPayload),
		g.WithDeliveryClassifier(func(value any) (g.PayloadKind, error) {
			calls++
			if _, ok := value.(customDeliveryString); !ok {
				return g.PayloadSafeUserText, errors.New("wrong representation")
			}
			return g.PayloadTechnicalPayload, nil
		}))
	pipeline := g.MustNewPipeline[customDeliveryString]()
	// Act.
	delivery, err := pipeline.GuardDelivery(context.Background(), nil, policy, "value")
	projection, ok := delivery.Projection()
	// Assert: no shape heuristic or custom MarshalJSON call occurred.
	if err != nil || calls != 1 || !ok || projection.Value != "value" ||
		delivery.Kind != g.PayloadTechnicalPayload || delivery.Decision.PayloadKind != delivery.Kind {
		t.Fatalf("calls=%d delivery=%+v error=%v", calls, delivery, err)
	}
}

func TestUserTextRepresentationMatrix(t *testing.T) {
	t.Parallel()
	plain := "hello"
	structured := `{"tool":true}`
	var nilText *string
	for _, tc := range []struct {
		name        string
		value       any
		kind        g.PayloadKind
		deliverable bool
		fault       bool
	}{
		{"string", plain, g.PayloadSafeUserText, true, false},
		{"text_pointer", &plain, g.PayloadSafeUserText, true, false},
		{"nil_text_pointer", nilText, g.PayloadSafeUserText, true, false},
		{"json_pointer", &structured, g.PayloadTechnicalPayload, false, false},
		{"bytes", []byte(plain), g.PayloadSafeUserText, true, false},
		{"json_bytes", []byte(structured), g.PayloadTechnicalPayload, false, false},
		{"raw_message", json.RawMessage(structured), g.PayloadTechnicalPayload, false, false},
		{"struct", struct{ Name string }{"hello"}, g.PayloadTechnicalPayload, false, false},
		{"nil_interface", nil, g.PayloadSafeUserText, false, true},
		{"unsupported_number", 123, g.PayloadSafeUserText, false, true},
		{"custom_marshaler", customDeliveryString("hello"), g.PayloadSafeUserText, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			pipeline := g.MustNewPipeline[any]()
			// Act.
			delivery, err := pipeline.GuardOutput(context.Background(), nil, tc.value)
			_, projected := delivery.Projection()
			// Assert.
			if delivery.Kind != tc.kind || delivery.Deliverable != tc.deliverable || projected != tc.deliverable ||
				delivery.Decision.IsSystemFault() != tc.fault {
				t.Fatalf("delivery=%+v error=%v", delivery, err)
			}
			if tc.fault && (!errors.Is(err, g.ErrDeliveryClassification) || delivery.Value != nil) {
				t.Fatalf("classification fault: delivery=%+v error=%v", delivery, err)
			}
			if tc.deliverable && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTypedNilFallbackIsChecked(t *testing.T) {
	t.Parallel()
	// Arrange: nil of the correct concrete pointer type is a configured fallback.
	var fallback *string
	calls := 0
	input := "denied"
	pipeline := g.MustNewPipeline(g.WithSequential(g.ValidatorFunc[*string](
		func(_ context.Context, value *string) (*string, *g.Report, error) {
			calls++
			if value != nil {
				return value, &g.Report{Action: g.ActionBlock}, nil
			}
			return value, nil, nil
		})))
	policy := g.NewUserTextPolicy("user", g.WithDeliveryFallback(fallback))
	// Act.
	delivery, err := pipeline.GuardDelivery(context.Background(), nil, policy, &input)
	// Assert.
	if !errors.Is(err, g.ErrBlocked) || calls != 2 || !delivery.Fallback ||
		!delivery.Deliverable || delivery.Value != nil {
		t.Fatalf("calls=%d delivery=%+v error=%v", calls, delivery, err)
	}
}

func TestClassifierFailureNeverUsesFallback(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"error", "panic", "invalid_kind", "cancellation"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			policy := g.NewDeliveryPolicy("host",
				g.WithDeliveryAllowedKinds(g.PayloadSafeUserText),
				g.WithDeliveryFallback("fallback"),
				g.WithDeliveryClassifier(func(any) (g.PayloadKind, error) {
					calls++
					switch mode {
					case "panic":
						panic("private detail")
					case "invalid_kind":
						return g.PayloadKind(255), nil
					case "cancellation":
						cancel()
						return g.PayloadSafeUserText, nil
					default:
						return g.PayloadSafeUserText, errors.New("private detail")
					}
				}))
			// Act.
			delivery, err := g.MustNewPipeline[string]().GuardDelivery(ctx, nil, policy, "value")
			// Assert.
			if calls != 1 || !errors.Is(err, g.ErrValidatorFailed) || !delivery.Decision.IsSystemFault() ||
				delivery.Deliverable || delivery.Fallback || delivery.Value != "" {
				t.Fatalf("calls=%d delivery=%+v error=%v", calls, delivery, err)
			}
		})
	}
}

func TestUserTextDereferenceLimit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		depth int
		nil   bool
		fault bool
	}{
		{"below", 63, false, false},
		{"exact", 64, false, false},
		{"above", 65, false, true},
		{"nil_exact", 64, true, false},
		{"nil_above", 65, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Arrange: build runtime pointer types without recursive source declarations.
			value := reflect.ValueOf("hello")
			for range tc.depth {
				pointer := reflect.New(value.Type())
				pointer.Elem().Set(value)
				value = pointer
			}
			if tc.nil {
				value = reflect.Zero(value.Type())
			}
			// Act.
			delivery, err := g.MustNewPipeline[any]().GuardOutput(context.Background(), nil, value.Interface())
			// Assert.
			if delivery.Decision.IsSystemFault() != tc.fault || delivery.Deliverable == tc.fault {
				t.Fatalf("depth=%d fault=%v deliverable=%v error=%v", tc.depth, tc.fault, delivery.Deliverable, err)
			}
			if tc.fault && !errors.Is(err, g.ErrDeliveryClassification) {
				t.Fatalf("depth fault category lost: %v", err)
			}
		})
	}
}
