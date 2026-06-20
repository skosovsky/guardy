// OTel integration: attach ext/guardyotel middleware to pipeline validators.
package main

import (
	"context"
	"fmt"

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
	decision := result.PolicyDecision()
	fmt.Printf("action=%s code=%s severity=%s\n", decision.Action.String(), decision.Code, decision.Severity)
}
