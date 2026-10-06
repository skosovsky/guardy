// JSON streaming: explicitly buffer and validate the final JSON value before release.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

const streamLimitBytes = 4096

func main() {
	validator, err := ext.NewRegexValidator(`(?i)secret`, ext.WithCode("SECRET_IN_JSON"))
	if err != nil {
		panic(err)
	}

	pipeline := guardy.MustNewPipeline(guardy.WithSequential(validator))
	var out bytes.Buffer
	gw, err := guardy.CompileStream(&out, guardy.StreamConfig{
		Identity:   "json-response",
		Profile:    guardy.ReleaseWholeResponse,
		Pipeline:   pipeline,
		JSONValues: true,
		Delivery: guardy.NewUserTextPolicy(
			"internal",
			guardy.WithDeliveryAllowedKinds(guardy.PayloadTechnicalPayload),
		),
		MaxInputBytes:     streamLimitBytes,
		MaxPendingBytes:   streamLimitBytes,
		MaxUnitBytes:      streamLimitBytes,
		MaxOutputBytes:    streamLimitBytes,
		ValidationTimeout: time.Second,
	})
	if err != nil {
		panic(err)
	}

	fragments := []string{
		`{"tool_calls":[{"name":"lookup","arguments":"user: `,
		`alice"}],`,
		`"metadata":{"note":"contains secret"}}`,
	}
	for _, part := range fragments {
		if _, err := gw.Write([]byte(part)); err != nil {
			if failure, ok := errors.AsType[*guardy.PolicyFailure](err); ok {
				fmt.Println("blocked while streaming JSON:", failure.Decision.Code)
				return
			}
			if errors.Is(err, guardy.ErrBlocked) {
				fmt.Println("blocked while streaming JSON:", err)
				return
			}
			panic(err)
		}
	}
	if _, err := gw.Complete(context.Background()); err != nil {
		if failure, ok := errors.AsType[*guardy.PolicyFailure](err); ok {
			fmt.Println("blocked on JSON flush:", failure.Decision.Code)
			return
		}
		if errors.Is(err, guardy.ErrBlocked) {
			fmt.Println("blocked on JSON flush:", err)
			return
		}
		panic(err)
	}
	fmt.Println("stream accepted:", out.String())
}
