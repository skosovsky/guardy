// Struct validation: raw-first pipeline plus ArgsPipeline into a typed struct.
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/skosovsky/guardy"
	jsonschemaext "github.com/skosovsky/guardy/ext/jsonschema"
)

const minBusinessAge = 18

type User struct {
	Name string `json:"name" jsonschema:"required"`
	Age  int    `json:"age"`
}

func (u *User) ValidatePostBind(context.Context) error {
	if u.Age < minBusinessAge {
		return errors.New("business rule: age must be at least 18")
	}
	return nil
}

func main() {
	validator, err := jsonschemaext.NewJSONSchemaValidatorFromStruct(&User{})
	if err != nil {
		panic(err)
	}

	pipeline := guardy.NewPipeline(guardy.WithFastPath(validator))
	// Schema allows age 12; post-bind enforces business minimum 18.
	const sampleJSON = `{"name":"Ivan","age":12}`

	argsPipeline := guardy.MustCompileArgs[User](pipeline)
	payload, err := argsPipeline.Validate(context.Background(), nil, sampleJSON)
	var failure *guardy.PolicyFailure
	if errors.As(err, &failure) && failure.Decision.IsRetryable() {
		fmt.Println("Action:", failure.Decision.Action)
		fmt.Println("Code:", failure.Decision.Code)
		fmt.Println("Retryable:", failure.Decision.Retryable)
		fmt.Println("Feedback:")
		fmt.Println(failure.Decision.RetryFeedback)
		return
	}
	if err != nil {
		panic(err)
	}
	fmt.Println("Bound user:", payload.Value.Name, payload.Value.Age)
	fmt.Println("Decision action:", payload.Decision.Action)
}
