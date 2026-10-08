package guardy_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	g "github.com/skosovsky/guardy"
)

func Example_guardedConsumer() {
	redact := g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
		clean := strings.ReplaceAll(value, "user@example.com", "[email]")
		return clean, &g.Report{Action: g.ActionRedact}, nil
	})
	pipeline := g.MustNewPipeline(g.WithSequential(redact))
	delivery, err := pipeline.GuardOutput(context.Background(), nil, "Contact user@example.com")
	if err != nil {
		fmt.Println("delivery rejected")
		return
	}
	projection, ok := delivery.Projection()
	if !ok {
		return
	}
	fmt.Println(projection.Value)
	// Output: Contact [email]
}

// lowLevelConsumer follows the README recipe. It never interprets MutatedText.
func lowLevelConsumer[T any](ctx context.Context, p *g.Pipeline[T], input T, consume func(T)) error {
	result, err := p.Run(ctx, nil, input)
	if err != nil {
		return err
	}
	decision := result.PolicyDecision()
	switch {
	case decision.IsSystemFault():
		return errors.New("validation fault")
	case decision.IsTerminal():
		return errors.New("blocked")
	case decision.IsRetryable():
		return errors.New("correction required")
	default:
		consume(result.Output)
		return nil
	}
}

func TestDocumentedConsumerFaultChannelsAndAuthoritativeOutput(t *testing.T) {
	for _, outcome := range []string{"pass", "redact", "deny", "retry", "report-fault", "go-error"} {
		t.Run(outcome, func(t *testing.T) { checkDocumentedConsumerFaultChannelsAndAuthoritativeOutput(t, outcome) })
	}
}

func TestDocumentedStructLensConsumesAuthoritativeValue(t *testing.T) {
	// Arrange.
	type dto struct {
		Text string
		ID   int
	}
	rule := g.ValidatorFunc[string](func(context.Context, string) (string, *g.Report, error) {
		return "clean", &g.Report{Action: g.ActionRedact, MutatedText: "wrong mirror"}, nil
	})
	lens := g.Map(rule, func(v dto) string { return v.Text }, func(v dto, s string) dto { v.Text = s; return v })
	p := g.MustNewPipeline(g.WithSequential(lens))
	original := dto{Text: "secret", ID: 42}
	var sink dto
	calls := 0
	// Act.
	err := lowLevelConsumer(context.Background(), p, original, func(value dto) { sink = value; calls++ })
	// Assert.
	if err != nil || calls != 1 || sink.Text != "clean" || sink.ID != 42 || original.Text != "secret" {
		t.Fatalf("sink=%+v calls=%d err=%v", sink, calls, err)
	}
}

func checkDocumentedConsumerFaultChannelsAndAuthoritativeOutput(t *testing.T, outcome string) {
	t.Helper()
	// Arrange.
	cause := errors.New("private detector failure")
	rule := g.ValidatorFunc[string](func(_ context.Context, value string) (string, *g.Report, error) {
		switch outcome {
		case "redact":
			return "clean typed output", &g.Report{
				Action:      g.ActionRedact,
				MutatedText: "wrong diagnostic mirror",
			}, nil
		case "deny":
			return "unsafe", &g.Report{Action: g.ActionBlock}, nil
		case "retry":
			return "unsafe", &g.Report{Action: g.ActionRetry, Retryable: true}, nil
		case "report-fault":
			return "unsafe", &g.Report{Disposition: g.DispositionSystemFault}, nil
		case "go-error":
			return "unsafe", &g.Report{Action: g.ActionPass}, cause
		default:
			return value, &g.Report{Action: g.ActionPass}, nil
		}
	})
	p := g.MustNewPipeline(g.WithSequential(rule))
	var guardedSink, lowSink bytes.Buffer
	// Act.
	delivery, guardedErr := p.GuardOutput(context.Background(), nil, "original")
	if projection, approved := delivery.Projection(); approved {
		_, _ = guardedSink.WriteString(projection.Value)
	}
	lowErr := lowLevelConsumer(
		context.Background(),
		p,
		"original",
		func(value string) { _, _ = lowSink.WriteString(value) },
	)
	// Assert.
	want := ""
	if outcome == "pass" {
		want = "original"
	}
	if outcome == "redact" {
		want = "clean typed output"
	}
	if guardedSink.String() != want || lowSink.String() != want || (guardedErr == nil) != (want != "") ||
		(lowErr == nil) != (want != "") {
		t.Fatalf("guarded=%q low=%q errors=%v/%v", guardedSink.String(), lowSink.String(), guardedErr, lowErr)
	}
	if outcome == "go-error" && (!errors.Is(guardedErr, cause) || !errors.Is(lowErr, cause)) {
		t.Fatal("error cause lost")
	}
}
