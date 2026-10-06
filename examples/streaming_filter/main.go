// Streaming filter: validate the whole producer response before any delivery.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

const streamLimitBytes = 4096

func main() {
	wordlistV := ext.MustWordlistValidator([]string{"forbidden", "blocked"}, ext.Blocklist, ext.WithCode("FORBIDDEN"))
	pipeline := guardy.NewPipeline(guardy.WithFastPath(wordlistV))

	mockStream := "Hello world this is forbidden content here."
	var out bytes.Buffer
	gw, err := guardy.CompileStream(&out, guardy.StreamConfig{
		Identity:          "response",
		Profile:           guardy.ReleaseWholeResponse,
		Pipeline:          pipeline,
		Delivery:          guardy.NewUserTextPolicy("external"),
		MaxInputBytes:     streamLimitBytes,
		MaxPendingBytes:   streamLimitBytes,
		MaxUnitBytes:      streamLimitBytes,
		MaxOutputBytes:    streamLimitBytes,
		ValidationTimeout: time.Second,
	})
	if err != nil {
		panic(err)
	}

	_ = context.Background()
	for token := range strings.FieldsSeq(mockStream) {
		_, err := gw.Write([]byte(token + " "))
		if err != nil {
			if failure, ok := errors.AsType[*guardy.PolicyFailure](err); ok {
				fmt.Println("Stream blocked:", failure.Decision.Code, failure.Decision.SafeMessage)
				return
			}
			if errors.Is(err, guardy.ErrBlocked) {
				fmt.Println("Stream blocked: forbidden word detected")
				return
			}
			fmt.Println("Error:", err)
			return
		}
	}
	if _, err := gw.Complete(context.Background()); err != nil {
		if failure, ok := errors.AsType[*guardy.PolicyFailure](err); ok {
			fmt.Println("Stream blocked on close:", failure.Decision.Code)
			return
		}
		if errors.Is(err, guardy.ErrBlocked) {
			fmt.Println("Stream blocked on close")
			return
		}
		fmt.Println("Close error:", err)
		return
	}
	fmt.Println("Stream OK:", out.String())
}
