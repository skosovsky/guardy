package guardy

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

//nolint:gocognit // Matrix checks normal/fallback timeout causes, completed kind and zero delivery.
func TestStreamCancellationRetainsCompletedKindAndIndependentCause(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		for _, independent := range []bool{false, true} {
			name := "normal/cancel"
			if fallback {
				name = "fallback/cancel"
			}
			if independent {
				name += "/independent-error"
			}
			t.Run(name, func(t *testing.T) {
				// Arrange.
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cause := errors.New("private independent detector failure")
				caps := StreamCapabilities{Unit: true, Final: true}
				first := WithStreamingCapabilities(
					ValidatorFunc[string](func(_ context.Context, v string) (string, *Report, error) {
						if v == "deny\n" {
							return v, &Report{Action: ActionBlock}, nil
						}
						return v, &Report{Action: ActionPass, PayloadKind: PayloadTechnicalPayload}, nil
					}),
					caps,
				)
				second := WithStreamingCapabilities(
					ValidatorFunc[string](func(_ context.Context, v string) (string, *Report, error) {
						cancel()
						if independent {
							return "failed output", &Report{PayloadKind: PayloadSafeUserText}, cause
						}
						return v, &Report{Action: ActionPass, PayloadKind: PayloadSafeUserText}, nil
					}),
					caps,
				)
				cfg := testStreamConfig(MustNewPipeline(WithSequential(first, second)))
				cfg.Profile = ReleaseValidatedUnits
				cfg.Delivery = NewUserTextPolicy(
					"internal",
					WithDeliveryAllowedKinds(PayloadTechnicalPayload),
					WithDeliveryFallback("hello\n"),
				)
				var sink bytes.Buffer
				stream, err := CompileStream(&sink, cfg)
				if err != nil {
					t.Fatal(err)
				}
				// Act.
				var outcome StreamOutcome
				if fallback {
					_, err = stream.Write([]byte("deny\n"))
					if !errors.Is(err, ErrBlocked) {
						t.Fatal(err)
					}
					outcome, err = stream.DeliverFallback(ctx)
				} else {
					_, err = stream.WriteContext(ctx, []byte("hello\n"))
					outcome = stream.Outcome()
				}
				// Assert.
				var release *ReleaseError
				var failure *PolicyFailure
				if !errors.As(err, &release) || !errors.As(err, &failure) || !errors.Is(err, context.Canceled) ||
					outcome.Category != StreamTimeout || outcome.Decision.PayloadKind != PayloadTechnicalPayload ||
					!outcome.Decision.IsSystemFault() || release.Outcome.Decision != outcome.Decision ||
					release.Failure.Decision != outcome.Decision || failure.Decision.PayloadKind != PayloadTechnicalPayload || sink.Len() != 0 {
					t.Fatalf(
						"outcome=%+v release=%+v failure=%+v err=%v sink=%q",
						outcome,
						release,
						failure,
						err,
						sink.String(),
					)
				}
				if independent && !errors.Is(err, cause) {
					t.Fatal("independent detector cause lost")
				}
			})
		}
	}
}

func TestStreamOutputLimitRetainsCheckedKind(t *testing.T) {
	// Arrange.
	rule := WithStreamingCapabilities(ValidatorFunc[string](func(_ context.Context, v string) (string, *Report, error) {
		return v, &Report{PayloadKind: PayloadTechnicalPayload}, nil
	}), StreamCapabilities{Unit: true, Final: true})
	cfg := testStreamConfig(MustNewPipeline(WithSequential(rule)))
	cfg.Profile = ReleaseValidatedUnits
	cfg.MaxOutputBytes = 1
	cfg.Delivery = NewUserTextPolicy("internal", WithDeliveryAllowedKinds(PayloadTechnicalPayload))
	var sink bytes.Buffer
	stream, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	_, err = stream.Write([]byte("hello\n"))
	outcome := stream.Outcome()
	// Assert.
	var release *ReleaseError
	var failure *PolicyFailure
	if !errors.As(err, &release) || !errors.As(err, &failure) || outcome.Category != StreamLimit ||
		!outcome.Decision.IsSystemFault() || outcome.Decision.PayloadKind != PayloadTechnicalPayload ||
		release.Failure.Decision != outcome.Decision || failure.Decision != outcome.Decision || sink.Len() != 0 {
		t.Fatalf("outcome=%+v failure=%+v err=%v sink=%q", outcome, failure, err, sink.String())
	}
}

func TestStreamTimeoutOutranksCompletedPolicyDecision(t *testing.T) {
	for _, disposition := range []FailureDisposition{DispositionNone, DispositionTerminalDeny, DispositionRetryableCorrection} {
		for _, fallback := range []bool{false, true} {
			// Arrange: context can expire just after a checked policy decision returns.
			decision := DecisionFromReport(&Report{PayloadKind: PayloadTechnicalPayload})
			decision.Disposition = disposition
			cause := errors.Join(&PolicyFailure{Decision: decision, Cause: ErrBlocked}, context.Canceled)
			stream := new(StreamProcessor)
			// Act.
			var outcome StreamOutcome
			var err error
			if fallback {
				outcome, err = stream.fallbackFailure(StreamOutcome{Decision: decision}, StreamTimeout, cause)
			} else {
				err = stream.fail(StreamTimeout, cause, decision)
				outcome = stream.Outcome()
			}
			// Assert.
			var failure *PolicyFailure
			if outcome.Category != StreamTimeout || !outcome.Decision.IsSystemFault() {
				t.Fatalf("timeout outcome=%+v", outcome)
			}
			if outcome.Decision.PayloadKind != PayloadTechnicalPayload {
				t.Fatalf("completed classification lost: %+v", outcome)
			}
			if !errors.As(err, &failure) || failure.Decision != outcome.Decision {
				t.Fatalf("failure=%+v outcome=%+v err=%v", failure, outcome, err)
			}
			if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrBlocked) {
				t.Fatalf("timeout causes lost: %v", err)
			}
		}
	}
}
