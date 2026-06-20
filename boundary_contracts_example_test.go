package guardy_test

import (
	"context"
	"fmt"

	"github.com/skosovsky/guardy"
)

func ExampleArgsPipeline_Validate() {
	type Command struct {
		Name string `json:"name"`
	}

	argsPipeline := guardy.MustCompileArgs[Command](guardy.NewPipeline[string]())
	payload, err := argsPipeline.Validate(context.Background(), nil, `{"name":"Ada"}`)
	if err != nil {
		panic(err)
	}

	fmt.Println(payload.Value.Name)
	fmt.Println(payload.Decision.Action)
	// Output:
	// Ada
	// pass
}

func ExamplePipeline_GuardOutput() {
	outputPipeline := guardy.NewPipeline(guardy.WithFastPath(guardy.ValidatorFunc[string](
		func(_ context.Context, input string) (string, *guardy.Report, error) {
			return input, guardy.FinishReport(&guardy.Report{
				Action:      guardy.ActionPass,
				Validator:   "classifier",
				PayloadKind: guardy.PayloadSafeUserText,
			}, guardy.ControlSpec{Action: guardy.ActionPass}), nil
		},
	)))

	guarded, err := outputPipeline.GuardOutput(context.Background(), nil, "hello")
	if err != nil {
		panic(err)
	}
	value, ok := guarded.DeliverableValue()

	fmt.Println(value, ok)
	fmt.Println(guarded.Decision.Action)
	// Output:
	// hello true
	// pass
}
