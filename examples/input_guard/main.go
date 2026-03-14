// Prompt guard: validate user prompt before sending to LLM.
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
	regexV, err := ext.NewRegex(`(?i)(ignore previous|system prompt|disregard instructions)`, guardy.ActionBlock, "PROMPT_INJECTION")
	if err != nil {
		fmt.Fprintln(os.Stderr, "regex:", err)
		os.Exit(1)
	}
	lengthV := ext.NewLength(1, maxPromptLen, guardy.ActionBlock, "TOO_LONG")
	pipeline := guardy.NewPipeline(guardy.WithFastPath(regexV, lengthV))

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		fmt.Fprintln(os.Stderr, "no input")
		os.Exit(1)
	}
	prompt := scanner.Text()

	ctx := context.Background()
	report, err := pipeline.Run(ctx, prompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pipeline error:", err)
		os.Exit(2)
	}
	switch report.Action {
	case guardy.ActionBlock:
		fmt.Fprintf(os.Stderr, "blocked: %s - %s\n", report.Validator, report.Reason)
		os.Exit(3)
	case guardy.ActionPass, guardy.ActionRedact:
		fmt.Println("OK")
		if report.MutatedText != "" {
			fmt.Println(report.MutatedText)
		} else {
			fmt.Println(prompt)
		}
	default:
		fmt.Fprintln(os.Stderr, "unexpected action:", report.Action)
		os.Exit(2)
	}
}
