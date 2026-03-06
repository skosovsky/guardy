// Output guard: validate LLM response as JSON against a schema.
// On Retry, prints Reason/Evidence/Guidance so the orchestrator can ask the LLM to fix the JSON.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

// Schema requires an object with optional "answer" field for demo.
const schema = `{"type":"object","properties":{"answer":{"type":"string"}},"additionalProperties":true}`

func main() {
	jsonV := ext.MustJSONSchema(schema, "INVALID_JSON")
	pipeline := guardy.NewPipeline(guardy.WithTier1(jsonV))

	// Simulated LLM output (in real use, read from model response).
	llmOutput := `{"answer": "Hello, world!"}`
	if len(os.Args) > 1 {
		llmOutput = os.Args[1]
	}

	ctx := context.Background()
	report, err := pipeline.Run(ctx, &guardy.Input{Data: llmOutput})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pipeline error:", err)
		os.Exit(2)
	}
	switch report.FinalAction {
	case guardy.Retry:
		if len(report.Results) > 0 {
			r := report.Results[0]
			fmt.Fprintln(os.Stderr, "Retry — send back to LLM:")
			fmt.Fprintln(os.Stderr, "  Reason:", r.Reason)
			fmt.Fprintln(os.Stderr, "  Evidence:", r.Evidence)
			fmt.Fprintln(os.Stderr, "  Guidance:", r.Guidance)
		}
		os.Exit(1)
	case guardy.Pass:
		var out map[string]any
		if err := json.Unmarshal([]byte(llmOutput), &out); err != nil {
			fmt.Fprintln(os.Stderr, "json parse:", err)
			os.Exit(2)
		}
		fmt.Println("Valid JSON:", out)
	default:
		fmt.Fprintln(os.Stderr, "unexpected action:", report.FinalAction)
		os.Exit(2)
	}
}
