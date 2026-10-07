//nolint:cyclop // Conformance matrices deliberately cover independent outcomes in one package.
package downstream_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/prompty"

	g "github.com/skosovsky/guardy"
	d "github.com/skosovsky/guardy/integration/downstream"
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

//nolint:cyclop,gocognit,gocyclo // Outcome matrix asserts distinct real boundary failures and observable effects.
func TestRealProducerWholeResponseMatrix(t *testing.T) {
	for _, mode := range []string{"completed", "incomplete", "refusal", "paused", "tool_calls", "unknown", "eof", "producer", "cancel", "observer", "finalizer"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var sink bytes.Buffer
			cause := errors.New("private transport")
			var failure error
			outcome := prompty.Outcome(mode)
			if mode == "eof" {
				outcome = ""
			}
			if mode == "producer" {
				failure = cause
			}
			before := func() {
				if sink.Len() != 0 {
					t.Fatal("whole response escaped before terminal")
				}
				if mode == "cancel" {
					cancel()
				}
			}
			var opts []prompty.StreamOption
			if mode == "observer" {
				outcome = prompty.OutcomeCompleted
				opts = append(
					opts,
					prompty.WithStreamFrameObserver(
						func(context.Context, *prompty.ResponseChunk) error { return cause },
					),
				)
			}
			if mode == "finalizer" {
				outcome = prompty.OutcomeCompleted
				opts = append(
					opts,
					prompty.WithStreamFinalizer(func(context.Context, *prompty.Response) error { return cause }),
				)
			}
			stream := textStream(ctx, outcome, failure, before, opts...)
			// Act.
			result, err := d.ConsumeTextStream(ctx, stream, &sink, textConfig(g.ReleaseWholeResponse))
			// Assert.
			want := d.TextRoute(mode)
			if mode == "unknown" || mode == "eof" || mode == "producer" || mode == "observer" || mode == "finalizer" {
				want = d.TextProducer
			}
			if mode == "cancel" {
				want = d.TextCanceled
			}
			if result.Route != want || !result.Outcome.Terminal {
				t.Fatalf("route=%s err=%v", result.Route, err)
			}
			if mode == "completed" {
				if err != nil || sink.String() != "safe" || result.Outcome.ReleasedBytes != 4 {
					t.Fatalf("completed: %q %v", sink.String(), err)
				}
			} else {
				if err == nil || sink.Len() != 0 || result.Outcome.ReleasedBytes != 0 {
					t.Fatalf("failed leaked: %q %v", sink.String(), err)
				}
			}
			if (mode == "producer" || mode == "observer" || mode == "finalizer") && !errors.Is(err, cause) {
				t.Fatal("producer cause lost")
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost")
			}
			// Act: the actual handle is single use.
			var repeat bytes.Buffer
			_, repeatErr := d.ConsumeTextStream(ctx, stream, &repeat, textConfig(g.ReleaseWholeResponse))
			// Assert.
			if repeatErr == nil || repeat.Len() != 0 {
				t.Fatal("repeated consumption emitted")
			}
		})
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

func TestSinkFailuresAndProgressivePrefixes(t *testing.T) {
	for _, cause := range []error{nil, errors.New("sink failed")} {
		// Arrange.
		sink := &shortSink{cause: cause}
		stream := textStream(context.Background(), prompty.OutcomeCompleted, nil, nil)
		// Act.
		result, err := d.ConsumeTextStream(context.Background(), stream, sink, textConfig(g.ReleaseWholeResponse))
		// Assert.
		want := cause
		if want == nil {
			want = io.ErrShortWrite
		}
		if result.Route != d.TextDelivery || !errors.Is(err, want) || result.Outcome.ReleasedBytes != 2 ||
			sink.calls != 1 ||
			sink.data.String() != "sa" {
			t.Fatalf("transport result=%+v err=%v calls=%d", result, err, sink.calls)
		}
	}
	// Arrange: explicit best-effort may release prior to unsuccessful terminal.
	var sink bytes.Buffer
	cfg := textConfig(g.ReleaseBestEffort)
	cfg.MaxUnitBytes = 6
	stream := textStream(context.Background(), prompty.OutcomeIncomplete, nil, nil)
	// Act.
	result, err := d.ConsumeTextStream(context.Background(), stream, &sink, cfg)
	// Assert.
	if result.Route != d.TextIncomplete || err == nil || sink.String() != "safe" || result.Outcome.ReleasedBytes != 4 {
		t.Fatalf("progressive: %q %+v %v", sink.String(), result, err)
	}
}

func TestUnsupportedContentAndTerminalProcessor(t *testing.T) {
	// Arrange.
	stream := prompty.NewStream(
		context.Background(),
		prompty.StreamNative,
		func(context.Context) iter.Seq2[*prompty.ResponseChunk, error] {
			return func(yield func(*prompty.ResponseChunk, error) bool) {
				yield(
					&prompty.ResponseChunk{
						Kind:     prompty.StreamDelta,
						Identity: prompty.StreamIdentity{PartIDs: []string{"reasoning"}},
						Content:  []prompty.ContentPart{prompty.ReasoningPart{Text: "private"}},
					},
					nil,
				)
			}
		},
	)
	var sink bytes.Buffer
	// Act.
	result, err := d.ConsumeTextStream(context.Background(), stream, &sink, textConfig(g.ReleaseWholeResponse))
	// Assert.
	if result.Route != d.TextUnsupported || err == nil || sink.Len() != 0 {
		t.Fatal("non-text leaked")
	}
	// Arrange: repeated terminal calls cannot flush twice or accept new content.
	processor, err := g.CompileStream(&sink, textConfig(g.ReleaseWholeResponse))
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	_, err = processor.Write([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	first, firstErr := processor.Complete(context.Background())
	second, secondErr := processor.Complete(context.Background())
	_, writeErr := processor.Write([]byte("late"))
	aborted, abortErr := processor.Abort(errors.New("late abort"))
	// Assert.
	if firstErr != nil || secondErr != nil || abortErr != nil || writeErr == nil || first != second ||
		second != aborted ||
		sink.String() != "safe" {
		t.Fatal("terminal was replayed")
	}
}

func TestInvalidConfigurationClosesOwnedProducer(t *testing.T) {
	// Arrange.
	stream := textStream(context.Background(), prompty.OutcomeCompleted, nil, nil)
	var sink bytes.Buffer
	// Act.
	_, err := d.ConsumeTextStream(context.Background(), stream, &sink, g.StreamConfig{})
	// Assert.
	if err == nil || stream.Status().State != prompty.StreamCanceled || sink.Len() != 0 {
		t.Fatalf("invalid configuration leaked handle: status=%+v err=%v", stream.Status(), err)
	}
}
