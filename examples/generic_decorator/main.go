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
	roleKey := guardy.NewScopeKey[string]("principal.role")
	scope := guardy.NewScope(guardy.ScopeValue(roleKey, "user"))

	inPipe := guardy.NewPipeline(
		guardy.WithPolicyValidators(
			guardy.NewTypedAttributeEquals[string, string](roleKey, "admin"),
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

	safe := guardy.WrapGuardedOutput(outPipe, scope, guardy.WrapInput(inPipe, scope, askLLM))

	if _, err := safe(ctx, "forbidden word"); err != nil {
		printBlock("input wordlist", err)
	}

	if _, err := safe(ctx, "LEAKY prompt"); err != nil {
		printBlock("output user channel", err)
	}

	adminScope := guardy.NewScope(guardy.ScopeValue(roleKey, "admin"))
	adminSafe := guardy.WrapGuardedOutput(outPipe, adminScope, guardy.WrapInput(inPipe, adminScope, askLLM))
	out, err := adminSafe(ctx, "nice user question")
	if err != nil {
		log.Fatal(err)
	}
	value, ok := out.DeliverableValue()
	fmt.Println("ok:", value, ok)
	fmt.Println("PayloadKind:", out.Kind)
}

func printBlock(label string, err error) {
	var failure *guardy.PolicyFailure
	if errors.As(err, &failure) {
		fmt.Printf("%s blocked: disposition=%s msg=%s\n",
			label,
			failure.Decision.Disposition,
			failure.Decision.SafeMessage,
		)
		return
	}
	log.Fatal(err)
}
