// Output guard: validate and redact PII in LLM response; block technical JSON on user channel.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func main() {
	piiV := ext.NewPIIValidator(ext.WithCode("PII_DETECTED"))
	classifier := ext.NewTechnicalJSONClassifier(ext.WithCode("TECHNICAL_JSON"))
	pipeline := guardy.NewPipeline(
		guardy.WithUserChannel[string](),
		guardy.WithUserChannelFallback[string]("Sorry, I can't show that response."),
		guardy.WithFastPath(piiV, classifier),
	)

	ctx := context.Background()

	// Scenario 1: PII redact on safe user text.
	piiOutput := `The user can be reached at john@example.com or 555-123-4567.`
	if len(os.Args) > 1 {
		piiOutput = os.Args[1]
	}
	runPipeline(ctx, pipeline, piiOutput, "PII demo")

	// Scenario 2: user channel blocks technical JSON without CLI args.
	technicalJSON := `{"tool":"search","arguments":{"query":"secret"}}`
	runPipeline(ctx, pipeline, technicalJSON, "technical JSON demo")
}

func runPipeline(ctx context.Context, pipeline *guardy.Pipeline[string], input, label string) {
	fmt.Println("---", label, "---")
	output, err := pipeline.GuardOutput(ctx, nil, input)
	if err != nil {
		var failure *guardy.PolicyFailure
		if errors.As(err, &failure) {
			fmt.Fprintf(os.Stderr, "blocked: code=%s disposition=%s msg=%s\n",
				failure.Decision.Code, failure.Decision.Disposition, failure.Decision.SafeMessage)
			fmt.Fprintln(os.Stderr, "OutputKind:", output.Kind)
			return
		}
		fmt.Fprintln(os.Stderr, "pipeline error:", err)
		os.Exit(2)
	}
	value, ok := output.DeliverableValue()
	if !ok {
		fmt.Fprintln(os.Stderr, "output is not deliverable")
		return
	}
	fmt.Println(value)
	fmt.Println("OutputKind:", output.Kind)
}
