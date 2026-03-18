// Output guard: validate and redact PII in LLM response.
// Uses PIIMasking in the Fast-Path to redact email, phone, credit card.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func main() {
	piiV := ext.NewPIIMasking()
	pipeline := guardy.NewPipeline(guardy.WithFastPath(piiV))

	llmOutput := `The user can be reached at john@example.com or 555-123-4567.`
	if len(os.Args) > 1 {
		llmOutput = os.Args[1]
	}

	ctx := context.Background()
	result, err := pipeline.Run(ctx, llmOutput)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pipeline error:", err)
		os.Exit(2)
	}
	report := result.Decision()
	switch report.Action {
	case guardy.ActionBlock:
		//nolint:gosec // G705 -- stderr output, not HTML
		fmt.Fprintf(os.Stderr, "blocked: %s\n", report.Reason)
		os.Exit(1)
	case guardy.ActionPass, guardy.ActionRedact:
		fmt.Println(result.Output)
	default:
		//nolint:gosec // G705 -- stderr output, not HTML
		fmt.Fprintln(os.Stderr, "unexpected action:", report.Action)
		os.Exit(2)
	}
}
