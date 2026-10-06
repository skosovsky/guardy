// OTel integration: attach ext/guardyotel middleware to pipeline validators.
package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
	"github.com/skosovsky/guardy/ext/guardyotel"
)

func main() {
	regexValidator, err := ext.NewRegexValidator(
		`(?i)forbidden`,
		ext.WithCode("FORBIDDEN_CONTENT"),
		ext.WithSeverity(guardy.SeverityHigh),
	)
	if err != nil {
		panic(err)
	}

	pipeline := guardy.NewPipeline(guardy.WithFastPath(regexValidator))
	pipeline = pipeline.Use(guardyotel.NewMiddleware[string](
		guardyotel.WithIncludePayloads(false), // secure-by-default; keep payloads disabled
	))

	result, err := pipeline.Run(context.Background(), nil, "this text is forbidden")
	if err != nil {
		panic(err)
	}
	streamed, streamErr := streamExample(guardy.ReleaseValidatedUnits)
	if streamErr != nil {
		panic(streamErr)
	}
	fmt.Printf("stream=%q\n", streamed)
	decision := result.PolicyDecision()
	fmt.Printf("action=%s code=%s severity=%s\n", decision.Action.String(), decision.Code, decision.Severity)
}

// streamExample uses an explicitly unit-local rule. OTel needs no capability
// adapter; it does not certify a rule's cross-unit detection quality.
func streamExample(profile guardy.ReleaseProfile) (string, error) {
	const totalBudget = 64
	const unitBudget = 8
	var sink bytes.Buffer
	rule := guardy.WithStreamingCapabilities(
		ext.NewLengthValidator(0, totalBudget),
		guardy.StreamCapabilities{Partial: true, Unit: true, Final: true, Lookbehind: 0, Lookahead: 0},
	)
	pipeline := guardy.NewPipeline(guardy.WithFastPath(rule)).Use(guardyotel.NewMiddleware[string]())
	cfg := guardy.StreamConfig{
		Delivery:          guardy.NewUserTextPolicy("external"),
		Identity:          "otel-example",
		Profile:           profile,
		Pipeline:          pipeline,
		MaxInputBytes:     totalBudget,
		MaxPendingBytes:   totalBudget,
		MaxUnitBytes:      unitBudget,
		MaxOutputBytes:    totalBudget,
		ValidationTimeout: time.Second,
	}
	if profile == guardy.ReleaseWholeResponse {
		cfg.MaxUnitBytes = totalBudget
	}
	stream, err := guardy.CompileStream(&sink, cfg)
	if err != nil {
		return "", err
	}
	if _, err = stream.Write([]byte("hello\nworld\n")); err != nil {
		return "", err
	}
	if _, err = stream.Complete(context.Background()); err != nil {
		return "", err
	}
	return sink.String(), nil
}
