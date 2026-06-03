// Policy attributes: context-aware rules without domain-specific types in guardy.
package main

import (
	"context"
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

	ctx := guardy.WithAttributes(context.Background(), guardy.Attributes{
		"principal.role": "viewer",
	})
	result, err := pipeline.Run(ctx, "hello")
	if err != nil {
		panic(err)
	}
	rep := result.Decision()
	fmt.Println("Action:", rep.Action)
	fmt.Println("Code:", rep.Code)
	fmt.Println("Message:", rep.PublicMessage())
}

func noopPassValidator() guardy.ValidatorFunc[string] {
	return func(_ context.Context, input string) (string, *guardy.Report, error) {
		return input, &guardy.Report{Action: guardy.ActionPass, Validator: "noop"}, nil
	}
}
