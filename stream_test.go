package guardy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// The old incremental writer tests are migrated to explicit release profiles.
// Close now aborts: every successful producer must explicitly call Complete.
func testStreamConfig(p *Pipeline[string]) StreamConfig {
	return StreamConfig{Identity: "contract", Profile: ReleaseWholeResponse, Pipeline: p,
		Delivery: NewUserTextPolicy("external"), MaxInputBytes: 32768, MaxPendingBytes: 32768,
		MaxUnitBytes: 32768, MaxOutputBytes: 65536, ValidationTimeout: time.Second}
}

func TestStreamPassRedactAndEmpty(t *testing.T) {
	for _, test := range []struct {
		name, input, output string
		action              Action
	}{
		{"pass", "hello", "hello", ActionPass},
		{"mutated_pass", "hello", "HELLO", ActionPass},
		{"redact", "secret", "[safe]", ActionRedact},
		{"empty_redaction", "secret", "", ActionRedact},
		{"cyrillic", "Привет мир", "Привет мир", ActionPass},
		{"emoji", "a😊b", "a😊b", ActionPass},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange.
			rule := ValidatorFunc[string](func(_ context.Context, _ string) (string, *Report, error) {
				return test.output, &Report{Action: test.action}, nil
			})
			var sink bytes.Buffer
			s, err := CompileStream(&sink, testStreamConfig(NewPipeline(WithFastPath(rule))))
			if err != nil {
				t.Fatal(err)
			}
			// Act: even UTF-8 partial bytes remain pending.
			for _, b := range []byte(test.input) {
				if _, err = s.Write([]byte{b}); err != nil {
					t.Fatal(err)
				}
			}
			if sink.Len() != 0 {
				t.Fatal("premature release")
			}
			outcome, err := s.Complete(context.Background())
			// Assert.
			if err != nil || sink.String() != test.output || outcome.ReleasedBytes != int64(len(test.output)) {
				t.Fatalf("%q %+v %v", sink.String(), outcome, err)
			}
			if _, err := s.Write([]byte("later")); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("write after terminal: %v", err)
			}
		})
	}
}

func TestStreamCanonicalDenyRetryAndFault(t *testing.T) {
	for _, rep := range []Report{
		{Action: ActionBlock, Code: "DENY"},
		{Action: ActionRetry, Retryable: true, Code: "CORRECT"},
		{Action: ActionRetry, Retryable: false, Code: "TERMINAL"},
		{Action: ActionPass, Fatal: true, Code: "FATAL"},
		{Action: ActionPass, Disposition: DispositionSystemFault, Code: "FAULT"},
	} {
		// Arrange.
		rule := ValidatorFunc[string](
			func(_ context.Context, s string) (string, *Report, error) { return s, &rep, nil },
		)
		var sink bytes.Buffer
		s, err := CompileStream(&sink, testStreamConfig(NewPipeline(WithFastPath(rule))))
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Write([]byte("secret"))
		if err != nil {
			t.Fatal(err)
		}
		// Act.
		_, err = s.Complete(context.Background())
		var failure *PolicyFailure
		// Assert.
		if err == nil || sink.Len() != 0 || !errors.As(err, &failure) || failure.Decision.Code != rep.Code {
			t.Fatalf("%+v %q %v", failure, sink.String(), err)
		}
		if err2 := s.Close(); !errors.Is(err2, err) {
			t.Fatalf("non-sticky: %v", err2)
		}
	}
}

func TestStreamRequiredScopeBeforeValidation(t *testing.T) {
	// Arrange.
	key := NewScopeKey[string]("destination")
	calls := 0
	policy := NewPolicyFuncWithScope(
		[]ScopeRequirement{key.Requirement()},
		func(_ context.Context, s string, _ ExecutionScope) (string, *Report, error) {
			calls++
			return s, nil, nil
		},
	)
	var sink bytes.Buffer
	s, err := CompileStream(&sink, testStreamConfig(NewPipeline(WithPolicyValidators(policy))))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Write([]byte("secret"))
	// Act.
	_, err = s.Complete(context.Background())
	// Assert.
	if !errors.Is(err, ErrScopeIncomplete) || calls != 0 || sink.Len() != 0 {
		t.Fatalf("%v %d %q", err, calls, sink.String())
	}
}

func TestStreamInvalidConfiguration(t *testing.T) {
	// Arrange / Act / Assert.
	for _, change := range []func(*StreamConfig){
		func(c *StreamConfig) { c.Profile = "" },
		func(c *StreamConfig) { c.MaxPendingBytes = 0 },
		func(c *StreamConfig) { c.ValidationTimeout = -time.Second },
		func(c *StreamConfig) { c.Pipeline = nil },
	} {
		cfg := testStreamConfig(NewPipeline[string]())
		change(&cfg)
		if _, err := CompileStream(io.Discard, cfg); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

func TestStreamBoundedIncrementalUTF8(t *testing.T) {
	// Arrange: best-effort has explicit partial capability and finite output bounds.
	calls := 0
	rule := WithStreamingCapabilities(
		ValidatorFunc[string](func(ctx context.Context, s string) (string, *Report, error) {
			calls++
			if !utf8.ValidString(s) {
				t.Error("split UTF-8 rune")
			}
			if StreamValidationStage(ctx) != StreamPartial {
				t.Error("missing partial stage")
			}
			return s, nil, nil
		}),
		StreamCapabilities{Partial: true},
	)
	cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
	cfg.Profile = ReleaseBestEffort
	cfg.MaxUnitBytes = 1024
	cfg.MaxPendingBytes = 2048
	input := strings.Repeat("😊abc", 2000)
	var sink bytes.Buffer
	s, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	for offset := 0; offset < len(input); {
		end := min(offset+17, len(input))
		if _, err = s.Write([]byte(input[offset:end])); err != nil {
			t.Fatal(err)
		}
		offset = end
	}
	outcome, err := s.Complete(context.Background())
	// Assert: bounded chunks, no per-byte validator degradation.
	if err != nil || sink.String() != input || calls > len(input)/1020+2 ||
		outcome.PeakPendingBytes > cfg.MaxPendingBytes {
		t.Fatalf("calls=%d %+v %v", calls, outcome, err)
	}
}

func TestJSONUnitFramingAcrossEverySplit(t *testing.T) {
	for _, input := range []string{
		`{"name":"Ada"}`,
		`{"note":"braces } [ and escaped \" quote"}`,
		`[{"name":"one"},{"name":"two"}]`,
	} {
		for split := 0; split <= len(input); split++ {
			// Arrange.
			rule := WithStreamingCapabilities(
				ValidatorFunc[string](
					func(_ context.Context, s string) (string, *Report, error) { return s, nil, nil },
				),
				StreamCapabilities{Unit: true},
			)
			cfg := testStreamConfig(NewPipeline(WithFastPath(rule)))
			cfg.Profile = ReleaseValidatedUnits
			cfg.JSONValues = true
			cfg.MaxPendingBytes = cfg.MaxUnitBytes + 1
			cfg.Delivery = NewUserTextPolicy("internal", WithDeliveryAllowedKinds(PayloadTechnicalPayload))
			var sink bytes.Buffer
			s, err := CompileStream(&sink, cfg)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			_, e1 := s.Write([]byte(input[:split]))
			_, e2 := s.Write([]byte(input[split:]))
			_, e3 := s.Complete(context.Background())
			// Assert.
			if e1 != nil || e2 != nil || e3 != nil || sink.String() != input {
				t.Fatalf("%q split=%d: %v %v %v output=%q", input, split, e1, e2, e3, sink.String())
			}
		}
	}
}

func TestJSONIncompleteMalformedAndLimit(t *testing.T) {
	for _, input := range []string{`{"name":`, `{"name":[}]`, strings.Repeat(" ", 65)} {
		// Arrange.
		cfg := testStreamConfig(NewPipeline[string]())
		cfg.Profile = ReleaseValidatedUnits
		cfg.JSONValues = true
		cfg.MaxUnitBytes = 64
		cfg.MaxPendingBytes = 65
		var sink bytes.Buffer
		s, err := CompileStream(&sink, cfg)
		if err != nil {
			t.Fatal(err)
		}
		// Act.
		_, err = s.Write([]byte(input))
		if err == nil {
			_, err = s.Complete(context.Background())
		}
		// Assert.
		if err == nil || sink.Len() != 0 {
			t.Fatalf("input=%q err=%v output=%q", input, err, sink.String())
		}
	}
}

func TestStreamObserverCannotChangeDelivery(t *testing.T) {
	// Arrange.
	cfg := testStreamConfig(NewPipeline[string]())
	cfg.Observer = func(StreamEvent) { panic("observer only") }
	var sink bytes.Buffer
	s, err := CompileStream(&sink, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	_, err = s.Write([]byte("safe"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Complete(context.Background())
	// Assert.
	if err != nil || sink.String() != "safe" {
		t.Fatalf("%q %v", sink.String(), err)
	}
}

var _ io.WriteCloser = (*StreamProcessor)(nil)
