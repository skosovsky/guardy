// Generic decorator: scope-aware input policy + user channel output guard in one flow.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
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

func main() { runExample(os.Stdout) }

func runExample(writer io.Writer) {
	ctx := context.Background()
	roleKey := guardy.NewScopeKey[string]("principal.role")
	scope := guardy.NewScope(guardy.ScopeValue(roleKey, "user"))

	inPipe := guardy.NewPipeline(
		guardy.WithPolicyValidators(
			guardy.NewTypedAttributeEquals[string, string](roleKey, "admin"),
		),
		guardy.WithFastPath(ext.MustWordlistValidator(
			[]string{"forbidden"},
			ext.Blocklist,
			ext.WithCode("TOXIC_INPUT"),
		)),
	)

	classifier := ext.MustTechnicalJSONClassifier(ext.WithCode("TECHNICAL_JSON"))
	outPipe := guardy.NewPipeline(
		guardy.WithUserChannel[string](),
		guardy.WithUserChannelFallback[string]("Output blocked for user safety."),
		guardy.WithFastPath(classifier),
	)

	scopeFactory := guardy.ScopeFactory(func(context.Context) (guardy.ExecutionScope, error) { return scope, nil })
	safe := guardy.WrapGuardedOutput(outPipe, scopeFactory, guardy.WrapInput(inPipe, scopeFactory, askLLM))

	if _, err := safe(ctx, "nice user question"); err != nil {
		printBlock(writer, "input scope policy", err)
	}

	adminScope := guardy.NewScope(guardy.ScopeValue(roleKey, "admin"))
	adminFactory := guardy.ScopeFactory(func(context.Context) (guardy.ExecutionScope, error) { return adminScope, nil })
	adminSafe := guardy.WrapGuardedOutput(outPipe, adminFactory, guardy.WrapInput(inPipe, adminFactory, askLLM))
	if _, err := adminSafe(ctx, "forbidden word"); err != nil {
		printBlock(writer, "input wordlist", err)
	}
	// Authorized input reaches the handler, then the separate delivery guard.
	leaky, err := adminSafe(ctx, "LEAKY prompt")
	if err != nil {
		printBlock(writer, "output user channel", err)
	} else {
		value, available := leaky.DeliverableValue()
		fmt.Fprintln(writer, "output user channel:", value, available)
	}

	out, err := adminSafe(ctx, "nice user question")
	if err != nil {
		log.Fatal(err)
	}
	value, ok := out.DeliverableValue()
	fmt.Fprintln(writer, "ok:", value, ok)
	fmt.Fprintln(writer, "PayloadKind:", out.Kind)
}

func printBlock(writer io.Writer, label string, err error) {
	if failure, ok := errors.AsType[*guardy.PolicyFailure](err); ok {
		fmt.Fprintf(writer, "%s blocked: disposition=%s msg=%s\n",
			label,
			failure.Decision.Disposition,
			failure.Decision.SafeMessage,
		)
		return
	}
	log.Fatal(err)
}
