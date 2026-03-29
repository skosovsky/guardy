// Streaming filter: validate token stream with GuardWriter.
// When a forbidden word (Wordlist) appears in a chunk, the pipeline Blocks and GuardWriter returns ErrBlocked.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

const exampleChunkSize = 64

func main() {
	wordlistV := ext.NewWordlistValidator([]string{"forbidden", "blocked"}, ext.Blocklist, ext.WithCode("FORBIDDEN"))
	pipeline := guardy.NewPipeline(guardy.WithFastPath(wordlistV))

	mockStream := "Hello world this is forbidden content here."
	var out bytes.Buffer
	gw := guardy.NewGuardWriter(&out, pipeline, guardy.WithChunkSize(exampleChunkSize))

	_ = context.Background()
	for token := range strings.FieldsSeq(mockStream) {
		_, err := gw.Write([]byte(token + " "))
		if err != nil {
			if errors.Is(err, guardy.ErrBlocked) {
				fmt.Println("Stream blocked: forbidden word detected")
				return
			}
			fmt.Println("Error:", err)
			return
		}
	}
	if err := gw.Close(); err != nil {
		if errors.Is(err, guardy.ErrBlocked) {
			fmt.Println("Stream blocked on close")
			return
		}
		fmt.Println("Close error:", err)
		return
	}
	fmt.Println("Stream OK:", out.String())
}
