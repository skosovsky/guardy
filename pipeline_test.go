package guardy

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeValidator for tests.
type fakeValidator struct {
	name     string
	validate func(context.Context, string) (Report, error)
}

func (f *fakeValidator) Name() string { return f.name }

func (f *fakeValidator) Validate(ctx context.Context, text string) (Report, error) {
	if f.validate != nil {
		return f.validate(ctx, text)
	}
	return Report{Action: ActionPass, Validator: f.name}, nil
}

func TestPipeline_Empty(t *testing.T) {
	p := NewPipeline()
	ctx := context.Background()
	report, err := p.Run(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != ActionPass {
		t.Errorf("Action = %s, want pass", report.Action)
	}
	if report.MutatedText != "hello" {
		t.Errorf("MutatedText = %q, want hello", report.MutatedText)
	}
}

func TestPipeline_FastPath_Pass(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	report, err := p.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != ActionPass {
		t.Errorf("Action = %s", report.Action)
	}
	if report.MutatedText != "hello" {
		t.Errorf("MutatedText = %q", report.MutatedText)
	}
}

func TestPipeline_FastPath_Block(t *testing.T) {
	v := &fakeValidator{
		name: "blocker",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionBlock, Validator: "blocker", Reason: "bad"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	report, err := p.Run(context.Background(), "bad")
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != ActionBlock {
		t.Errorf("Action = %s, want block", report.Action)
	}
	if report.Validator != "blocker" || report.Reason != "bad" {
		t.Errorf("Validator=%q Reason=%q", report.Validator, report.Reason)
	}
}

func TestPipeline_FastPath_RedactChain(t *testing.T) {
	v1 := &fakeValidator{
		name: "r1",
		validate: func(_ context.Context, text string) (Report, error) {
			if text == "x" {
				return Report{Action: ActionRedact, Validator: "r1", MutatedText: "y"}, nil
			}
			return Report{Action: ActionPass, Validator: "r1"}, nil
		},
	}
	v2 := &fakeValidator{
		name: "r2",
		validate: func(_ context.Context, text string) (Report, error) {
			if text == "y" {
				return Report{Action: ActionRedact, Validator: "r2", MutatedText: "z"}, nil
			}
			return Report{Action: ActionPass, Validator: "r2"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v1, v2))
	report, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != ActionRedact {
		t.Errorf("Action = %s", report.Action)
	}
	if report.MutatedText != "z" {
		t.Errorf("MutatedText = %q, want z (chained redact)", report.MutatedText)
	}
}

func TestPipeline_FastPath_RedactToEmptyText(t *testing.T) {
	wiper := &fakeValidator{
		name: "wiper",
		validate: func(_ context.Context, _ string) (Report, error) {
			return Report{Action: ActionRedact, Validator: "wiper", MutatedText: ""}, nil
		},
	}
	p := NewPipeline(WithFastPath(wiper))
	report, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != ActionRedact {
		t.Errorf("Action = %s, want redact", report.Action)
	}
	if report.MutatedText != "" {
		t.Errorf("MutatedText = %q, want empty string", report.MutatedText)
	}
}

func TestPipeline_FastPath_RedactKeepsValidatorAfterSubsequentPass(t *testing.T) {
	redactor := &fakeValidator{
		name: "redactor",
		validate: func(_ context.Context, _ string) (Report, error) {
			return Report{Action: ActionRedact, Validator: "redactor", Reason: "mutated", MutatedText: "clean"}, nil
		},
	}
	passer := &fakeValidator{
		name: "passer",
		validate: func(_ context.Context, _ string) (Report, error) {
			return Report{Action: ActionPass, Validator: "passer"}, nil
		},
	}
	p := NewPipeline(WithFastPath(redactor, passer))
	rep, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != ActionRedact {
		t.Errorf("Action = %s, want redact", rep.Action)
	}
	if rep.Validator != "redactor" || rep.Reason != "mutated" {
		t.Errorf("Validator=%q Reason=%q, want redactor/mutated", rep.Validator, rep.Reason)
	}
}

func TestPipeline_ShortCircuit(t *testing.T) {
	// First validator blocks immediately; second would sleep 1s. Run must finish in milliseconds.
	blocker := &fakeValidator{
		name: "blocker",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionBlock, Validator: "blocker"}, nil
		},
	}
	sleeper := &fakeValidator{
		name: "sleeper",
		validate: func(ctx context.Context, _ string) (Report, error) {
			select {
			case <-time.After(time.Second):
				return Report{Action: ActionPass, Validator: "sleeper"}, nil
			case <-ctx.Done():
				return Report{}, ctx.Err()
			}
		},
	}
	p := NewPipeline(WithSlowPath(blocker, sleeper))
	start := time.Now()
	report, err := p.Run(context.Background(), "x")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != ActionBlock {
		t.Errorf("Action = %s, want block", report.Action)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Run took %v, should short-circuit in under 500ms", elapsed)
	}
}

func TestPipeline_ShadowMode(t *testing.T) {
	blockShadow := &fakeValidator{
		name: "shadow",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionBlock, Validator: "shadow", ShadowMode: true}, nil
		},
	}
	passer := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithSlowPath(blockShadow, passer))
	report, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	// Shadow block does not win; pipeline returns pass (or last pass).
	if report.Action != ActionPass {
		t.Errorf("Action = %s, want pass (shadow block should not stop pipeline)", report.Action)
	}
}

func TestPipeline_ShadowBlock_CallsObserver(t *testing.T) {
	var calls int32
	shadowBlock := &fakeValidator{
		name: "shadow",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionBlock, ShadowMode: true, Validator: "shadow", Reason: "seen"}, nil
		},
	}
	p := NewPipeline(
		WithObserver(func(ctx context.Context, rep Report) {
			if ctx == nil {
				t.Error("observer must receive non-nil context")
			}
			atomic.AddInt32(&calls, 1)
		}),
		WithFastPath(shadowBlock),
	)
	rep, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != ActionPass {
		t.Errorf("Action = %s, want pass", rep.Action)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("observer calls = %d, want 1", calls)
	}
}

func TestPipeline_WordlistRedact(t *testing.T) {
	v := &fakeValidator{
		name: "wordlist",
		validate: func(_ context.Context, text string) (Report, error) {
			if text == "hello spam world" {
				return Report{Action: ActionRedact, Validator: "wordlist", MutatedText: "hello [REDACTED] world"}, nil
			}
			return Report{Action: ActionPass, Validator: "wordlist"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	report, err := p.Run(context.Background(), "hello spam world")
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != ActionRedact {
		t.Errorf("Action = %s, want redact", report.Action)
	}
	if report.MutatedText != "hello [REDACTED] world" {
		t.Errorf("MutatedText = %q, want hello [REDACTED] world", report.MutatedText)
	}
}

func TestPipeline_ValidatorError(t *testing.T) {
	errFail := errors.New("validator failed")
	v := &fakeValidator{
		name: "fail",
		validate: func(context.Context, string) (Report, error) {
			return Report{}, errFail
		},
	}
	p := NewPipeline(WithFastPath(v))
	_, err := p.Run(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrValidatorFailed) {
		t.Errorf("expected ErrValidatorFailed in chain, got %v", err)
	}
}

func TestPipeline_SlowPath_BlockCancelsOthers(t *testing.T) {
	var runCount atomic.Int32
	v1 := &fakeValidator{
		name: "v1",
		validate: func(ctx context.Context, _ string) (Report, error) {
			runCount.Add(1)
			<-ctx.Done()
			return Report{}, ctx.Err()
		},
	}
	v2 := &fakeValidator{
		name: "v2",
		validate: func(context.Context, string) (Report, error) {
			runCount.Add(1)
			return Report{Action: ActionBlock, Validator: "v2"}, nil
		},
	}
	p := NewPipeline(WithSlowPath(v1, v2))
	report, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != ActionBlock || report.Validator != "v2" {
		t.Errorf("report = %+v", report)
	}
	// v1 may or may not have run before v2 blocked; both run in parallel
	if runCount.Load() < 1 {
		t.Error("at least one validator should have run")
	}
}

func TestPipeline_SlowPath_InvalidActionReturnsError(t *testing.T) {
	badSlow := &fakeValidator{
		name: "bad",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionRedact, Validator: "bad", MutatedText: "x"}, nil
		},
	}
	p := NewPipeline(WithSlowPath(badSlow))
	_, err := p.Run(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error for redact in slow-path")
	}
	if !errors.Is(err, ErrValidatorFailed) {
		t.Errorf("err = %v, want ErrValidatorFailed", err)
	}
}

func TestPipeline_SlowPath_BlockPriorityOverError(t *testing.T) {
	errInfra := errors.New("infra failure")
	blocker := &fakeValidator{
		name: "blocker",
		validate: func(context.Context, string) (Report, error) {
			return Report{Action: ActionBlock, Validator: "blocker", Reason: "policy"}, nil
		},
	}
	failer := &fakeValidator{
		name: "failer",
		validate: func(context.Context, string) (Report, error) {
			return Report{}, errInfra
		},
	}
	p := NewPipeline(WithSlowPath(blocker, failer))
	report, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatalf("expected block to win, got err: %v", err)
	}
	if report.Action != ActionBlock || report.Validator != "blocker" {
		t.Errorf("report = %+v, want block from blocker", report)
	}
}
