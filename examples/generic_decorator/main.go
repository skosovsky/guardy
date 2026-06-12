// Generic decorator: scope-aware input policy + user channel output guard in one flow.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func askLLM(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "LEAKY") {
		return `{"tool":"search","arguments":{"query":"secret"}}`, nil
	}
	return "Hello! How can I help?", nil
}

func main() {
	ctx := context.Background()
	scope := guardy.MapScope{"principal.role": "user"}

	inPipe := guardy.NewPipeline(
		guardy.WithPolicyValidators(
			guardy.NewAttributeEquals[string]("principal.role", "admin"),
		),
		guardy.WithFastPath(ext.NewWordlistValidator(
			[]string{"forbidden"},
			ext.Blocklist,
			ext.WithCode("TOXIC_INPUT"),
		)),
	)

	classifier := ext.NewTechnicalJSONClassifier(ext.WithCode("TECHNICAL_JSON"))
	outPipe := guardy.NewPipeline(
		guardy.WithUserChannel[string](),
		guardy.WithUserChannelFallback[string]("Output blocked for user safety."),
		guardy.WithFastPath(classifier),
	)

	safe := guardy.WrapOutput(outPipe, scope, guardy.WrapInput(inPipe, scope, askLLM))

	if _, err := safe(ctx, "forbidden word"); err != nil {
		printBlock("input wordlist", err)
	}

	if _, err := safe(ctx, "LEAKY prompt"); err != nil {
		printBlock("output user channel", err)
	}

	adminScope := guardy.MapScope{"principal.role": "admin"}
	adminSafe := guardy.WrapOutput(outPipe, adminScope, guardy.WrapInput(inPipe, adminScope, askLLM))
	out, err := adminSafe(ctx, "nice user question")
	if err != nil {
		log.Fatal(err)
	}
	outResult, err := outPipe.Run(ctx, adminScope, out)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("ok:", out)
	fmt.Println("OutputKind:", outResult.OutputKind)
}

func printBlock(label string, err error) {
	var blockErr *guardy.BlockError
	if errors.As(err, &blockErr) {
		fmt.Printf("%s blocked: disposition=%s msg=%s\n", label, blockErr.Report.Disposition, blockErr.Message)
		return
	}
	log.Fatal(err)
}
