package guardy_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	g "github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/ext"
)

func streamConfig(p *g.Pipeline[string]) g.StreamConfig {
	return g.StreamConfig{
		Identity:          "test",
		Profile:           g.ReleaseWholeResponse,
		Pipeline:          p,
		MaxInputBytes:     4096,
		MaxPendingBytes:   4096,
		MaxUnitBytes:      4096,
		MaxOutputBytes:    4096,
		ValidationTimeout: time.Second,
		Delivery:          g.NewUserTextPolicy("external"),
	}
}

func TestWholeResponseRedactsEverySplit(t *testing.T) {
	// Arrange: includes multi-byte characters and length-changing PII redaction.
	value := "Привет alice@example.com!"
	for split := 0; split <= len(value); split++ {
		var sink bytes.Buffer
		s, err := g.CompileStream(&sink, streamConfig(g.NewPipeline(g.WithFastPath(ext.MustPIIValidator()))))
		if err != nil {
			t.Fatal(err)
		}
		// Act.
		_, e1 := s.Write([]byte(value[:split]))
		_, e2 := s.Write([]byte(value[split:]))
		if sink.Len() != 0 {
			t.Fatalf("split %d: premature release", split)
		}
		outcome, e3 := s.Complete(context.Background())
		// Assert.
		if e1 != nil || e2 != nil || e3 != nil {
			t.Fatalf("split %d: %v %v %v", split, e1, e2, e3)
		}
		if sink.String() != "Привет [REDACTED]!" {
			t.Fatalf("split %d: %q", split, sink.String())
		}
		if !outcome.Terminal || outcome.ReleasedBytes != int64(sink.Len()) {
			t.Fatalf("%+v", outcome)
		}
	}
}

func TestAbortAndPayloadCompletionCannotFlush(t *testing.T) {
	// Arrange.
	var sink bytes.Buffer
	s, err := g.CompileStream(&sink, streamConfig(g.NewPipeline[string]()))
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	_, err = s.Write([]byte(`{"final":true}`))
	if err != nil {
		t.Fatal(err)
	}
	err = s.Close()
	_, later := s.Write([]byte("tail"))
	_, complete := s.Complete(context.Background())
	// Assert.
	if err == nil || !errors.Is(later, err) || !errors.Is(complete, err) || sink.Len() != 0 {
		t.Fatalf("%v %v %v %q", err, later, complete, sink.String())
	}
	if s.Outcome().Category != g.StreamIncomplete {
		t.Fatalf("%+v", s.Outcome())
	}
}

func TestUnitCapabilityAndSequence(t *testing.T) {
	// Arrange.
	rule := g.ValidatorFunc[string](func(ctx context.Context, s string) (string, *g.Report, error) {
		if g.StreamValidationStage(ctx) != g.StreamUnit {
			t.Error("untrusted/missing stage")
		}
		return strings.ReplaceAll(s, "secret", "[X]"), &g.Report{Action: g.ActionRedact}, nil
	})
	cfg := streamConfig(g.NewPipeline(g.WithFastPath(rule)))
	cfg.Profile = g.ReleaseValidatedUnits
	var sink bytes.Buffer
	// Act: unknown capability rejected before issuing any bytes.
	_, err := g.CompileStream(&sink, cfg)
	// Assert.
	if err == nil || sink.Len() != 0 {
		t.Fatal("unsupported unit profile accepted")
	}
	// Arrange compatible unit-local rule.
	cfg.Pipeline = g.NewPipeline(g.WithFastPath(g.WithStreamingCapabilities(rule, g.StreamCapabilities{Unit: true})))
	s, err := g.CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	_, err = s.Write([]byte("secret\nbenign\n"))
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := s.Complete(context.Background())
	// Assert.
	if err != nil || outcome.Sequence != 2 || sink.String() != "[X]\nbenign\n" {
		t.Fatalf("%+v %v %q", outcome, err, sink.String())
	}
}

func TestStreamBoundsAndFault(t *testing.T) {
	for _, test := range []struct {
		name    string
		guard   g.Validator[string]
		input   string
		pending int
		output  int64
		want    g.StreamCategory
	}{
		{"input", nil, strings.Repeat("x", 33), 32, 32, g.StreamLimit},
		{"whitespace", nil, strings.Repeat(" ", 33), 32, 32, g.StreamLimit},
		{"expanded", g.ValidatorFunc[string](func(_ context.Context, _ string) (string, *g.Report, error) {
			return strings.Repeat("x", 100), &g.Report{Action: g.ActionRedact}, nil
		}), "x", 32, 32, g.StreamLimit},
		{"report_fault", g.ValidatorFunc[string](func(_ context.Context, s string) (string, *g.Report, error) {
			return s, &g.Report{Action: g.ActionBlock, Disposition: g.DispositionSystemFault}, nil
		}), "secret", 32, 32, g.StreamFault},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange.
			p := g.NewPipeline[string]()
			if test.guard != nil {
				p = g.NewPipeline(g.WithFastPath(test.guard))
			}
			cfg := streamConfig(p)
			cfg.MaxPendingBytes = test.pending
			cfg.MaxOutputBytes = test.output
			var sink bytes.Buffer
			s, err := g.CompileStream(&sink, cfg)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			_, err = s.Write([]byte(test.input))
			if err == nil {
				_, err = s.Complete(context.Background())
			}
			// Assert.
			if err == nil || sink.Len() != 0 || s.Outcome().Category != test.want ||
				s.Outcome().PeakPendingBytes > test.pending {
				t.Fatalf("%v %q %+v", err, sink.String(), s.Outcome())
			}
		})
	}
}

type shortSink struct{}

func (shortSink) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestTransportCountAndStickyCompletion(t *testing.T) {
	// Arrange.
	s, err := g.CompileStream(shortSink{}, streamConfig(g.NewPipeline[string]()))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Write([]byte("abcd"))
	// Act.
	outcome, err := s.Complete(context.Background())
	again, err2 := s.Complete(context.Background())
	// Assert.
	if !errors.Is(err, io.ErrShortWrite) || !errors.Is(err2, err) || again != outcome || outcome.ReleasedBytes != 2 ||
		outcome.Category != g.StreamTransport {
		t.Fatalf("%+v %v", outcome, err)
	}
}

func TestCancellationAfterIgnoringValidatorSuppressesDelivery(t *testing.T) {
	// Arrange: cooperative timeout must also be checked after a non-cooperative rule.
	rule := g.ValidatorFunc[string](func(_ context.Context, s string) (string, *g.Report, error) {
		time.Sleep(5 * time.Millisecond)
		return s, &g.Report{Action: g.ActionPass}, nil
	})
	cfg := streamConfig(g.NewPipeline(g.WithFastPath(rule)))
	cfg.ValidationTimeout = time.Millisecond
	var sink bytes.Buffer
	s, err := g.CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Write([]byte("secret"))
	// Act.
	outcome, err := s.Complete(context.Background())
	// Assert.
	if err == nil || sink.Len() != 0 || outcome.Category != g.StreamTimeout {
		t.Fatalf("%+v %v %q", outcome, err, sink.String())
	}
}
