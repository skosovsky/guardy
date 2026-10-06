package guardy

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFinalOnlyRuleRejectedForBoundedUnits(t *testing.T) {
	// Arrange.
	calls := 0
	rule := WithStreamingCapabilities(
		ValidatorFunc[string](
			func(_ context.Context, value string) (string, *Report, error) { calls++; return value, nil, nil },
		),
		StreamCapabilities{Final: true},
	)
	cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
	cfg.Profile = ReleaseValidatedUnits
	var sink bytes.Buffer
	// Act.
	_, err := CompileStream(&sink, cfg)
	// Assert.
	var release *ReleaseError
	if !errors.As(err, &release) || release.Outcome.Category != StreamUnsupported || sink.Len() != 0 || calls != 0 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestTrustedFinalJSONRequiredFieldFailure(t *testing.T) {
	// Arrange: valid framing is not an executable schema guarantee.
	calls := 0
	rule := WithStreamingCapabilities(
		ValidatorFunc[string](func(ctx context.Context, value string) (string, *Report, error) {
			calls++
			if StreamValidationStage(ctx) != StreamFinal {
				t.Fatal("final check ran at untrusted stage")
			}
			if !strings.Contains(value, `"required"`) {
				return value, &Report{Action: ActionBlock, Code: CodeJSONSchemaInvalid}, nil
			}
			return value, nil, nil
		}),
		StreamCapabilities{Final: true},
	)
	cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
	cfg.JSONValues = true
	var sink bytes.Buffer
	stream, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	_, err = stream.Write([]byte(`{"optional":true}`))
	if err != nil || calls != 0 || sink.Len() != 0 {
		t.Fatalf("premature validation/release: %d %v", calls, err)
	}
	outcome, err := stream.Complete(context.Background())
	// Assert.
	if err == nil || outcome.Category != StreamBlocked || outcome.Decision.Code != CodeJSONSchemaInvalid ||
		sink.Len() != 0 ||
		calls != 1 {
		t.Fatalf("%+v %v calls=%d", outcome, err, calls)
	}
}

func streamFaultPipeline(phase, mode string, cancel context.CancelFunc) *Pipeline[string] {
	callback := func(_ context.Context, value string) (string, *Report, error) {
		switch mode {
		case "error":
			return value, nil, errors.New("detector unavailable")
		case "cancel":
			cancel()
			return value, nil, nil
		default:
			return value, &Report{
				Action:      ActionPass,
				Disposition: DispositionSystemFault,
				Code:        "REPORT_ONLY_FAULT",
			}, nil
		}
	}
	rule := ValidatorFunc[string](callback)
	switch phase {
	case "fast":
		return NewPipeline(WithFastPath(rule))
	case "slow":
		return NewPipeline(WithSlowPath(rule))
	default:
		policy := NewPolicyFuncWithScope(
			nil,
			func(ctx context.Context, value string, _ ExecutionScope) (string, *Report, error) {
				return callback(ctx, value)
			},
		)
		return NewPipeline(WithPolicyValidators(policy))
	}
}

func TestStreamFaultAndCancellationAcrossAllPhases(t *testing.T) {
	for _, phase := range []string{"fast", "policy", "slow"} {
		for _, mode := range []string{"report", "error", "cancel"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				// Arrange.
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				cfg := testStreamConfig(streamFaultPipeline(phase, mode, cancel))
				cfg.Delivery.Fallback = "must-not-mask-fault"
				var sink bytes.Buffer
				stream, err := CompileStream(&sink, cfg)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = stream.Write([]byte("SECRET"))
				// Act.
				outcome, err := stream.Complete(ctx)
				_, late := stream.Write([]byte("late"))
				again, repeated := stream.Complete(context.Background())
				_, fallback := stream.DeliverFallback(context.Background())
				// Assert.
				var failure *PolicyFailure
				want := StreamFault
				if mode == "cancel" {
					want = StreamTimeout
				}
				if !errors.As(err, &failure) || !failure.Decision.IsSystemFault() || sink.Len() != 0 ||
					outcome.Category != want ||
					!errors.Is(late, err) ||
					!errors.Is(repeated, err) ||
					outcome != again ||
					fallback == nil {
					t.Fatalf("%+v %v late=%v repeated=%v fallback=%v", outcome, err, late, repeated, fallback)
				}
			})
		}
	}
}

func TestStreamFallbackDistinctPositiveNegativeFaultOutcomes(t *testing.T) {
	for _, candidate := range []string{"safe notice", "secret fallback", "fault fallback"} {
		t.Run(candidate, func(t *testing.T) {
			// Arrange.
			rule := ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
				if strings.Contains(value, "secret") {
					return value, &Report{Action: ActionBlock}, nil
				}
				if strings.Contains(value, "fault") {
					return value, nil, errors.New("fallback detector unavailable")
				}
				return value, nil, nil
			})
			cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
			cfg.Delivery.Fallback = candidate
			var events []StreamEvent
			cfg.Observer = func(event StreamEvent) { events = append(events, event) }
			var sink bytes.Buffer
			stream, err := CompileStream(&sink, cfg)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = stream.Write([]byte("secret original"))
			original, denied := stream.Complete(context.Background())
			// Act.
			fallback, err := stream.DeliverFallback(context.Background())
			_, repeated := stream.DeliverFallback(context.Background())
			// Assert: fallback never changes the original terminal outcome or replays.
			if denied == nil || repeated == nil || stream.Outcome() != original || !fallback.Fallback ||
				!fallback.Terminal ||
				original.ReleasedBytes != 0 {
				t.Fatalf("original=%+v fallback=%+v err=%v", original, fallback, err)
			}
			assertCheckedFallback(t, candidate, fallback, err, sink.String())
			if len(events) != 2 || !events[1].Outcome.Fallback || events[1].Outcome.Category != fallback.Category {
				t.Fatalf("events=%+v", events)
			}
		})
	}
}

func assertCheckedFallback(t *testing.T, candidate string, outcome StreamOutcome, err error, delivered string) {
	t.Helper()
	switch candidate {
	case "safe notice":
		if err != nil || outcome.Category != StreamSuccess || delivered != candidate ||
			outcome.ReleasedBytes != int64(len(candidate)) ||
			outcome.Sequence != 1 {
			t.Fatalf("%+v %v %q", outcome, err, delivered)
		}
	case "secret fallback":
		if err == nil || outcome.Category != StreamBlocked || delivered != "" {
			t.Fatalf("%+v %v %q", outcome, err, delivered)
		}
	default:
		if err == nil || outcome.Category != StreamFault || !outcome.Decision.IsSystemFault() || delivered != "" {
			t.Fatalf("%+v %v %q", outcome, err, delivered)
		}
	}
}

func TestLengthChangingRedactionEverySplitWholeAndUnits(t *testing.T) {
	for _, replacement := range []string{"X", "[long sanitized replacement 😊]"} {
		for _, profile := range []ReleaseProfile{ReleaseWholeResponse, ReleaseValidatedUnits} {
			input := "Привет secret!\n"
			for split := 0; split <= len(input); split++ {
				// Arrange: newline units are self-contained; no cross-unit guarantee.
				rule := WithStreamingCapabilities(
					ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
						return strings.ReplaceAll(value, "secret", replacement), &Report{Action: ActionRedact}, nil
					}),
					StreamCapabilities{Unit: true, Final: true},
				)
				cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
				cfg.Profile = profile
				var sink bytes.Buffer
				stream, err := CompileStream(&sink, cfg)
				if err != nil {
					t.Fatal(err)
				}
				// Act.
				_, first := stream.Write([]byte(input[:split]))
				_, second := stream.Write([]byte(input[split:]))
				_, final := stream.Complete(context.Background())
				// Assert.
				if first != nil || second != nil || final != nil ||
					sink.String() != strings.ReplaceAll(input, "secret", replacement) ||
					strings.Contains(sink.String(), "secret") {
					t.Fatalf(
						"profile=%s split=%d %v %v %v output=%q",
						profile,
						split,
						first,
						second,
						final,
						sink.String(),
					)
				}
			}
		}
	}
}

func TestJSONRedactionLengthChangesEverySplit(t *testing.T) {
	for _, replacement := range []string{"X", "[long safe replacement 😊]"} {
		for _, profile := range []ReleaseProfile{ReleaseWholeResponse, ReleaseValidatedUnits} {
			input := `{"text":"Привет secret"}`
			for split := 0; split <= len(input); split++ {
				// Arrange.
				rule := WithStreamingCapabilities(
					ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
						return strings.ReplaceAll(value, "secret", replacement), &Report{Action: ActionRedact}, nil
					}),
					StreamCapabilities{Unit: true, Final: true},
				)
				cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
				cfg.Profile, cfg.JSONValues = profile, true
				cfg.MaxPendingBytes = cfg.MaxUnitBytes + 1
				cfg.Delivery = NewUserTextPolicy("internal", WithDeliveryAllowedKinds(PayloadTechnicalPayload))
				var sink bytes.Buffer
				stream, err := CompileStream(&sink, cfg)
				if err != nil {
					t.Fatal(err)
				}
				// Act.
				_, first := stream.Write([]byte(input[:split]))
				_, second := stream.Write([]byte(input[split:]))
				_, final := stream.Complete(context.Background())
				// Assert: release also checks that redaction preserved JSON/UTF-8 framing.
				if first != nil || second != nil || final != nil ||
					sink.String() != strings.ReplaceAll(input, "secret", replacement) {
					t.Fatalf(
						"profile=%s split=%d %v %v %v output=%q",
						profile,
						split,
						first,
						second,
						final,
						sink.String(),
					)
				}
			}
		}
	}
}

func TestStreamIndependentByteLimits(t *testing.T) {
	for _, kind := range []string{"input", "pending", "unit", "json", "whitespace", "expansion"} {
		t.Run(kind, func(t *testing.T) {
			// Arrange: all other limits are large enough to isolate the named bound.
			cfg := testStreamConfig(NewPipeline[string]())
			input := strings.Repeat("x", 9)
			switch kind {
			case "input":
				cfg.MaxInputBytes = 8
			case "pending":
				cfg.MaxPendingBytes = 8
			case "unit":
				cfg.Profile, cfg.MaxUnitBytes = ReleaseValidatedUnits, 8
				input += "\n"
			case "json":
				cfg.Profile, cfg.JSONValues, cfg.MaxUnitBytes, cfg.MaxPendingBytes = ReleaseValidatedUnits, true, 16, 17
				input = `{"x":"` + strings.Repeat("x", 32) + `"}`
			case "whitespace":
				cfg.Profile, cfg.JSONValues, cfg.MaxUnitBytes, cfg.MaxPendingBytes = ReleaseValidatedUnits, true, 16, 17
				input = strings.Repeat(" ", 32)
			case "expansion":
				cfg.MaxOutputBytes = 8
				cfg.Pipeline = NewPipeline(
					WithFastPath(ValidatorFunc[string](func(_ context.Context, _ string) (string, *Report, error) {
						return strings.Repeat("😊", 3), &Report{Action: ActionRedact}, nil
					})),
				)
			}
			var sink bytes.Buffer
			stream, err := CompileStream(&sink, cfg)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			accepted, err := stream.Write([]byte(input))
			if err == nil {
				_, err = stream.Complete(context.Background())
			}
			outcome := stream.Outcome()
			// Assert.
			var release *ReleaseError
			if !errors.As(err, &release) || outcome.Category != StreamLimit || sink.Len() != 0 ||
				outcome.PeakPendingBytes > cfg.MaxPendingBytes ||
				!outcome.Terminal {
				t.Fatalf("%+v %v accepted=%d", outcome, err, accepted)
			}
			if kind == "input" && (accepted != 0 || outcome.ReceivedBytes != 0 || outcome.PeakPendingBytes != 0) {
				t.Fatal("input copied before input-limit check")
			}
		})
	}
}
