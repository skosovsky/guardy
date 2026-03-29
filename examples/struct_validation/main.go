// Struct validation: validate JSON against a struct schema using ext/jsonschema.
package main

import (
	"context"
	"fmt"

	"github.com/skosovsky/guardy"
	jsonschemaext "github.com/skosovsky/guardy/ext/jsonschema"
)

type User struct {
	Name string `json:"name" jsonschema:"required"`
	Age  int    `json:"age"  jsonschema:"minimum=18"`
}

func main() {
	validator, err := jsonschemaext.NewJSONSchemaValidatorFromStruct(&User{})
	if err != nil {
		panic(err)
	}

	pipeline := guardy.NewPipeline(guardy.WithFastPath(validator))
	const sampleJSON = `{"name":"Ivan","age":12}`
	result, err := pipeline.Run(context.Background(), sampleJSON)
	if err != nil {
		panic(err)
	}

	report := result.Decision()
	fmt.Println("Action:", report.Action)
	fmt.Println("Feedback:")
	fmt.Println(report.Feedback)
}
