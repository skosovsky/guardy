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

func ExampleJSONArgsPipeline_Validate() {
	schema := guardy.JSONArgsSchemaFunc{ID: "command.schema"}
	argsPipeline := guardy.MustCompileJSONArgs(guardy.NewPipeline[string](), schema)

	args, err := argsPipeline.Validate(context.Background(), nil, `{"name":"Ada"}`)
	if err != nil {
		panic(err)
	}

	fmt.Println(args.SchemaID)
	fmt.Println(args.Object["name"])
	fmt.Println(args.Decision.Action)
	// Output:
	// command.schema
	// Ada
	// pass
}

func ExampleWrapGuardedJSONArgs() {
	schema := guardy.JSONArgsSchemaFunc{ID: "command.schema"}
	argsPipeline := guardy.MustCompileJSONArgs(guardy.NewPipeline[string](), schema)
	handler := guardy.WrapGuardedJSONArgs(
		argsPipeline,
		nil,
		func(_ context.Context, args guardy.GuardedJSONArgs) (string, error) {
			return args.SchemaID + ":" + args.Object["name"].(string), nil
		},
	)

	result, args, err := handler(context.Background(), `{"name":"Ada"}`)
	if err != nil {
		panic(err)
	}

	fmt.Println(result)
	fmt.Println(args.Decision.Action)
	// Output:
	// command.schema:Ada
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

func ExamplePipeline_GuardDelivery() {
	outputPipeline := guardy.NewPipeline[string]()
	guarded, _ := outputPipeline.GuardDelivery(
		context.Background(),
		nil,
		guardy.NewDeliveryPolicy("external", guardy.WithDeliveryFallback("Blocked.")),
		`{"internal":true}`,
	)
	value, ok := guarded.DeliverableValue()

	fmt.Println(value, ok)
	fmt.Println(guarded.Kind)
	fmt.Println(guarded.Fallback)
	// Output:
	// Blocked. true
	// safe_user_text
	// true
}

func ExampleDecision_Route() {
	decision := guardy.DecisionFromReport(guardy.FinishReport(&guardy.Report{
		Action:   guardy.ActionRetry,
		Feedback: "fix the payload",
	}, guardy.ControlSpec{Action: guardy.ActionRetry}))

	route := decision.Route(guardy.RemediationPolicy{RetryAttempt: 1, MaxRetries: 3})

	fmt.Println(route.Outcome)
	fmt.Println(route.Retryable)
	fmt.Println(route.RetryFeedback)
	// Output:
	// retry_correction
	// true
	// fix the payload
}
