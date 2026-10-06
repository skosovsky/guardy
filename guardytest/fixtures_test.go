package guardytest_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	g "github.com/skosovsky/guardy"
	gt "github.com/skosovsky/guardy/guardytest"
)

func fixturePipeline(fixture gt.StringBoundaryFixture) *g.Pipeline[string] {
	rule := g.ValidatorFunc[string](func(_ context.Context, input string) (string, *g.Report, error) {
		if strings.Contains(input, "secret") || fixture.ClaimedTrusted != fixture.ConfirmedTrusted {
			return input, &g.Report{Action: g.ActionBlock}, nil
		}
		return input, nil, nil
	})
	return g.NewPipeline(g.WithFastPath(rule))
}

func TestReferenceBoundaryFixturesActualHandlerAndSink(t *testing.T) {
	gt.CheckBoundaryCases(
		t,
		gt.StringBoundaryCases(func(ctx context.Context, fixture gt.StringBoundaryFixture) gt.BoundaryObservation {
			// Arrange: synthetic policy is configured explicitly, not claimed as a detector.
			var observed gt.BoundaryObservation
			pipeline := fixturePipeline(fixture)
			handler := g.WrapInput(pipeline, nil, func(_ context.Context, value string) (string, error) {
				observed.HandlerCalls++
				if fixture.Replacement != "" {
					return fixture.Replacement, nil
				}
				return value, nil
			})
			// Act: actual pre-handler enforcement followed by the downstream content boundary.
			value, err := handler(ctx, fixture.Input)
			if err == nil {
				delivery, deliveryErr := pipeline.GuardDelivery(ctx, nil, g.NewUserTextPolicy("external"), value)
				err, observed.Decision = deliveryErr, delivery.Decision
				if projection, ok := delivery.Projection(); ok {
					observed.Delivered = projection.Value
				}
			}
			observed.Err = err
			if failure, ok := errors.AsType[*g.PolicyFailure](err); ok {
				observed.Decision = failure.Decision
			}
			return observed
		}),
	)
}

func TestReferenceSemanticFixturesActualBoundary(t *testing.T) {
	for _, fixture := range gt.ReferenceSemanticFixtures() {
		t.Run(fixture.Name, func(t *testing.T) {
			// Arrange.
			observations, calls := 0, 0
			validator, configErr := fixture.Validator()
			if configErr != nil {
				if validator != nil || !errors.Is(configErr, g.ErrConfiguration) ||
					fixture.Disposition != g.DispositionSystemFault {
					t.Fatalf("fixture config=%v expected=%v", configErr, fixture.Disposition)
				}
				return
			}
			pipeline := g.NewPipeline(g.WithSlowPath(validator), g.WithPipelineName[string](fixture.Identity),
				g.WithObserver[string](func(_ context.Context, event g.GuardEvent) {
					observations++
					if event.PipelineName != fixture.Identity {
						t.Fatal("wrong applied detector identity")
					}
				}))
			args := g.MustCompileArgs[map[string]string](
				pipeline,
				g.WithArgsConfigurationID[map[string]string](fixture.Identity),
			)
			handler := g.WrapArgs(
				args,
				nil,
				func(_ context.Context, _ map[string]string) (string, error) { calls++; return "ok", nil },
			)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			// Act.
			_, boundary, err := handler(ctx, `{"text":"hello"}`)
			// Assert.
			if boundary.ConfigurationID != fixture.Identity || boundary.Decision.Disposition != fixture.Disposition ||
				observations != fixture.Observations {
				t.Fatalf("boundary=%+v observations=%d err=%v", boundary, observations, err)
			}
			if fixture.Disposition == g.DispositionNone {
				if err != nil || calls != 1 {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
			} else {
				var failure *g.PolicyFailure
				if !errors.As(err, &failure) || calls != 0 || failure.Decision.Disposition != fixture.Disposition {
					t.Fatalf("calls=%d err=%v", calls, err)
				}
			}
		})
	}
}

func TestReferenceChunkFixturesStrictProducerConsumer(t *testing.T) {
	for _, fixture := range gt.ReferenceBoundaryFixtures() {
		if len(fixture.Chunks) == 0 {
			continue
		}
		t.Run(fixture.Name, func(t *testing.T) {
			// Arrange: use the actual chunk fixture, not the prejoined string path.
			p := fixturePipeline(fixture)
			var sink bytes.Buffer
			stream, err := g.CompileStream(&sink, g.StreamConfig{
				Identity:          fixture.Name,
				Profile:           g.ReleaseWholeResponse,
				Pipeline:          p,
				ScopeFactory:      nil,
				Delivery:          g.NewUserTextPolicy("external"),
				Observer:          nil,
				MaxInputBytes:     1024,
				MaxPendingBytes:   1024,
				MaxUnitBytes:      1024,
				MaxOutputBytes:    1024,
				ValidationTimeout: time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			for _, chunk := range fixture.Chunks {
				if _, err = stream.Write([]byte(chunk)); err != nil {
					t.Fatal(err)
				}
			}
			if sink.Len() != 0 {
				t.Fatal("strict release exposed prefix")
			}
			outcome, err := stream.Complete(context.Background())
			// Assert.
			if outcome.Decision.Disposition != fixture.Disposition || sink.String() != fixture.Delivered {
				t.Fatalf("%+v %v %q", outcome, err, sink.String())
			}
			if fixture.Disposition != g.DispositionNone {
				var failure *g.PolicyFailure
				if !errors.As(err, &failure) || failure.Decision.Disposition != fixture.Disposition {
					t.Fatalf("%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
