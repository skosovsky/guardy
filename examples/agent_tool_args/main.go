// Agent tool args: MapJSONRawMessage on opaque tool-args JSON inside a struct pipeline.
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

type agentCall struct {
	ToolName string          `json:"tool_name"`
	ToolArgs json.RawMessage `json:"tool_args"`
}

func main() {
	piiV := ext.NewPIIValidator(
		ext.WithAction(guardy.ActionRedact),
		ext.WithCode("PII_IN_TOOL_ARGS"),
	)
	toolArgsGuard := guardy.MapJSONRawMessage(
		piiV,
		func(c *agentCall) json.RawMessage { return c.ToolArgs },
		func(c *agentCall, raw json.RawMessage) *agentCall {
			c.ToolArgs = raw
			return c
		},
	)

	pipeline := guardy.NewPipeline(guardy.WithFastPath(toolArgsGuard))
	in := agentCall{
		ToolName: "lookup",
		ToolArgs: json.RawMessage(`{"user":"alice","email":"alice@example.com"}`),
	}
	result, err := pipeline.Run(context.Background(), nil, in)
	if err != nil {
		panic(err)
	}
	decision := result.PolicyDecision()
	fmt.Println("Action:", decision.Action)
	fmt.Println("Code:", decision.Code)
	out := result.Output
	fmt.Println("Tool args:", string(out.ToolArgs))
	if !json.Valid(out.ToolArgs) {
		panic("tool args must remain valid JSON")
	}
}
