// Multi-turn BYOT: adapt []ChatMessage to Validator[string] with MapSlice.
package main

import (
	"context"
	"fmt"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

type ChatMessage struct {
	Role    string
	Content string
}

func main() {
	base, err := ext.NewRegexValidator(
		`(?i)(password\s*[:=]\s*\S+)`,
		ext.WithAction(guardy.ActionRedact),
		ext.WithRedactionReplacement("[MASKED]"),
		ext.WithCode("PASSWORD_LEAK"),
	)
	if err != nil {
		panic(err)
	}

	multiTurnValidator := ext.MapSlice(
		func(m ChatMessage) string { return m.Content },
		func(m ChatMessage, newContent string) ChatMessage {
			m.Content = newContent
			return m
		},
		base,
	)

	pipeline := guardy.NewPipeline(guardy.WithFastPath(multiTurnValidator))
	messages := []ChatMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "password=super-secret"},
	}
	result, err := pipeline.Run(context.Background(), nil, messages)
	if err != nil {
		panic(err)
	}

	fmt.Println("decision:", result.PolicyDecision().Action)
	for i, msg := range result.Output {
		fmt.Printf("%d [%s] %s\n", i, msg.Role, msg.Content)
	}
}
