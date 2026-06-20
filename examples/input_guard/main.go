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

const (
	exitBlocked = 3
	regexPrompt = `(?i)(ignore previous|system prompt|disregard instructions)`
)

func main() {
	regexV, err := ext.NewRegexValidator(regexPrompt, ext.WithCode("PROMPT_INJECTION"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "regex:", err)
		os.Exit(1)
	}
	lengthV := ext.NewLengthValidator(1, maxPromptLen, ext.WithCode("TOO_LONG"))
	pipeline := guardy.NewPipeline(guardy.WithFastPath(regexV, lengthV))

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		fmt.Fprintln(os.Stderr, "no input")
		os.Exit(1)
	}
	prompt := scanner.Text()

	ctx := context.Background()
	result, err := pipeline.Run(ctx, nil, prompt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pipeline error:", err)
		os.Exit(2)
	}
	decision := result.PolicyDecision()
	switch {
	case decision.IsTerminal():
		// #nosec G705 -- stderr output, not HTML
		fmt.Fprintf(os.Stderr, "blocked: code=%s disposition=%s msg=%s\n",
			decision.Code, decision.Disposition, decision.SafeMessage)
		os.Exit(exitBlocked)
	case decision.IsRetryable():
		fmt.Fprintf(os.Stderr, "retry: %s\n", decision.RetryFeedback)
		os.Exit(2)
	default:
		fmt.Println("OK")
		fmt.Println(result.Output)
	}
}
