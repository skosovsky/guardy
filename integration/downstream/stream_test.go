package downstream_test

import (
	"bytes"
	"context"
	"iter"
	"strings"
	"time"

	"github.com/skosovsky/prompty"

	g "github.com/skosovsky/guardy"
)

func textStream(
	ctx context.Context,
	outcome prompty.Outcome,
	failure error,
	beforeFinish func(),
	options ...prompty.StreamOption,
) *prompty.Stream {
	return prompty.NewStream(ctx, prompty.StreamNative, func(context.Context) iter.Seq2[*prompty.ResponseChunk, error] {
		return func(yield func(*prompty.ResponseChunk, error) bool) {
			if !yield(
				&prompty.ResponseChunk{
					Kind:     prompty.StreamDelta,
					Identity: prompty.StreamIdentity{PartIDs: []string{"text"}},
					Content:  []prompty.ContentPart{prompty.TextPart{Text: "secret"}},
				},
				nil,
			) {
				return
			}
			if beforeFinish != nil {
				beforeFinish()
			}
			if failure != nil {
				yield(nil, failure)
				return
			}
			if outcome != "" {
				yield(&prompty.ResponseChunk{Kind: prompty.StreamFinish, Outcome: outcome}, nil)
			}
		}
	}, options...)
}

func textConfig(profile g.ReleaseProfile) g.StreamConfig {
	rule := g.ValidatorFunc[string](func(_ context.Context, s string) (string, *g.Report, error) {
		return strings.ReplaceAll(s, "secret", "safe"), &g.Report{Action: g.ActionRedact}, nil
	})
	return g.StreamConfig{
		Identity: "text-release",
		Profile:  profile,
		Pipeline: g.MustNewPipeline(
			g.WithSequential(
				g.WithStreamingCapabilities(rule, g.StreamCapabilities{Partial: true, Unit: true, Final: true}),
			),
		),
		Delivery:          g.NewUserTextPolicy("user"),
		MaxInputBytes:     1024,
		MaxOutputBytes:    1024,
		MaxUnitBytes:      128,
		MaxPendingBytes:   1024,
		ValidationTimeout: time.Second,
	}
}

type shortSink struct {
	data  bytes.Buffer
	calls int
	cause error
}

func (s *shortSink) Write(p []byte) (int, error) {
	s.calls++
	n := min(2, len(p))
	_, _ = s.data.Write(p[:n])
	return n, s.cause
}
