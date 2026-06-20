// Policy attributes: scope-aware rules without domain-specific types in guardy.
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/skosovsky/guardy"
)

func main() {
	roleKey := guardy.NewScopeKey[string]("principal.role")
	pipeline := guardy.NewPipeline(
		guardy.WithPolicyValidators(
			guardy.NewTypedAttributeEquals[string, string](
				roleKey,
				"admin",
				guardy.WithPolicySafeUserMessage("You do not have permission to run this action."),
			),
		),
		guardy.WithFastPath(noopPassValidator()),
	)

	scope := guardy.NewScope(guardy.ScopeValue(roleKey, "viewer"))
	result, err := pipeline.Run(context.Background(), scope, "hello")
	if err != nil {
		panic(err)
	}
	decision := result.PolicyDecision()
	fmt.Println("--- policy mismatch ---")
	fmt.Println("Action:", decision.Action)
	fmt.Println("Code:", decision.Code)
	fmt.Println("Message:", decision.SafeMessage)
	fmt.Println("Disposition:", decision.Disposition)

	_, err = pipeline.Run(context.Background(), guardy.NewScope(), "hello")
	if !errors.Is(err, guardy.ErrScopeIncomplete) {
		panic(fmt.Sprintf("expected ErrScopeIncomplete, got %v", err))
	}
	fmt.Println("--- missing scope ---")
	fmt.Println("ErrScopeIncomplete:", err)
	fmt.Println("Missing:", guardy.MissingScopeKeys(err))
}

func noopPassValidator() guardy.ValidatorFunc[string] {
	return func(_ context.Context, input string) (string, *guardy.Report, error) {
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: "noop"}, nil
	}
}
