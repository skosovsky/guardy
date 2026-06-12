package guardy

import (
	"context"
	"testing"
)

type dtoOutput struct {
	Text string
}

func TestAggregatePayloadKind_Priority(t *testing.T) {
	t.Parallel()
	reports := []Report{
		{Action: ActionPass, PayloadKind: PayloadSafeUserText},
		{Action: ActionPass, PayloadKind: PayloadTechnicalPayload},
	}
	if got := AggregatePayloadKind(reports); got != PayloadTechnicalPayload {
		t.Fatalf("got %v", got)
	}
}

func TestAggregatePayloadKind_InternalVsTechnical(t *testing.T) {
	t.Parallel()
	reports := []Report{
		{Action: ActionPass, PayloadKind: PayloadInternalControlSignal},
		{Action: ActionPass, PayloadKind: PayloadTechnicalPayload},
	}
	if got := AggregatePayloadKind(reports); got != PayloadTechnicalPayload {
		t.Fatalf("got %v", got)
	}
}

func TestPipeline_UserChannel_InternalControlSignal(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithUserChannel[string](),
		WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "classifier",
				PayloadKind: PayloadInternalControlSignal,
			}, ControlSpec{Action: ActionPass}), nil
		})),
	)
	result, err := p.Run(context.Background(), nil, "routing hint")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != ActionBlock {
		t.Fatalf("action = %v", result.Decision().Action)
	}
}

func TestPipeline_UserChannelBlocksTechnicalPayload(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
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
	result, err := p.Run(context.Background(), nil, `{"tool":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != ActionBlock {
		t.Fatalf("action = %v", result.Decision().Action)
	}
	if result.OutputKind != PayloadTechnicalPayload {
		t.Fatalf("OutputKind = %v", result.OutputKind)
	}
	if result.Output != "" {
		t.Fatalf("Output = %q, want empty after user channel block", result.Output)
	}
}

func TestPipeline_UserChannelAllowsSafeText(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithUserChannel[string](),
		WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			return input, &Report{Action: ActionPass, Validator: "pass"}, nil
		})),
	)
	result, err := p.Run(context.Background(), nil, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != ActionPass {
		t.Fatalf("action = %v", result.Decision().Action)
	}
	if result.OutputKind != PayloadSafeUserText {
		t.Fatalf("OutputKind = %v", result.OutputKind)
	}
}

func TestPipeline_UserChannel_GenericDTO(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithUserChannel[dtoOutput](),
		WithFastPath(ValidatorFunc[dtoOutput](func(_ context.Context, input dtoOutput) (dtoOutput, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "classifier",
				PayloadKind: PayloadTechnicalPayload,
			}, ControlSpec{Action: ActionPass}), nil
		})),
	)
	result, err := p.Run(context.Background(), nil, dtoOutput{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != ActionBlock {
		t.Fatalf("action = %v", result.Decision().Action)
	}
	if result.Output != (dtoOutput{}) {
		t.Fatalf("Output = %+v, want zero after user channel block", result.Output)
	}
}

func TestPipeline_UserChannel_ExplicitBlockScrubsOutput(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithUserChannel[string](),
		WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:    ActionBlock,
				Validator: "wordlist",
				Code:      "WORDLIST_BLOCK",
			}, ControlSpec{Action: ActionBlock}), nil
		})),
	)
	result, err := p.Run(context.Background(), nil, "forbidden payload")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != ActionBlock {
		t.Fatalf("action = %v", result.Decision().Action)
	}
	if result.Output != "" {
		t.Fatalf("Output = %q, want empty with user channel + explicit block", result.Output)
	}
}

func TestPipeline_UserChannelFallback_PublicMessage(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithUserChannel[string](),
		WithUserChannelFallback[string]("blocked for user"),
		WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:      ActionPass,
				Validator:   "classifier",
				PayloadKind: PayloadTechnicalPayload,
			}, ControlSpec{Action: ActionPass}), nil
		})),
	)
	result, err := p.Run(context.Background(), nil, `{"tool":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().PublicMessage() != "blocked for user" {
		t.Fatalf("PublicMessage = %q", result.Decision().PublicMessage())
	}
}

func TestPipeline_UserChannel_TerminalRetryScrubsOutput(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithUserChannel[string](),
		WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			return input, FinishReport(&Report{
				Action:    ActionRetry,
				Validator: ActionRetry.String(),
				Retryable: false,
				Reason:    "terminal retry",
			}, ControlSpec{Action: ActionRetry, Retryable: new(false)}), nil
		})),
	)
	result, err := p.Run(context.Background(), nil, "payload")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", result.Decision().Disposition)
	}
	if result.Output != "" {
		t.Fatalf("Output = %q, want empty with user channel + terminal retry", result.Output)
	}
}
