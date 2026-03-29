// JSON streaming: GuardWriter in JSON-aware mode buffers until complete JSON value.
// This prevents validating partial tool-call payloads.
package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

const demoChunkSizeBytes = 64

func main() {
	validator, err := ext.NewRegexValidator(`(?i)secret`, ext.WithCode("SECRET_IN_JSON"))
	if err != nil {
		panic(err)
	}

	pipeline := guardy.NewPipeline(guardy.WithFastPath(validator))
	var out bytes.Buffer
	gw := guardy.NewGuardWriter(
		&out,
		pipeline,
		guardy.WithJSONAwareSplitter(),
		guardy.WithChunkSize(demoChunkSizeBytes),
	)

	fragments := []string{
		`{"tool_calls":[{"name":"lookup","arguments":"user: `,
		`alice"}],`,
		`"metadata":{"note":"contains secret"}}`,
	}
	for _, part := range fragments {
		if _, err := gw.Write([]byte(part)); err != nil {
			if errors.Is(err, guardy.ErrBlocked) {
				fmt.Println("blocked while streaming JSON:", err)
				return
			}
			panic(err)
		}
	}
	if err := gw.Close(); err != nil {
		if errors.Is(err, guardy.ErrBlocked) {
			fmt.Println("blocked on JSON flush:", err)
			return
		}
		panic(err)
	}
	fmt.Println("stream accepted:", out.String())
}
