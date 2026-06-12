// Policy attributes: scope-aware rules without domain-specific types in guardy.
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/skosovsky/guardy"
)

func main() {
	pipeline := guardy.NewPipeline(
		guardy.WithPolicyValidators(
			guardy.NewAttributeEquals[string](
				"principal.role",
				"admin",
				guardy.WithPolicySafeUserMessage("You do not have permission to run this action."),
			),
		),
		guardy.WithFastPath(noopPassValidator()),
	)

	scope := guardy.MapScope{"principal.role": "viewer"}
	result, err := pipeline.Run(context.Background(), scope, "hello")
	if err != nil {
		panic(err)
	}
	rep := result.Decision()
	fmt.Println("--- policy mismatch ---")
	fmt.Println("Action:", rep.Action)
	fmt.Println("Code:", rep.Code)
	fmt.Println("Message:", rep.PublicMessage())
	fmt.Println("Disposition:", rep.Disposition)

	_, err = pipeline.Run(context.Background(), guardy.MapScope{}, "hello")
	if !errors.Is(err, guardy.ErrScopeIncomplete) {
		panic(fmt.Sprintf("expected ErrScopeIncomplete, got %v", err))
	}
	fmt.Println("--- missing scope ---")
	fmt.Println("ErrScopeIncomplete:", err)
}

func noopPassValidator() guardy.ValidatorFunc[string] {
	return func(_ context.Context, input string) (string, *guardy.Report, error) {
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: "noop"}, nil
	}
}
