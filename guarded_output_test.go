package guardy

import (
	"context"
	"errors"
	"testing"
)

func TestPipeline_GuardOutputDeliverable(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline(WithFastPath(ValidatorFunc[string](
		func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "classifier",
				PayloadKind: PayloadSafeUserText,
			}, ControlSpec{Action: ActionPass}), nil
		},
	)))

	// Act.
	output, err := pipeline.GuardOutput(context.Background(), nil, "hello")

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	value, ok := output.DeliverableValue()
	if !ok {
		t.Fatalf("output should be deliverable: %+v", output)
	}
	if value != "hello" {
		t.Fatalf("Value = %q", value)
	}
	if output.Decision.Action != ActionPass {
		t.Fatalf("Decision = %+v", output.Decision)
	}
}

func TestPipeline_GuardOutputBlocksTechnicalPayload(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline(
		WithUserChannel[string](),
		WithUserChannelFallback[string]("blocked"),
		WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "classifier",
				PayloadKind: PayloadTechnicalPayload,
			}, ControlSpec{Action: ActionPass}), nil
		})),
	)

	// Act.
	output, err := pipeline.GuardOutput(context.Background(), nil, `{"internal":true}`)

	// Assert.
	if err == nil {
		t.Fatal("expected blocked output")
	}
	var failure *PolicyFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected PolicyFailure, got %v", err)
	}
	if output.Deliverable {
		t.Fatalf("output should not be deliverable: %+v", output)
	}
	if output.Value != "" {
		t.Fatalf("Value = %q, want zero value for non-deliverable output", output.Value)
	}
	if output.Decision.PayloadKind != PayloadTechnicalPayload {
		t.Fatalf("Decision = %+v", output.Decision)
	}
	if !failure.Decision.IsTerminal() {
		t.Fatalf("PolicyFailure = %+v", failure)
	}
}

func TestPipeline_GuardOutputBlockDoesNotExposeRawValue(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline(WithFastPath(ValidatorFunc[string](
		func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:    ActionBlock,
				Validator: "blocker",
				Code:      "DENIED",
			}, ControlSpec{Action: ActionBlock}), nil
		},
	)))

	// Act.
	output, err := pipeline.GuardOutput(context.Background(), nil, "unsafe raw text")

	// Assert.
	if err == nil {
		t.Fatal("expected block")
	}
	if output.Value != "" {
		t.Fatalf("Value = %q, want zero value", output.Value)
	}
	if _, ok := output.DeliverableValue(); ok {
		t.Fatal("blocked output should not be deliverable")
	}
}

func TestPipeline_GuardDeliveryBlocksStructuredSafeText(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline(WithFastPath(ValidatorFunc[string](
		func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "declared-safe",
				PayloadKind: PayloadSafeUserText,
			}, ControlSpec{Action: ActionPass}), nil
		},
	)))

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external"),
		`{"internal":true}`,
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	var failure *PolicyFailure
	if !errors.As(err, &failure) {
		t.Fatalf("expected PolicyFailure, got %v", err)
	}
	if output.Deliverable {
		t.Fatalf("output should not be deliverable: %+v", output)
	}
	if output.Kind != PayloadTechnicalPayload || output.Decision.PayloadKind != PayloadTechnicalPayload {
		t.Fatalf("output = %+v", output)
	}
	if failure.Decision.Validator != defaultDeliveryPolicyValidator {
		t.Fatalf("failure decision = %+v", failure.Decision)
	}
}

func TestPipeline_GuardDeliveryBlocksDefinedStringJSON(t *testing.T) {
	t.Parallel()
	// Arrange.
	type reply string
	pipeline := NewPipeline[reply]()

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external"),
		reply(`{"internal":true}`),
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	if output.Deliverable {
		t.Fatalf("output should not be deliverable: %+v", output)
	}
	if output.Kind != PayloadTechnicalPayload || output.Decision.PayloadKind != PayloadTechnicalPayload {
		t.Fatalf("output = %+v", output)
	}
}

func TestPipeline_GuardDeliveryBlocksStructuredMap(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline[map[string]any]()

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external"),
		map[string]any{"internal": true},
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	if output.Deliverable {
		t.Fatalf("output should not be deliverable: %+v", output)
	}
	if output.Kind != PayloadTechnicalPayload || output.Decision.PayloadKind != PayloadTechnicalPayload {
		t.Fatalf("output = %+v", output)
	}
}

func TestPipeline_GuardDeliveryBlocksStructuredSlice(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline[[]string]()

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external"),
		[]string{"internal"},
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	if output.Deliverable {
		t.Fatalf("output should not be deliverable: %+v", output)
	}
	if output.Kind != PayloadTechnicalPayload || output.Decision.PayloadKind != PayloadTechnicalPayload {
		t.Fatalf("output = %+v", output)
	}
}

func TestPipeline_GuardDeliveryBlocksStructuredStruct(t *testing.T) {
	t.Parallel()
	// Arrange.
	type deliveryEnvelope struct {
		Internal bool
	}
	pipeline := NewPipeline[deliveryEnvelope]()

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external"),
		deliveryEnvelope{Internal: true},
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	if output.Deliverable {
		t.Fatalf("output should not be deliverable: %+v", output)
	}
	if output.Kind != PayloadTechnicalPayload || output.Decision.PayloadKind != PayloadTechnicalPayload {
		t.Fatalf("output = %+v", output)
	}
}

func TestPipeline_GuardDeliveryBlocksStructuredPointer(t *testing.T) {
	t.Parallel()
	// Arrange.
	type deliveryEnvelope struct {
		Internal bool
	}
	pipeline := NewPipeline[*deliveryEnvelope]()

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external"),
		&deliveryEnvelope{Internal: true},
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	if output.Deliverable {
		t.Fatalf("output should not be deliverable: %+v", output)
	}
	if output.Kind != PayloadTechnicalPayload || output.Decision.PayloadKind != PayloadTechnicalPayload {
		t.Fatalf("output = %+v", output)
	}
}

func TestPipeline_GuardDeliveryBlocksNilStructuredPointer(t *testing.T) {
	t.Parallel()
	// Arrange.
	type deliveryEnvelope struct {
		Internal bool
	}
	pipeline := NewPipeline[*deliveryEnvelope]()
	var outputValue *deliveryEnvelope

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external"),
		outputValue,
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	if output.Deliverable {
		t.Fatalf("output should not be deliverable: %+v", output)
	}
	if output.Kind != PayloadTechnicalPayload || output.Decision.PayloadKind != PayloadTechnicalPayload {
		t.Fatalf("output = %+v", output)
	}
	if output.Value != nil {
		t.Fatalf("Value = %#v", output.Value)
	}
}

func TestPipeline_GuardDeliveryUsesTypedFallback(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline(WithFastPath(ValidatorFunc[string](
		func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "classifier",
				PayloadKind: PayloadTechnicalPayload,
			}, ControlSpec{Action: ActionPass}), nil
		},
	)))

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external", WithDeliveryFallback("safe fallback")),
		`{"internal":true}`,
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	if !output.Deliverable || !output.Fallback {
		t.Fatalf("output should deliver fallback: %+v", output)
	}
	value, ok := output.DeliverableValue()
	if !ok || value != "safe fallback" {
		t.Fatalf("deliverable value = %q, %v", value, ok)
	}
	if output.Channel != "external" {
		t.Fatalf("Channel = %q", output.Channel)
	}
}

func TestPipeline_GuardDeliveryBlocksNilStructuredPointerFallback(t *testing.T) {
	t.Parallel()
	// Arrange.
	type deliveryEnvelope struct {
		Internal bool
	}
	var fallback *deliveryEnvelope
	pipeline := NewPipeline(WithFastPath(ValidatorFunc[*deliveryEnvelope](
		func(_ context.Context, input *deliveryEnvelope) (*deliveryEnvelope, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "classifier",
				PayloadKind: PayloadTechnicalPayload,
			}, ControlSpec{Action: ActionPass}), nil
		},
	)))

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external", WithDeliveryFallback(fallback)),
		&deliveryEnvelope{Internal: true},
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	if output.Deliverable || output.Fallback {
		t.Fatalf("nil structured pointer fallback must not be deliverable: %+v", output)
	}
	if output.Value != nil {
		t.Fatalf("Value = %#v", output.Value)
	}
}

func TestPipeline_GuardDeliveryBlocksStructuredFallback(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline(WithFastPath(ValidatorFunc[map[string]any](
		func(_ context.Context, input map[string]any) (map[string]any, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "classifier",
				PayloadKind: PayloadTechnicalPayload,
			}, ControlSpec{Action: ActionPass}), nil
		},
	)))

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external", WithDeliveryFallback(map[string]any{"safe": false})),
		map[string]any{"internal": true},
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	if output.Deliverable || output.Fallback {
		t.Fatalf("structured fallback must not be deliverable: %+v", output)
	}
	if output.Value != nil {
		t.Fatalf("Value = %#v", output.Value)
	}
}

func TestPipeline_GuardDeliveryBlocksJSONFallback(t *testing.T) {
	t.Parallel()
	// Arrange.
	pipeline := NewPipeline(WithFastPath(ValidatorFunc[string](
		func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "classifier",
				PayloadKind: PayloadTechnicalPayload,
			}, ControlSpec{Action: ActionPass}), nil
		},
	)))

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		nil,
		NewDeliveryPolicy("external", WithDeliveryFallback(`{"fallback":true}`)),
		`{"internal":true}`,
	)

	// Assert.
	if err == nil {
		t.Fatal("expected delivery block")
	}
	if output.Deliverable || output.Fallback {
		t.Fatalf("JSON fallback must not be deliverable: %+v", output)
	}
	if output.Value != "" {
		t.Fatalf("Value = %q", output.Value)
	}
}

func TestPipeline_GuardDeliveryRunErrorIsNotDeliverable(t *testing.T) {
	t.Parallel()
	// Arrange.
	roleKey := NewScopeKey[string]("role")
	pipeline := NewPipeline(
		WithPolicyValidators(NewTypedAttributePresent[string, string](roleKey)),
	)

	// Act.
	output, err := pipeline.GuardDelivery(
		context.Background(),
		MapScope{},
		NewDeliveryPolicy("external", WithDeliveryFallback("safe fallback")),
		"hello",
	)

	// Assert.
	if !errors.Is(err, ErrScopeIncomplete) {
		t.Fatalf("err = %v", err)
	}
	if output.Deliverable || output.Fallback {
		t.Fatalf("output must not be deliverable on run error: %+v", output)
	}
	if output.Value != "" {
		t.Fatalf("Value = %q", output.Value)
	}
}
