// Reversible redaction: validators emit guardy tokens, then UnredactText restores originals.
package main

import (
	"context"
	"fmt"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func main() {
	vault := ext.NewInMemoryTokenVault()

	piiValidator := ext.NewPIIValidator(
		ext.WithAction(guardy.ActionRedact),
		ext.WithTokenVault(vault),
		ext.WithCode("PII_DETECTED"),
		ext.WithSeverity(guardy.SeverityHigh),
	)
	wordlistValidator := ext.NewWordlistValidator(
		[]string{"acme"},
		ext.Blocklist,
		ext.WithAction(guardy.ActionRedact),
		ext.WithLowercase(true),
		ext.WithTokenVault(vault),
		ext.WithCode("CONFIDENTIAL_TERM"),
		ext.WithSeverity(guardy.SeverityMedium),
	)

	pipeline := guardy.NewPipeline(guardy.WithFastPath(piiValidator, wordlistValidator))
	input := "Contact alice@example.com. Internal customer: ACME."
	result, err := pipeline.Run(context.Background(), input)
	if err != nil {
		panic(err)
	}

	redacted := result.Output
	llmAnswer := "Approved summary: " + redacted
	restored := ext.UnredactText(llmAnswer, vault)

	fmt.Println("redacted:", redacted)
	fmt.Println("restored:", restored)
}
