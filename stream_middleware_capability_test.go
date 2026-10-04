package guardy

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestStreamChecksCapabilitiesOfAppliedMiddleware(t *testing.T) {
	for _, phase := range []string{"fast", "policy", "slow"} {
		t.Run(phase, func(t *testing.T) {
			// Arrange: the underlying validator supports units, but the actually
			// applied middleware declares and executes only final checks.
			base := WithStreamingCapabilities(
				ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
					return value, &Report{Action: ActionBlock}, nil
				}),
				StreamCapabilities{Unit: true, Final: true},
			)
			var pipeline *Pipeline[string]
			switch phase {
			case "fast":
				pipeline = NewPipeline(WithFastPath(base))
			case "slow":
				pipeline = NewPipeline(WithSlowPath(base))
			default:
				policy := NewPolicyFuncWithScope(
					nil,
					func(ctx context.Context, value string, _ ExecutionScope) (string, *Report, error) {
						return base.Validate(ctx, value)
					},
				)
				pipeline = NewPipeline(
					WithPolicyValidators(
						WithStreamingPolicyCapabilities(policy, StreamCapabilities{Unit: true, Final: true}),
					),
				)
			}
			pipeline = pipeline.Use(func(next Validator[string]) Validator[string] {
				return WithStreamingCapabilities(
					ValidatorFunc[string](func(ctx context.Context, value string) (string, *Report, error) {
						if StreamValidationStage(ctx) == StreamFinal {
							return next.Validate(ctx, value)
						}
						return value, nil, nil
					}),
					StreamCapabilities{Final: true},
				)
			})
			cfg := testStreamConfig(pipeline)
			cfg.Profile = ReleaseValidatedUnits
			var sink bytes.Buffer
			// Act.
			_, err := CompileStream(&sink, cfg)
			// Assert: capabilities of the discarded base cannot authorize unit release.
			var release *ReleaseError
			if !errors.As(err, &release) || release.Outcome.Category != StreamUnsupported || sink.Len() != 0 {
				t.Fatalf("applied final-only middleware accepted: %v", err)
			}
		})
	}
}

func TestStreamMiddlewareCapabilityContract(t *testing.T) {
	for _, phase := range []string{"fast", "policy", "slow"} {
		for _, scenario := range []struct {
			name       string
			profile    ReleaseProfile
			declared   bool
			capability StreamCapabilities
			allowed    bool
		}{
			{"forwarding", ReleaseValidatedUnits, true, StreamCapabilities{Unit: true, Final: true}, true},
			{"undeclared", ReleaseValidatedUnits, false, StreamCapabilities{}, false},
			{"no-final", ReleaseWholeResponse, true, StreamCapabilities{Unit: true}, false},
			{"ordinary-final", ReleaseWholeResponse, false, StreamCapabilities{}, true},
		} {
			t.Run(phase+"/"+scenario.name, func(t *testing.T) {
				// Arrange.
				base := WithStreamingCapabilities(
					ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
						return value, nil, nil
					}),
					StreamCapabilities{Unit: true, Final: true},
				)
				pipeline := middlewareCapabilityPipeline(phase, base)
				pipeline = pipeline.Use(func(next Validator[string]) Validator[string] {
					wrapper := ValidatorFunc[string](next.Validate)
					if scenario.declared {
						return WithStreamingCapabilities(wrapper, scenario.capability)
					}
					return wrapper
				})
				cfg := testStreamConfig(pipeline)
				cfg.Profile = scenario.profile
				var sink bytes.Buffer
				// Act.
				stream, err := CompileStream(&sink, cfg)
				if err == nil {
					_, err = stream.Write([]byte("safe\n"))
					if err == nil {
						_, err = stream.Complete(context.Background())
					}
				}
				// Assert.
				if scenario.allowed {
					if err != nil || sink.String() != "safe\n" {
						t.Fatalf("supported middleware: output=%q error=%v", sink.String(), err)
					}
				} else {
					var release *ReleaseError
					if !errors.As(err, &release) || release.Outcome.Category != StreamUnsupported || sink.Len() != 0 {
						t.Fatalf("unsupported middleware: output=%q error=%v", sink.String(), err)
					}
				}
			})
		}
	}
}

func middlewareCapabilityPipeline(phase string, base Validator[string]) *Pipeline[string] {
	switch phase {
	case "fast":
		return NewPipeline(WithFastPath(base))
	case "slow":
		return NewPipeline(WithSlowPath(base))
	default:
		policy := NewPolicyFuncWithScope(
			nil,
			func(ctx context.Context, value string, _ ExecutionScope) (string, *Report, error) {
				return base.Validate(ctx, value)
			},
		)
		return NewPipeline(
			WithPolicyValidators(WithStreamingPolicyCapabilities(policy, StreamCapabilities{Unit: true, Final: true})),
		)
	}
}

func TestStreamRejectsHiddenIntermediateMiddleware(t *testing.T) {
	for _, phase := range []string{"fast", "policy", "slow"} {
		t.Run(phase, func(t *testing.T) {
			// Arrange: an outer forwarding wrapper cannot upgrade a final-only inner layer.
			base := WithStreamingCapabilities(
				ValidatorFunc[string](func(_ context.Context, value string) (string, *Report, error) {
					return value, &Report{Action: ActionBlock}, nil
				}),
				StreamCapabilities{Unit: true, Final: true},
			)
			outer := func(next Validator[string]) Validator[string] {
				return WithStreamingCapabilities(
					ValidatorFunc[string](next.Validate),
					StreamCapabilities{Unit: true, Final: true},
				)
			}
			inner := func(next Validator[string]) Validator[string] {
				return WithStreamingCapabilities(
					ValidatorFunc[string](func(ctx context.Context, value string) (string, *Report, error) {
						if StreamValidationStage(ctx) == StreamFinal {
							return next.Validate(ctx, value)
						}
						return value, nil, nil
					}),
					StreamCapabilities{Final: true},
				)
			}
			pipeline := middlewareCapabilityPipeline(phase, base).Use(outer, inner)
			cfg := testStreamConfig(pipeline)
			cfg.Profile = ReleaseValidatedUnits
			var sink bytes.Buffer
			// Act.
			_, err := CompileStream(&sink, cfg)
			// Assert.
			var release *ReleaseError
			if !errors.As(err, &release) || release.Outcome.Category != StreamUnsupported || sink.Len() != 0 {
				t.Fatalf("hidden intermediate capability accepted: output=%q error=%v", sink.String(), err)
			}
		})
	}
}
