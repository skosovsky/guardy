// Input guard: validate user prompt before sending to LLM.
// Uses Regex (prompt injection pattern) + Length (max length).
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

const maxPromptLen = 4000

func main() {
	regexV, err := ext.NewRegex(`(?i)(ignore previous|system prompt|disregard instructions)`, guardy.Block, "PROMPT_INJECTION")
	if err != nil {
		fmt.Fprintln(os.Stderr, "regex:", err)
		os.Exit(1)
	}
	lengthV := ext.NewLength(1, maxPromptLen, guardy.Block, "TOO_LONG")
	pipeline := guardy.NewPipeline(
		guardy.WithFailFast(true),
		guardy.WithTier1(regexV, lengthV),
	)

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		fmt.Fprintln(os.Stderr, "no input")
		os.Exit(1)
	}
	prompt := scanner.Text()

	ctx := context.Background()
	report, err := pipeline.Run(ctx, guardy.Input{Text: prompt})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pipeline error:", err)
		os.Exit(2)
	}
	switch report.FinalAction {
	case guardy.Block:
		if len(report.Results) > 0 {
			r := report.Results[0]
			fmt.Fprintf(os.Stderr, "blocked: %s - %s\n", r.Code, r.Reason)
		} else {
			fmt.Fprintln(os.Stderr, "blocked")
		}
		os.Exit(3)
	case guardy.Pass, guardy.Redact:
		fmt.Println("OK")
		fmt.Println(report.FinalText)
	default:
		fmt.Fprintln(os.Stderr, "unexpected action:", report.FinalAction)
		os.Exit(2)
	}
}
