// Declarative guard: compile GuardSpec into a pipeline via guardy/build.
// Optional: build.WithJSONSchema(schemaBytes) for JSON Schema validation.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/build"
)

const (
	declarativeLengthMax = 4096
)

func main() { runExample(os.Stdout) }

func runExample(writer io.Writer) {
	// Scenario 1: policy scope mismatch only (no wordlist/PII — fast-path cannot mask policy outcome).
	roleKey := guardy.NewScopeKey[string]("principal.role")
	policyPipeline, err := build.CompileStringGuard(build.GuardSpec{
		PolicyRules: []build.PolicyRuleSpec{{
			Kind:  build.PolicyAttributeDeepEqual,
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
	fmt.Fprintln(writer, "--- policy scope mismatch ---")
	fmt.Fprintln(writer, "Disposition:", decision.Disposition)
	fmt.Fprintln(writer, "Output:", result.Output)
	// Continue to independent scenarios after this expected denial.

	// Scenario 2: heuristic JSON shape detection reaches the user-channel boundary.
	// It does not establish provenance or authorize any recipient.
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
		if _, expectedDeny := errors.AsType[*guardy.PolicyFailure](err); !expectedDeny {
			panic(err)
		}
	}
	fmt.Fprintln(writer, "--- user channel + classifier ---")
	fmt.Fprintln(writer, "Action:", outResult.Decision.Action)
	fmt.Fprintln(writer, "Disposition:", outResult.Decision.Disposition)
	fmt.Fprintln(writer, "Output:", outResult.Value)
	fmt.Fprintln(writer, "PayloadKind:", outResult.Kind)

	// Scenario 3: wordlist + PII + length from GuardSpec (README-style intent).
	fastPipeline, err := build.CompileStringGuard(build.GuardSpec{
		WordlistBlock: []string{"secret"},
		PIIRedact:     true,
		LengthMax:     declarativeLengthMax,
	})
	if err != nil {
		panic(err)
	}
	fastResult, err := fastPipeline.Run(context.Background(), nil, "secret alice@example.com")
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(writer, "--- wordlist + PII + length ---")
	fmt.Fprintln(writer, "Action:", fastResult.PolicyDecision().Action)
	fmt.Fprintln(writer, "Output:", fastResult.Output)
}
