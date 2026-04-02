// Generic decorator example: WrapInput + WrapOutput around a func(ctx, prompt) (reply, err)
// without net/http — suitable for any router Executor-style API.
//
// The output guard uses a regex on "secret" to model a leak check. For real PII redaction/block,
// swap in ext.NewPIIValidator with the same WrapOutput pattern.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

// askLLM simulates an LLM: normal prompts get a safe reply; prompts containing "LEAKY"
// get a reply that triggers the output guard (regex on "secret").
func askLLM(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "LEAKY") {
		return "Here is the secret token for you.", nil
	}
	return "Hello! How can I help?", nil
}

func main() {
	ctx := context.Background()

	inGuard := ext.NewWordlistValidator(
		[]string{"forbidden"},
		ext.Blocklist,
		ext.WithCode("TOXIC_INPUT"),
	)
	inPipe := guardy.NewPipeline(guardy.WithFastPath(inGuard))

	outRe, reErr := ext.NewRegexValidator(`(?i)secret`, ext.WithCode("SECRET_IN_OUTPUT"))
	if reErr != nil {
		log.Fatal(reErr)
	}
	outPipe := guardy.NewPipeline(guardy.WithFastPath(outRe))

	safe := guardy.WrapOutput(outPipe, guardy.WrapInput(inPipe, askLLM))

	if _, inErr := safe(ctx, "forbidden word in prompt"); inErr != nil {
		if errors.Is(inErr, guardy.ErrBlocked) {
			fmt.Println("input blocked:", inErr)
		} else {
			log.Fatal(inErr)
		}
	}

	if _, outErr := safe(ctx, "LEAKY prompt"); outErr != nil {
		if errors.Is(outErr, guardy.ErrBlocked) {
			fmt.Println("output blocked:", outErr)
		} else {
			log.Fatal(outErr)
		}
	}

	out, err := safe(ctx, "nice user question")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("ok:", out)
}
