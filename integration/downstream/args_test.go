package downstream_test

import (
	"context"
	"testing"

	"github.com/skosovsky/toolsy"

	g "github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext/jsonschema"
)

type sampleArgs struct {
	Value string `json:"value"`
}

func newTool[T any](
	t *testing.T,
	binder toolsy.ArgsBinder[T],
	handler func(toolsy.ValidatedArgs[T]) (toolsy.ToolResult[string, string], error),
	resultCheck toolsy.ResultValidator[string],
) toolsy.Tool {
	t.Helper()
	tool, err := toolsy.NewTypedTool(toolsy.TypedToolSpec[toolsy.NoSubject, toolsy.NoScope, T, string, string]{
		Name:            "sample",
		Description:     "sample",
		ArgsBinder:      binder,
		ResultValidator: resultCheck,
		Handler: func(_ context.Context, _ toolsy.TypedCallContext[toolsy.NoSubject, toolsy.NoScope], _ *toolsy.RunEnv, args toolsy.ValidatedArgs[T]) (toolsy.ToolResult[string, string], error) {
			return handler(args)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tool
}

func finalSchema(t *testing.T, schema string) *g.Pipeline[string] {
	t.Helper()
	checker, err := jsonschema.NewJSONSchemaValidator(schema)
	if err != nil {
		t.Fatal(err)
	}
	return g.MustNewPipeline(g.WithSequential(checker))
}

type numericArgs struct {
	Amount int    `json:"amount"`
	Role   string `json:"role"`
}
type hookedArgs struct {
	Value string `json:"value"`
}

func (a *hookedArgs) ValidatePostBind(context.Context) error { a.Value = "changed"; return nil }

type profileFunc func(context.Context, toolsy.PreparedCall, toolsy.InvocationHandler, func(toolsy.Chunk) error) error

func (f profileFunc) ExecutePrepared(
	ctx context.Context,
	c toolsy.PreparedCall,
	next toolsy.InvocationHandler,
	y func(toolsy.Chunk) error,
) error {
	return f(ctx, c, next, y)
}

type postBindSchemaArgs struct {
	Amount   int    `json:"amount"`
	Role     string `json:"role,omitempty"`
	Mutation string `json:"mutation"`
}

func (a *postBindSchemaArgs) ValidatePostBind(context.Context) error {
	switch a.Mutation {
	case "required":
		a.Role = ""
	case "enum":
		a.Role = "other"
	case "minimum":
		a.Amount = 0
	}
	return nil
}
