package integration_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	g "github.com/skosovsky/guardy"
)

//nolint:gocognit // Matrix checks actual sink, causes and side effects for each output boundary outcome.
func TestOutputAdapterDeliversOnlyApprovedProjection(t *testing.T) {
	for _, stage := range []string{"pass", "redact", "partial", "scope", "go-error", "report-fault", "cancel", "deny", "retry"} {
		t.Run(stage, func(t *testing.T) {
			// Arrange.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("private handler failure")
			effects, checks, facts := 0, 0, 0
			rule := g.ValidatorFunc[string](func(_ context.Context, v string) (string, *g.Report, error) {
				checks++
				switch stage {
				case "redact":
					return "approved", &g.Report{Action: g.ActionRedact, MutatedText: "wrong mirror"}, nil
				case "go-error":
					return "unvalidated", &g.Report{Action: g.ActionPass}, cause
				case "report-fault":
					return "unvalidated", &g.Report{Action: g.Action(255)}, nil
				case "cancel":
					cancel()
					return "unvalidated", &g.Report{Action: g.ActionPass}, nil
				case "deny":
					return v, &g.Report{Action: g.ActionBlock}, nil
				case "retry":
					return v, &g.Report{Action: g.ActionRetry, Retryable: true}, nil
				default:
					return v, &g.Report{Action: g.ActionPass}, nil
				}
			})
			factory := g.ScopeFactory(func(context.Context) (g.ExecutionScope, error) {
				facts++
				if stage == "scope" {
					return nil, cause
				}
				return g.NewScope(), nil
			})
			handler := func(context.Context, string) (string, error) {
				effects++
				if stage == "partial" {
					return "private partial", cause
				}
				return "original", nil
			}
			wrapped := g.WrapGuardedOutput(g.MustNewPipeline(g.WithSequential(rule)), factory, handler)
			var sink bytes.Buffer
			// Act.
			delivery, err := wrapped(ctx, "request")
			projection, approved := delivery.Projection()
			if approved {
				_, _ = sink.WriteString(projection.Value)
			}
			// Assert.
			want := ""
			if stage == "pass" {
				want = "original"
			}
			if stage == "redact" {
				want = "approved"
			}
			if sink.String() != want || effects != 1 || approved != (want != "") || (err == nil) != (want != "") {
				t.Fatalf("sink=%q effects=%d approved=%v err=%v", sink.String(), effects, approved, err)
			}
			if stage == "partial" && (delivery.Value != "" || checks != 0 || facts != 0 || !errors.Is(err, cause)) {
				t.Fatal("partial result escaped or later checks ran")
			}
			if stage == "scope" && (checks != 0 || !errors.Is(err, cause)) {
				t.Fatal("scope fault did not stop validation")
			}
			if stage == "go-error" && !errors.Is(err, cause) {
				t.Fatal("cause lost")
			}
			if stage == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost")
			}
		})
	}
}

func TestLowLevelOutputPreservesUnvalidatedPartialWithoutRetry(t *testing.T) {
	// Arrange.
	cause := errors.New("handler failure")
	effects, checks := 0, 0
	p := g.MustNewPipeline(
		g.WithSequential(g.ValidatorFunc[string](func(context.Context, string) (string, *g.Report, error) {
			checks++
			return "approved", &g.Report{Action: g.ActionRedact}, nil
		})),
	)
	wrapped := g.WrapOutput(
		p,
		nil,
		func(context.Context, string) (string, error) { effects++; return "unvalidated partial", cause },
	)
	// Act.
	value, err := wrapped(context.Background(), "request")
	// Assert.
	if value != "unvalidated partial" || !errors.Is(err, cause) || effects != 1 || checks != 0 {
		t.Fatalf("value=%q err=%v effects=%d checks=%d", value, err, effects, checks)
	}
}
