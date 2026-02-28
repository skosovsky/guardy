package guardy

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestPipeline_Empty(t *testing.T) {
	p := NewPipeline()
	ctx := context.Background()
	in := Input{Text: "hello"}
	report, err := p.Run(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalAction != Pass {
		t.Errorf("empty pipeline FinalAction = %s, want Pass", report.FinalAction)
	}
	if report.FinalText != "hello" {
		t.Errorf("FinalText = %q, want hello", report.FinalText)
	}
	if len(report.Results) != 0 {
		t.Errorf("len(Results) = %d, want 0", len(report.Results))
	}
}

func TestPipeline_PassThrough(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	ctx := context.Background()
	report, err := p.Run(ctx, Input{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalAction != Pass {
		t.Errorf("FinalAction = %s", report.FinalAction)
	}
	if len(report.Results) != 1 {
		t.Fatalf("len(Results) = %d", len(report.Results))
	}
	if !report.Results[0].Passed {
		t.Error("result should be Passed")
	}
}

func TestPipeline_SingleBlock(t *testing.T) {
	v := &fakeValidator{
		name: "blocker",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: false, Action: Block, Code: "BAD"}, nil
		},
	}
	p := NewPipeline(WithTier1(v), WithFailFast(true))
	ctx := context.Background()
	report, err := p.Run(ctx, Input{Text: "bad"})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalAction != Block {
		t.Errorf("FinalAction = %s, want Block", report.FinalAction)
	}
	if len(report.Results) != 1 {
		t.Fatalf("len(Results) = %d", len(report.Results))
	}
	if report.Results[0].Code != "BAD" {
		t.Errorf("Code = %s", report.Results[0].Code)
	}
}

func TestPipeline_MultipleRedact(t *testing.T) {
	v1 := &fakeValidator{
		name: "redact1",
		validate: func(_ context.Context, in Input) (Result, error) {
			if in.Text == "phone 123" {
				return Result{Passed: false, Action: Redact, Code: "PII", CleanText: "phone [REDACTED]"}, nil
			}
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	v2 := &fakeValidator{
		name: "redact2",
		validate: func(_ context.Context, in Input) (Result, error) {
			if in.Text == "phone 123" {
				return Result{Passed: false, Action: Redact, Code: "PII2", CleanText: "phone [MASKED]"}, nil
			}
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v1, v2), WithFailFast(false))
	ctx := context.Background()
	report, err := p.Run(ctx, Input{Text: "phone 123"})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalAction != Redact {
		t.Errorf("FinalAction = %s", report.FinalAction)
	}
	// Both validators run in parallel; we apply Redact results in order, so FinalText is one of the CleanTexts
	if report.FinalText != "phone [REDACTED]" && report.FinalText != "phone [MASKED]" {
		t.Errorf("FinalText = %q, want phone [REDACTED] or phone [MASKED]", report.FinalText)
	}
}

func TestPipeline_Override(t *testing.T) {
	v := &fakeValidator{
		name: "override",
		validate: func(context.Context, Input) (Result, error) {
			return Result{
				Passed:       false,
				Action:       Override,
				Code:         "BOT",
				OverrideText: "I am a bot.",
			}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	ctx := context.Background()
	report, err := p.Run(ctx, Input{Text: "who are you?"})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalAction != Override {
		t.Errorf("FinalAction = %s", report.FinalAction)
	}
	if report.OverrideText != "I am a bot." {
		t.Errorf("OverrideText = %q", report.OverrideText)
	}
}

func TestPipeline_Retry(t *testing.T) {
	v := &fakeValidator{
		name: "retry",
		validate: func(context.Context, Input) (Result, error) {
			return Result{
				Passed: false,
				Action: Retry,
				Code:   "HALLUCINATION",
				Reason: "Answer not grounded in documents",
			}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	ctx := context.Background()
	report, err := p.Run(ctx, Input{Text: "..."})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalAction != Retry {
		t.Errorf("FinalAction = %s", report.FinalAction)
	}
	if len(report.Results) != 1 || report.Results[0].Reason != "Answer not grounded in documents" {
		t.Errorf("Reason not preserved: %+v", report.Results)
	}
}

func TestPipeline_ActionPriority_BlockWinsRedact(t *testing.T) {
	vBlock := &fakeValidator{
		name: "block",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: false, Action: Block, Code: "B"}, nil
		},
	}
	vRedact := &fakeValidator{
		name: "redact",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: false, Action: Redact, Code: "R", CleanText: "x"}, nil
		},
	}
	p := NewPipeline(WithTier1(vBlock, vRedact))
	ctx := context.Background()
	report, err := p.Run(ctx, Input{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalAction != Block {
		t.Errorf("FinalAction = %s, want Block", report.FinalAction)
	}
}

func TestPipeline_ContextCancellationBetweenTiers(t *testing.T) {
	tier1Run := false
	tier2Run := false
	v1 := &fakeValidator{
		name: "t1",
		validate: func(context.Context, Input) (Result, error) {
			tier1Run = true
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	v2 := &fakeValidator{
		name: "t2",
		validate: func(context.Context, Input) (Result, error) {
			tier2Run = true
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v1), WithTier2(v2))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Run(ctx, Input{Text: "x"})
	if err == nil {
		t.Error("expected error when context cancelled")
	}
	if tier1Run {
		t.Error("tier1 should not run when ctx already cancelled at start")
	}
	if tier2Run {
		t.Error("tier2 should not run when ctx already cancelled")
	}
}

func TestPipeline_FailOpen_ContinuesOnValidatorError(t *testing.T) {
	vFail := &fakeValidator{
		name: "fail",
		validate: func(context.Context, Input) (Result, error) {
			return Result{}, errors.New("network error")
		},
	}
	vPass := &fakeValidator{
		name: "pass",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(vFail, vPass), WithFailOpen(true))
	ctx := context.Background()
	report, err := p.Run(ctx, Input{Text: "x"})
	if err != nil {
		t.Errorf("FailOpen should not return error: %v", err)
	}
	if report.FinalAction != Pass {
		t.Errorf("FinalAction = %s, want Pass (from successful validator)", report.FinalAction)
	}
	if len(report.Results) != 1 {
		t.Errorf("len(Results) = %d, want 1 (only successful validator)", len(report.Results))
	}
}

func TestPipeline_FailClosed_BlocksOnValidatorError(t *testing.T) {
	vFail := &fakeValidator{
		name: "fail",
		validate: func(context.Context, Input) (Result, error) {
			return Result{}, errors.New("system error")
		},
	}
	p := NewPipeline(WithTier1(vFail), WithFailOpen(false))
	ctx := context.Background()
	report, err := p.Run(ctx, Input{Text: "x"})
	if err == nil {
		t.Error("expected error")
	}
	if report.FinalAction != Block {
		t.Errorf("FailClosed should report Block, got %s", report.FinalAction)
	}
	if !errors.Is(err, ErrValidatorFailed) {
		t.Errorf("expected ErrValidatorFailed in error chain, got %v", err)
	}
}

func TestPipeline_ValidatorPanic(t *testing.T) {
	vPanic := &fakeValidator{
		name: "panicker",
		validate: func(context.Context, Input) (Result, error) {
			panic("validator panic")
		},
	}
	p := NewPipeline(WithTier1(vPanic), WithFailOpen(false))
	_, err := p.Run(context.Background(), Input{Text: "x"})
	if err == nil {
		t.Fatal("expected error when validator panics")
	}
	if !errors.Is(err, ErrValidatorFailed) {
		t.Errorf("expected ErrValidatorFailed in error chain, got %v", err)
	}
}

func TestPipeline_ConditionalValidator_Skips(t *testing.T) {
	called := false
	inner := &fakeValidator{
		name: "inner",
		validate: func(context.Context, Input) (Result, error) {
			called = true
			return Result{Passed: false, Action: Block}, nil
		},
	}
	cv := &ConditionalValidator{
		Validator: inner,
		Predicate: func(in Input) bool { return in.Text == "run" },
	}
	p := NewPipeline(WithTier1(cv))
	ctx := context.Background()
	report, err := p.Run(ctx, Input{Text: "skip"})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("conditional validator should not run when predicate false")
	}
	if report.FinalAction != Pass {
		t.Errorf("FinalAction = %s", report.FinalAction)
	}
}

func TestPipeline_ConcurrentRun(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_, _ = p.Run(ctx, Input{Text: "hello"})
		})
	}
	wg.Wait()
}

func TestPipeline_WithLogger(t *testing.T) {
	v := &fakeValidator{
		name: "v",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	logger := slog.Default()
	p := NewPipeline(WithTier1(v), WithLogger(logger))
	report, err := p.Run(context.Background(), Input{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalAction != Pass {
		t.Errorf("FinalAction = %s", report.FinalAction)
	}
}

func TestPipeline_WithOnResult(t *testing.T) {
	done := make(chan string, 1)
	cb := func(name string, _ Result, _ time.Duration) {
		select {
		case done <- name:
		default:
		}
	}
	v := &fakeValidator{
		name: "myvalidator",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v), WithOnResult(cb))
	_, err := p.Run(context.Background(), Input{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-done:
		if name != "myvalidator" {
			t.Errorf("onResult callback: got %q", name)
		}
	case <-time.After(2 * time.Second):
		t.Error("onResult callback did not run within 2s")
	}
}

func TestPipeline_OnResultPanicDoesNotCrashRun(t *testing.T) {
	panickingCb := func(_ string, _ Result, _ time.Duration) {
		panic("onResult panic test")
	}
	v := &fakeValidator{
		name: "v1",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v), WithOnResult(panickingCb))
	report, err := p.Run(context.Background(), Input{Text: "x"})
	if err != nil {
		t.Fatalf("Run should not return error when onResult panics: %v", err)
	}
	if report.FinalAction != Pass {
		t.Errorf("FinalAction = %s, want Pass", report.FinalAction)
	}
}

func TestPipeline_CrossTierRedactPropagation(t *testing.T) {
	t1Redact := &fakeValidator{
		name: "t1",
		validate: func(_ context.Context, in Input) (Result, error) {
			if in.Text == "foo" {
				return Result{Passed: false, Action: Redact, Code: "R1", CleanText: "bar"}, nil
			}
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	t2SeesT1Output := &fakeValidator{
		name: "t2",
		validate: func(_ context.Context, in Input) (Result, error) {
			if in.Text == "foo" {
				return Result{Passed: false, Action: Block, Code: "T2_FOO"}, nil
			}
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(t1Redact), WithTier2(t2SeesT1Output))
	report, err := p.Run(context.Background(), Input{Text: "foo"})
	if err != nil {
		t.Fatal(err)
	}
	// T1 redacts "foo" -> "bar"; T2 receives "bar" so it must not Block.
	if report.FinalAction == Block {
		t.Errorf("T2 should see redacted text bar, not block: FinalAction = Block")
	}
	if report.FinalText != "bar" {
		t.Errorf("FinalText = %q, want bar", report.FinalText)
	}
}

func TestPipeline_WithTier3_AllTiersRun(t *testing.T) {
	var tier1Run, tier2Run, tier3Run bool
	v1 := &fakeValidator{
		name: "t1",
		validate: func(context.Context, Input) (Result, error) {
			tier1Run = true
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	v2 := &fakeValidator{
		name: "t2",
		validate: func(context.Context, Input) (Result, error) {
			tier2Run = true
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	v3 := &fakeValidator{
		name: "t3",
		validate: func(context.Context, Input) (Result, error) {
			tier3Run = true
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v1), WithTier2(v2), WithTier3(v3))
	report, err := p.Run(context.Background(), Input{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !tier1Run || !tier2Run || !tier3Run {
		t.Errorf("all tiers should run: t1=%v t2=%v t3=%v", tier1Run, tier2Run, tier3Run)
	}
	if report.FinalAction != Pass || len(report.Results) != 3 {
		t.Errorf("FinalAction=%s len(Results)=%d", report.FinalAction, len(report.Results))
	}
}

// TestPipeline_BlockWinsOverrideWhenFailFastFalse ensures that when failFast=false,
// Block from an earlier tier wins over Override from a later tier (priority: Block > Override).
func TestPipeline_BlockWinsOverrideWhenFailFastFalse(t *testing.T) {
	vBlock := &fakeValidator{
		name: "block",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: false, Action: Block, Code: "BLOCK_T1"}, nil
		},
	}
	vOverride := &fakeValidator{
		name: "override",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: false, Action: Override, Code: "OVERRIDE_T2", OverrideText: "replaced"}, nil
		},
	}
	p := NewPipeline(WithTier1(vBlock), WithTier2(vOverride), WithFailFast(false))
	report, err := p.Run(context.Background(), Input{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalAction != Block {
		t.Errorf("FinalAction = %s, want Block (Block from tier1 must win over Override from tier2)", report.FinalAction)
	}
	if report.OverrideText != "" {
		t.Errorf("OverrideText should be empty when Block wins")
	}
}

func BenchmarkPipeline_Tier1Only(b *testing.B) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	ctx := context.Background()
	in := Input{Text: "hello world"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Run(ctx, in)
	}
}

func BenchmarkPipeline_ThreeTiers(b *testing.B) {
	pass := &fakeValidator{
		name:     "pass",
		validate: func(context.Context, Input) (Result, error) { return Result{Passed: true, Action: Pass}, nil },
	}
	p := NewPipeline(WithTier1(pass), WithTier2(pass), WithTier3(pass))
	ctx := context.Background()
	in := Input{Text: "hello"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.Run(ctx, in)
	}
}

func FuzzPipeline_Run(f *testing.F) {
	f.Add([]byte("hello"))
	v := &fakeValidator{
		name: "fuzz",
		validate: func(context.Context, Input) (Result, error) {
			return Result{Passed: true, Action: Pass}, nil
		},
	}
	p := NewPipeline(WithTier1(v))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip("input too large")
		}
		_, _ = p.Run(context.Background(), Input{Text: string(data)})
	})
}
