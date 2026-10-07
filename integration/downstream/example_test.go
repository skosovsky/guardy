package downstream_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/skosovsky/toolsy"

	g "github.com/skosovsky/guardy"
	d "github.com/skosovsky/guardy/integration/downstream"
)

func ExampleNewArgsBinder() {
	// Arrange: final is required; adapter also enforces the real manifest schema.
	raw := g.MustNewPipeline(
		g.WithSequential(g.ValidatorFunc[string](func(_ context.Context, s string) (string, *g.Report, error) {
			return strings.ReplaceAll(s, "secret", "safe"), &g.Report{Action: g.ActionRedact}, nil
		})),
	)
	binder, err := d.NewArgsBinder[sampleArgs](raw, g.MustNewPipeline[string](), nil)
	if err != nil {
		panic(err)
	}
	tool, err := toolsy.NewTypedTool(
		toolsy.TypedToolSpec[toolsy.NoSubject, toolsy.NoScope, sampleArgs, string, string]{
			Name:        "echo",
			Description: "Echo approved args",
			ArgsBinder:  binder,
			Handler: func(_ context.Context, _ toolsy.TypedCallContext[toolsy.NoSubject, toolsy.NoScope], _ *toolsy.RunEnv, args toolsy.ValidatedArgs[sampleArgs]) (toolsy.ToolResult[string, string], error) {
				fmt.Println(string(args.Raw))
				return toolsy.NewToolResult[string, string](args.Value.Value), nil
			},
		},
	)
	if err != nil {
		panic(err)
	}
	// Act.
	err = tool.Execute(
		context.Background(),
		nil,
		toolsy.ToolInput{ArgsJSON: []byte(`{"value":"secret"}`)},
		func(chunk toolsy.Chunk) error { fmt.Println(string(chunk.Data)); return nil },
	)
	// Assert.
	if err != nil {
		panic(err)
	}
	// Output:
	// {"value":"safe"}
	// "safe"
}
