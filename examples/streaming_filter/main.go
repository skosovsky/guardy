// Streaming filter: validate token stream with GuardWriter.
// When a forbidden word (Wordlist) appears in a chunk, the pipeline Blocks and GuardWriter returns ErrBlocked.
package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func main() {
	wordlistV := ext.NewWordlist([]string{"forbidden", "blocked"}, ext.Blocklist, guardy.Block, "FORBIDDEN")
	pipeline := guardy.NewPipeline(
		guardy.WithFailFast(true),
		guardy.WithTier1(wordlistV),
	)

	// Mock stream: tokens written one word at a time.
	mockStream := "Hello world this is forbidden content here."
	var out bytes.Buffer
	gw := guardy.NewGuardWriter(&out, pipeline, guardy.WithChunkSize(64))

	ctx := context.Background()
	_ = ctx
	for _, token := range strings.Fields(mockStream) {
		_, err := gw.Write([]byte(token + " "))
		if err != nil {
			if err == guardy.ErrBlocked {
				fmt.Println("Stream blocked: forbidden word detected")
				return
			}
			fmt.Println("Error:", err)
			return
		}
	}
	if err := gw.Close(); err != nil {
		if err == guardy.ErrBlocked {
			fmt.Println("Stream blocked on close")
			return
		}
		fmt.Println("Close error:", err)
		return
	}
	fmt.Println("Stream OK:", out.String())
}
