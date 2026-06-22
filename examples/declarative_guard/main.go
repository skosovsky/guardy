// Declarative guard: compile GuardSpec into a pipeline via guardy/build.
// Optional: build.WithJSONSchema(schemaBytes) for JSON Schema validation.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/build"
)

const (
	declarativeLengthMax = 4096
	exitBlocked          = 3
)

func main() {
	// Scenario 1: policy scope mismatch only (no wordlist/PII — fast-path cannot mask policy outcome).
	roleKey := guardy.NewScopeKey[string]("principal.role")
	policyPipeline, err := build.CompileStringGuard(build.GuardSpec{
		PolicyRules: []build.PolicyRuleSpec{{
			Kind:  build.PolicyAttributeEquals,
			Key:   roleKey.Name(),
			Value: "admin",
		}},
	})
	if err != nil {
		panic(err)
	}

	scope := guardy.NewScope(guardy.ScopeValue(roleKey, "viewer"))
	result, err := policyPipeline.Run(context.Background(), scope, "hello")
	if err != nil {
		panic(err)
	}
	decision := result.PolicyDecision()
	fmt.Println("--- policy scope mismatch ---")
	fmt.Println("Disposition:", decision.Disposition)
	fmt.Println("Output:", result.Output)
	if decision.IsTerminal() {
		os.Exit(exitBlocked)
	}

	// Scenario 2: output guard with user channel + technical JSON classifier.
	outputPipeline, err := build.CompileStringGuard(
		build.GuardSpec{},
		build.WithUserChannel(),
		build.WithUserChannelFallback("Output blocked for user safety."),
		build.WithOutputClassifier(),
	)
	if err != nil {
		panic(err)
	}
	outResult, err := outputPipeline.GuardOutput(context.Background(), nil, `{"tool":"search"}`)
	if err != nil {
		panic(err)
	}
	fmt.Println("--- user channel + classifier ---")
	fmt.Println("Action:", outResult.Decision.Action)
	fmt.Println("Disposition:", outResult.Decision.Disposition)
	fmt.Println("Output:", outResult.Value)
	fmt.Println("PayloadKind:", outResult.Kind)

	// Scenario 3: wordlist + PII + length from GuardSpec (README-style intent).
	fastPipeline, err := build.CompileStringGuard(build.GuardSpec{
		WordlistBlock: []string{"secret"},
		PIIRedact:     true,
		LengthMax:     declarativeLengthMax,
	})
	if err != nil {
		panic(err)
	}
	fastResult, err := fastPipeline.Run(context.Background(), nil, "no secrets here")
	if err != nil {
		panic(err)
	}
	fmt.Println("--- wordlist + PII + length ---")
	fmt.Println("Action:", fastResult.PolicyDecision().Action)
	fmt.Println("Output:", fastResult.Output)
}
