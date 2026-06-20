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
