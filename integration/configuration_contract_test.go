package integration_test

import (
	"context"
	"errors"
	"fmt"

	g "github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext/jsonschema"
)

type nilArgsChecker struct{}

func (*nilArgsChecker) ValidateJSONArgs(context.Context, map[string]any) *g.Report {
	panic("nil checker invoked")
}

type argsConfigDTO struct {
	Mode   string `json:"mode"`
	Change bool   `json:"change"`
}

func (v *argsConfigDTO) ValidatePostBind(context.Context) error {
	if v.Change {
		v.Mode = "forbidden"
	}
	return nil
}

const argsConfigSchema = `{"type":"object","required":["mode"],"additionalProperties":false,"properties":{"mode":{"type":"string","enum":["allowed"]},"change":{"type":"boolean"}}}`

func Example_documentAPI() {
	// The same schema pipeline checks plain documents and typed API arguments.
	schema, err := jsonschema.NewJSONSchemaValidator(argsConfigSchema)
	if err != nil {
		panic(err)
	}
	checks := g.MustNewPipeline(g.WithSequential(schema))
	document, _ := checks.Run(context.Background(), nil, `{"mode":"allowed"}`)
	args := g.MustCompileArgs[argsConfigDTO](checks, g.WithArgsFinalGuard[argsConfigDTO](checks))
	_, invalid := args.Validate(context.Background(), nil, `{"mode":"allowed","change":true}`)
	fmt.Println(document.PolicyDecision().Action)
	fmt.Println(errors.Is(invalid, g.ErrRetryRequested))
	// Output:
	// pass
	// true
}
