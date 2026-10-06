package guardy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
)

func TestCompletedObservationsSnapshotAndJoinedCauses(t *testing.T) {
	t.Parallel()
	// Arrange.
	first, second := errors.New("private one"), errors.New("private two")
	report := &Report{
		Action:      ActionRedact,
		PayloadKind: PayloadInternalControlSignal,
		MutatedText: "private value",
		Code:        "completed",
	}
	encoded := WithCompletedObservations(first, report)
	report.PayloadKind = PayloadSafeUserText
	report.Code = "mutated"
	joined := fmt.Errorf(
		"outer: %w",
		errors.Join(
			encoded,
			WithCompletedObservations(second, &Report{Action: ActionPass, PayloadKind: PayloadTechnicalPayload}),
		),
	)
	// Act.
	snapshot := CompletedReportFromError(encoded)
	all := CompletedReportFromError(joined)
	snapshot.PayloadKind = PayloadSafeUserText
	again := CompletedReportFromError(encoded)
	// Assert.
	if again.PayloadKind != PayloadInternalControlSignal || again.Code != "completed" || again.MutatedText != "" ||
		all.PayloadKind != PayloadTechnicalPayload ||
		!errors.Is(joined, first) ||
		!errors.Is(joined, second) ||
		encoded.Error() != ErrValidatorFailed.Error() {
		t.Fatalf("again=%+v all=%+v error=%v", again, all, encoded)
	}
	if WithCompletedObservations(nil, report) != nil || !errors.Is(WithCompletedObservations(first), first) ||
		CompletedReportFromError(first) != nil {
		t.Fatal("absent cause/evidence must not fabricate completed observations")
	}
}

func TestFailedReportsAreNotCompletedObservations(t *testing.T) {
	t.Parallel()
	for _, phase := range []ValidationPhase{ValidationPhaseFast, ValidationPhasePolicy, ValidationPhaseSlow} {
		for _, cancelOnReturn := range []bool{false, true} {
			t.Run(fmt.Sprint(phase, cancelOnReturn), func(t *testing.T) {
				t.Parallel()
				// Arrange.
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				cause := errors.New("failed callback")
				validate := func(_ context.Context, input string) (string, *Report, error) {
					rep := &Report{Action: ActionPass, Validator: "failed", PayloadKind: PayloadInternalControlSignal}
					if cancelOnReturn {
						cancel()
						return "failed " + input, rep, nil
					}
					return "failed " + input, rep, cause
				}
				var option PipelineOption[string]
				switch phase {
				case ValidationPhaseFast:
					option = WithFastPath(ValidatorFunc[string](validate))
				case ValidationPhaseSlow:
					option = WithSlowPath(ValidatorFunc[string](validate))
				case ValidationPhasePolicy:
					option = WithPolicyValidators(
						NewPolicyFuncWithScope(
							nil,
							func(ctx context.Context, input string, _ ExecutionScope) (string, *Report, error) {
								return validate(ctx, input)
							},
						),
					)
				}
				pipeline := NewPipeline(option)
				// Act.
				result, err := pipeline.Run(ctx, nil, "original")
				// Assert: first fault has no successful evidence; late nil-error cancellation is still failure.
				if err == nil || result.OutputKind != PayloadSafeUserText || !result.PolicyDecision().IsSystemFault() ||
					CompletedReportFromError(err) != nil {
					t.Fatalf("result=%+v evidence=%+v error=%v", result, CompletedReportFromError(err), err)
				}
				if cancelOnReturn && !errors.Is(err, context.Canceled) {
					t.Fatalf("lost cancellation: %v", err)
				}
				for _, rep := range result.Reports {
					if rep.Validator == "failed" {
						t.Fatalf("trusted failed report: %+v", rep)
					}
				}
			})
		}
	}
}

func TestNestedPipelineFaultCarriesOnlyCompletedHistory(t *testing.T) {
	t.Parallel()
	// Arrange.
	cause := errors.New("late failure")
	child := NewPipeline(WithFastPath(
		ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			return input, &Report{Action: ActionPass, PayloadKind: PayloadInternalControlSignal}, nil
		}),
		ValidatorFunc[string](func(context.Context, string) (string, *Report, error) {
			return "failed output", &Report{Action: ActionPass, PayloadKind: PayloadTechnicalPayload}, cause
		}),
	))
	parent := NewPipeline(
		WithFastPath(ValidatorFunc[string](func(ctx context.Context, input string) (string, *Report, error) {
			result, err := child.Run(ctx, nil, input)
			return result.Output, result.Decision(), err
		})),
	)
	// Act.
	result, err := parent.Run(t.Context(), nil, "original")
	// Assert: the returned raw child fault report is ignored; explicit child history is retained.
	if !errors.Is(err, cause) || result.OutputKind != PayloadInternalControlSignal ||
		!result.PolicyDecision().IsSystemFault() ||
		CompletedReportFromError(err).PayloadKind != PayloadInternalControlSignal {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestSlowSiblingCancellationRetainsCompletedEvidence(t *testing.T) {
	t.Parallel()
	// Arrange: deny occurs only after another sibling has a completed internal observation.
	completed := make(chan struct{})
	waiting := ValidatorFunc[string](func(ctx context.Context, input string) (string, *Report, error) {
		close(completed)
		<-ctx.Done()
		return input, &Report{
			PayloadKind: PayloadTechnicalPayload,
		}, WithCompletedObservations(
			ctx.Err(),
			&Report{Action: ActionPass, PayloadKind: PayloadInternalControlSignal},
		)
	})
	deny := ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
		<-completed
		return input, &Report{Action: ActionBlock}, nil
	})
	pipeline := NewPipeline(WithSlowPath(waiting, deny))
	// Act.
	result, err := pipeline.Run(t.Context(), nil, "original")
	// Assert: sibling-only cancellation adds no fault, but its prior classification survives.
	if err != nil || !result.PolicyDecision().IsTerminal() || result.OutputKind != PayloadInternalControlSignal {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestMappingFaultProjectsAttestedHistoryWithoutInjection(t *testing.T) {
	t.Parallel()
	for _, raw := range []bool{false, true} {
		t.Run(strconv.FormatBool(raw), func(t *testing.T) {
			t.Parallel()
			// Arrange.
			type document struct {
				Value string
				Raw   json.RawMessage
			}
			cause := errors.New("inner fault")
			encoded := WithCompletedObservations(
				cause,
				&Report{Action: ActionPass, PayloadKind: PayloadInternalControlSignal},
			)
			inner := ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
				return "failed " + input, &Report{Action: ActionRedact, PayloadKind: PayloadTechnicalPayload}, encoded
			})
			injected := 0
			var adapter Validator[document]
			if raw {
				adapter = MapJSONRawMessage(
					inner,
					func(d *document) json.RawMessage { return d.Raw },
					func(d *document, value json.RawMessage) *document { injected++; d.Raw = value; return d },
				)
			} else {
				adapter = Map(
					inner,
					func(d document) string { return d.Value },
					func(d document, value string) document { injected++; d.Value = value; return d },
				)
			}
			input := document{Value: "original", Raw: json.RawMessage(`{"value":"original"}`)}
			// Act.
			output, report, err := adapter.Validate(t.Context(), input)
			// Assert.
			if !errors.Is(err, cause) || injected != 0 || !reflect.DeepEqual(output, input) ||
				report.PayloadKind != PayloadInternalControlSignal ||
				report.MutatedText != "" {
				t.Fatalf("output=%+v report=%+v error=%v injected=%d", output, report, err, injected)
			}
		})
	}
}
