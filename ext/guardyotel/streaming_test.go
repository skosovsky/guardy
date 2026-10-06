package guardyotel

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/skosovsky/guardy"
)

func otelStreamConfig(p *guardy.Pipeline[string], profile guardy.ReleaseProfile) guardy.StreamConfig {
	return guardy.StreamConfig{
		Identity:          "otel-stream",
		Profile:           profile,
		Pipeline:          p,
		Delivery:          guardy.NewUserTextPolicy("external"),
		MaxInputBytes:     64,
		MaxPendingBytes:   64,
		MaxUnitBytes:      4,
		MaxOutputBytes:    64,
		ValidationTimeout: time.Second,
	}
}

func TestOTelStreamingCapabilitiesPreserveEveryLayer(t *testing.T) {
	for _, profile := range []guardy.ReleaseProfile{guardy.ReleaseWholeResponse, guardy.ReleaseValidatedUnits, guardy.ReleaseBestEffort} {
		for _, test := range []struct {
			name       string
			capability *guardy.StreamCapabilities
			inner      bool
			allowed    bool
		}{
			{name: "declared", capability: &guardy.StreamCapabilities{Partial: true, Unit: true, Final: true}, allowed: true},
			{name: "unknown", allowed: profile == guardy.ReleaseWholeResponse},
			{name: "final only", capability: &guardy.StreamCapabilities{Final: true}, allowed: profile == guardy.ReleaseWholeResponse},
			{name: "unknown inner middleware", capability: &guardy.StreamCapabilities{Partial: true, Unit: true, Final: true}, inner: true, allowed: profile == guardy.ReleaseWholeResponse},
		} {
			t.Run(string(profile)+test.name, func(t *testing.T) {
				verifyOTelStream(t, profile, test.capability, test.inner, test.allowed)
			})
		}
	}
}

func verifyOTelStream(
	t *testing.T,
	profile guardy.ReleaseProfile,
	capability *guardy.StreamCapabilities,
	inner, allowed bool,
) {
	t.Helper()
	// Arrange.
	var sink bytes.Buffer
	var rule guardy.Validator[string] = guardy.ValidatorFunc[string](func(_ context.Context, v string) (string, *guardy.Report, error) { return v, nil, nil })
	if capability != nil {
		rule = guardy.WithStreamingCapabilities(rule, *capability)
	}
	p := guardy.MustNewPipeline(guardy.WithSequential(rule))
	if inner {
		p = p.MustUse(func(next guardy.Validator[string]) guardy.Validator[string] {
			return guardy.ValidatorFunc[string](next.Validate)
		})
	}
	p = p.MustUse(NewMiddleware[string](WithTracer(nil), WithMeter(nil)))
	cfg := otelStreamConfig(p, profile)
	if profile == guardy.ReleaseWholeResponse {
		cfg.MaxUnitBytes = 64
	}
	// Act.
	stream, err := guardy.CompileStream(&sink, cfg)
	// Assert.
	if !allowed {
		if err == nil || stream != nil || sink.Len() != 0 {
			t.Fatalf("unsupported stream=%v err=%v", stream, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Write([]byte("one\ntwo\n")); err != nil {
		t.Fatal(err)
	}
	outcome, err := stream.Complete(context.Background())
	if err != nil || sink.String() != "one\ntwo\n" || outcome.ReleasedBytes != 8 ||
		outcome.Category != guardy.StreamSuccess {
		t.Fatalf("sink=%q outcome=%+v err=%v", sink.String(), outcome, err)
	}
}
