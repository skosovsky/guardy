// Struct validation: JSON schema pipeline plus ValidateAndBind into a typed struct.
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

	user, rep, err := guardy.ValidateAndBind[User](context.Background(), pipeline, sampleJSON)
	var retry *guardy.RetryError
	if errors.As(err, &retry) {
		fmt.Println("Action:", rep.Action)
		fmt.Println("Code:", rep.Code)
		fmt.Println("Retryable:", rep.Retryable)
		fmt.Println("Feedback:")
		fmt.Println(retry.Feedback)
		return
	}
	if err != nil {
		panic(err)
	}
	fmt.Println("Bound user:", user.Name, user.Age)
	fmt.Println("Report action:", rep.Action)
}
