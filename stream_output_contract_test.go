package guardy

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func streamOutputRule(transform string) Validator[string] {
	return WithStreamingCapabilities(
		ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			if input == `{"deny":true}` {
				return input, &Report{Action: ActionBlock}, nil
			}
			if transform != "" && input == `{"source":true}` {
				return transform, &Report{Action: ActionRedact}, nil
			}
			return input, nil, nil
		}),
		StreamCapabilities{Unit: true, Final: true},
	)
}

func TestStreamJSONRepresentationAcrossDeliveryPaths(t *testing.T) {
	t.Parallel()
	for _, profile := range []ReleaseProfile{ReleaseValidatedUnits, ReleaseWholeResponse} {
		for _, path := range []string{"normal", "transformed", "fallback", "fallback_transformed"} {
			for _, tc := range []streamJSONCase{
				{"number", "123", false, true}, {"bool", "true", false, true}, {"null", "null", false, true}, {"string", `"hello"`, false, true},
				{"object", "{}", true, true}, {"array", "[]", true, true},
				{"spaced_object", " \t{}\r\n", true, true},
				{"spaced_array", "\n[] ", true, true},
				{"malformed", "{]", false, false},
				{"invalid_utf8", "{\xff}", false, false},
			} {
				t.Run(string(profile)+"/"+path+"/"+tc.name, func(t *testing.T) {
					t.Parallel()
					assertStreamJSONPath(t, profile, path, tc)
				})
			}
		}
	}
}

func TestStreamOutputUnitBudgetBeforeWriter(t *testing.T) {
	t.Parallel()
	for _, profile := range []ReleaseProfile{ReleaseValidatedUnits, ReleaseBestEffort, ReleaseWholeResponse} {
		for _, fallback := range []bool{false, true} {
			name := string(profile) + map[bool]string{false: "/transformed", true: "/fallback_transformed"}[fallback]
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assertStreamOutputBudget(t, profile, fallback)
			})
		}
	}
}

func assertStreamOutputBudget(t *testing.T, profile ReleaseProfile, fallback bool) {
	t.Helper()
	// Arrange: a two-byte unit expands past the four-byte unit budget, below total output budget.
	rule := WithStreamingCapabilities(
		ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			if input == "no" {
				return input, &Report{Action: ActionBlock}, nil
			}
			return "0123456789", &Report{Action: ActionRedact}, nil
		}),
		StreamCapabilities{Unit: true, Partial: true, Final: true},
	)
	cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
	cfg.Profile = profile
	cfg.MaxUnitBytes, cfg.MaxPendingBytes = 4, 4
	cfg.MaxOutputBytes = 100
	input := "ok"
	if fallback {
		input = "no"
		cfg.Delivery.Fallback = "ok"
	}
	var sink bytes.Buffer
	stream, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	err = writePartition(t.Context(), stream, []string{input})
	if fallback {
		if !errors.Is(err, ErrBlocked) {
			t.Fatal(err)
		}
		_, err = stream.DeliverFallback(t.Context())
	}
	// Assert.
	if profile == ReleaseWholeResponse {
		if err != nil || sink.String() != "0123456789" {
			t.Fatalf("whole output=%q error=%v", sink.String(), err)
		}
	} else if !errors.Is(err, ErrStreamUnitLimit) || sink.Len() != 0 {
		t.Fatalf("output=%q error=%v", sink.String(), err)
	}
}

func TestStreamFaultCannotActivateFallback(t *testing.T) {
	t.Parallel()
	for _, reportOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "report_fault"}[reportOnly], func(t *testing.T) {
			t.Parallel()
			// Arrange.
			cause := errors.New("detector failure")
			rule := WithStreamingCapabilities(
				ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
					if reportOnly {
						return input, &Report{Disposition: DispositionSystemFault}, nil
					}
					return input, nil, cause
				}),
				StreamCapabilities{Unit: true},
			)
			cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
			cfg.Profile, cfg.JSONValues = ReleaseValidatedUnits, true
			cfg.MaxPendingBytes = cfg.MaxUnitBytes + 1
			cfg.Delivery = NewUserTextPolicy(
				"internal",
				WithDeliveryAllowedKinds(PayloadTechnicalPayload),
				WithDeliveryFallback("{}"),
			)
			var sink bytes.Buffer
			stream, err := CompileStream(&sink, cfg)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			err = writePartition(t.Context(), stream, []string{"{}"})
			_, fallbackErr := stream.DeliverFallback(t.Context())
			// Assert.
			if err == nil || stream.Outcome().Category != StreamFault || fallbackErr == nil || sink.Len() != 0 {
				t.Fatalf("outcome=%+v err=%v fallback=%v output=%q", stream.Outcome(), err, fallbackErr, sink.String())
			}
		})
	}
}

func TestStreamJSONExpandedUnitBudget(t *testing.T) {
	t.Parallel()
	for _, fallback := range []bool{false, true} {
		for _, output := range []string{`{"v":1}`, `{"vv":1}`} {
			t.Run(map[bool]string{false: "normal", true: "fallback"}[fallback]+output, func(t *testing.T) {
				t.Parallel()
				assertStreamJSONUnitBudget(t, fallback, output)
			})
		}
	}
}

func TestStreamTransformedNewlineMustRemainOneUnit(t *testing.T) {
	t.Parallel()
	// Arrange.
	rule := WithStreamingCapabilities(ValidatorFunc[string](func(context.Context, string) (string, *Report, error) {
		return "a\nb\n", &Report{Action: ActionRedact}, nil
	}), StreamCapabilities{Unit: true})
	cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
	cfg.Profile = ReleaseValidatedUnits
	var sink bytes.Buffer
	stream, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	err = writePartition(t.Context(), stream, []string{"source\n"})
	// Assert.
	if !errors.Is(err, ErrInvalidStreamUnit) || stream.Outcome().Category != StreamFault || sink.Len() != 0 {
		t.Fatalf("outcome=%+v err=%v sink=%q", stream.Outcome(), err, sink.String())
	}
}

type streamJSONCase struct {
	name, value      string
	composite, valid bool
}

func assertStreamJSONPath(t *testing.T, profile ReleaseProfile, path string, tc streamJSONCase) {
	t.Helper()
	// Arrange: destination accepts both text and technical kinds; framing remains independent.
	input, transform := tc.value, ""
	if path == "transformed" {
		input, transform = `{"source":true}`, tc.value
	}
	if path == "fallback_transformed" {
		transform = tc.value
	}
	cfg := testStreamConfig(NewPipeline(WithFastPath(streamOutputRule(transform))))
	cfg.Profile, cfg.JSONValues = profile, true
	cfg.MaxUnitBytes, cfg.MaxPendingBytes = 64, 65
	cfg.Delivery = NewUserTextPolicy(
		"internal",
		WithDeliveryAllowedKinds(PayloadSafeUserText, PayloadTechnicalPayload),
	)
	if path == "fallback" {
		input = `{"deny":true}`
		cfg.Delivery.Fallback = tc.value
	}
	if path == "fallback_transformed" {
		input = `{"deny":true}`
		cfg.Delivery.Fallback = `{"source":true}`
	}
	var sink bytes.Buffer
	stream, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	err = writePartition(t.Context(), stream, []string{input})
	if path == "fallback" || path == "fallback_transformed" {
		if !errors.Is(err, ErrBlocked) {
			t.Fatalf("expected initial deny: %v", err)
		}
		_, err = stream.DeliverFallback(t.Context())
	}
	// Assert: only unit JSON restricts the top-level representation to object/array.
	allowed := tc.valid && (profile == ReleaseWholeResponse || tc.composite)
	if allowed {
		if err != nil || sink.String() != tc.value {
			t.Fatalf("output=%q error=%v", sink.String(), err)
		}
	} else if err == nil || sink.Len() != 0 {
		t.Fatalf("scalar output=%q error=%v", sink.String(), err)
	}
}

func assertStreamJSONUnitBudget(t *testing.T, fallback bool, output string) {
	t.Helper()
	// Arrange.
	rule := WithStreamingCapabilities(
		ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			if input == "[]" {
				return input, &Report{Action: ActionBlock}, nil
			}
			return output, &Report{Action: ActionRedact}, nil
		}),
		StreamCapabilities{Unit: true},
	)
	cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
	cfg.Profile, cfg.JSONValues = ReleaseValidatedUnits, true
	cfg.MaxUnitBytes, cfg.MaxPendingBytes = 7, 8
	cfg.Delivery = NewUserTextPolicy("internal", WithDeliveryAllowedKinds(PayloadTechnicalPayload))
	input := "{}"
	if fallback {
		input = "[]"
		cfg.Delivery.Fallback = "{}"
	}
	var sink bytes.Buffer
	stream, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	err = writePartition(t.Context(), stream, []string{input})
	if fallback {
		if !errors.Is(err, ErrBlocked) {
			t.Fatal(err)
		}
		_, err = stream.DeliverFallback(t.Context())
	}
	// Assert.
	if len(output) == 7 {
		if err != nil || sink.String() != output {
			t.Fatalf("output=%q err=%v", sink.String(), err)
		}
	} else if !errors.Is(err, ErrStreamUnitLimit) || sink.Len() != 0 {
		t.Fatalf("output=%q err=%v", sink.String(), err)
	}
}

func TestStreamFallbackInputBudgetBeforeValidation(t *testing.T) {
	t.Parallel()
	for _, jsonUnits := range []bool{false, true} {
		t.Run(map[bool]string{false: "newline", true: "json"}[jsonUnits], func(t *testing.T) {
			t.Parallel()
			// Arrange.
			calls := 0
			rule := WithStreamingCapabilities(
				ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
					calls++
					return input, &Report{Action: ActionBlock}, nil
				}),
				StreamCapabilities{Unit: true},
			)
			cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
			cfg.Profile, cfg.JSONValues = ReleaseValidatedUnits, jsonUnits
			cfg.MaxUnitBytes, cfg.MaxPendingBytes = 4, 5
			cfg.Delivery = NewUserTextPolicy(
				"internal",
				WithDeliveryAllowedKinds(PayloadSafeUserText, PayloadTechnicalPayload),
			)
			cfg.Delivery.Fallback = "12345"
			input := "no"
			if jsonUnits {
				input = "[]"
				cfg.Delivery.Fallback = `{"v":1}`
			}
			var sink bytes.Buffer
			stream, err := CompileStream(&sink, cfg)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			denied := writePartition(t.Context(), stream, []string{input})
			outcome, err := stream.DeliverFallback(t.Context())
			// Assert: the oversized fallback never reaches validation or transport.
			if !errors.Is(denied, ErrBlocked) || !errors.Is(err, ErrStreamUnitLimit) ||
				outcome.Category != StreamLimit ||
				calls != 1 ||
				sink.Len() != 0 {
				t.Fatalf("calls=%d outcome=%+v error=%v denied=%v sink=%q", calls, outcome, err, denied, sink.String())
			}
		})
	}
}
