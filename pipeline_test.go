package guardy

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakeValidator for tests.
type fakeValidator struct {
	name     string
	validate func(context.Context, string) (string, *Report, error)
}

func (f *fakeValidator) Validate(ctx context.Context, text string) (string, *Report, error) {
	if f.validate != nil {
		return f.validate(ctx, text)
	}
	return text, &Report{Action: ActionPass, Validator: f.name}, nil
}

func TestPipeline_Empty(t *testing.T) {
	p := NewPipeline[string]()
	ctx := context.Background()
	result, err := p.Run(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionPass {
		t.Errorf("Action = %v, want pass", rep.Action)
	}
	if result.Output != "hello" {
		t.Errorf("Output = %q, want hello", result.Output)
	}
}

func TestPipeline_FastPath_Pass(t *testing.T) {
	v := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (string, *Report, error) {
			return "hello", &Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	result, err := p.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionPass {
		t.Errorf("Action = %v", rep.Action)
	}
	if result.Output != "hello" {
		t.Errorf("Output = %q", result.Output)
	}
}

func TestPipeline_FastPath_PassMutatesOutput(t *testing.T) {
	t.Parallel()
	v := &fakeValidator{
		name: "mutate",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text + "-mutated", &Report{Action: ActionPass, Validator: "mutate"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	result, err := p.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "hello-mutated" {
		t.Fatalf("Output = %q, want hello-mutated", result.Output)
	}
}

func TestPipeline_FastPath_Block(t *testing.T) {
	v := &fakeValidator{
		name: "blocker",
		validate: func(context.Context, string) (string, *Report, error) {
			return "bad", &Report{Action: ActionBlock, Validator: "blocker", Reason: "bad"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	result, err := p.Run(context.Background(), "bad")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionBlock {
		t.Errorf("Action = %v, want block", rep.Action)
	}
	if rep.Validator != "blocker" || rep.Reason != "bad" {
		t.Errorf("Validator=%q Reason=%q", rep.Validator, rep.Reason)
	}
}

func TestPipeline_FastPath_RedactChain(t *testing.T) {
	v1 := &fakeValidator{
		name: "r1",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			if text == "x" {
				return "y", &Report{Action: ActionRedact, Validator: "r1", MutatedText: "y"}, nil
			}
			return text, &Report{Action: ActionPass, Validator: "r1"}, nil
		},
	}
	v2 := &fakeValidator{
		name: "r2",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			if text == "y" {
				return "z", &Report{Action: ActionRedact, Validator: "r2", MutatedText: "z"}, nil
			}
			return text, &Report{Action: ActionPass, Validator: "r2"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v1, v2))
	result, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionRedact {
		t.Errorf("Action = %v", rep.Action)
	}
	if result.Output != "z" {
		t.Errorf("Output = %q, want z (chained redact)", result.Output)
	}
}

func TestPipeline_FastPath_RedactToEmptyText(t *testing.T) {
	wiper := &fakeValidator{
		name: "wiper",
		validate: func(_ context.Context, _ string) (string, *Report, error) {
			return "", &Report{Action: ActionRedact, Validator: "wiper", MutatedText: ""}, nil
		},
	}
	p := NewPipeline(WithFastPath(wiper))
	result, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionRedact {
		t.Errorf("Action = %v, want redact", rep.Action)
	}
	if result.Output != "" {
		t.Errorf("Output = %q, want empty string", result.Output)
	}
}

func TestPipeline_FastPath_RedactKeepsValidatorAfterSubsequentPass(t *testing.T) {
	redactor := &fakeValidator{
		name: "redactor",
		validate: func(_ context.Context, _ string) (string, *Report, error) {
			rep := &Report{
				Action:      ActionRedact,
				Validator:   "redactor",
				Reason:      "mutated",
				MutatedText: "clean",
			}
			return "clean", rep, nil
		},
	}
	passer := &fakeValidator{
		name: "passer",
		validate: func(_ context.Context, _ string) (string, *Report, error) {
			return "clean", &Report{Action: ActionPass, Validator: "passer"}, nil
		},
	}
	p := NewPipeline(WithFastPath(redactor, passer))
	result, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionRedact {
		t.Errorf("Action = %v, want redact", rep.Action)
	}
	if rep.Validator != "redactor" || rep.Reason != "mutated" {
		t.Errorf("Validator=%q Reason=%q, want redactor/mutated", rep.Validator, rep.Reason)
	}
}

func TestPipeline_ShortCircuit(t *testing.T) {
	blocker := &fakeValidator{
		name: "blocker",
		validate: func(context.Context, string) (string, *Report, error) {
			return "x", &Report{Action: ActionBlock, Validator: "blocker"}, nil
		},
	}
	sleeper := &fakeValidator{
		name: "sleeper",
		validate: func(ctx context.Context, t string) (string, *Report, error) {
			select {
			case <-time.After(time.Second):
				return t, &Report{Action: ActionPass, Validator: "sleeper"}, nil
			case <-ctx.Done():
				return "", nil, ctx.Err()
			}
		},
	}
	p := NewPipeline(WithSlowPath(blocker, sleeper))
	start := time.Now()
	result, err := p.Run(context.Background(), "x")
	elapsed := time.Since(start)
	rep := result.Decision()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != ActionBlock {
		t.Errorf("Action = %v, want block", rep.Action)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Run took %v, should short-circuit in under 500ms", elapsed)
	}
}

func TestPipeline_ShadowMode(t *testing.T) {
	blockShadow := &fakeValidator{
		name: "shadow",
		validate: func(context.Context, string) (string, *Report, error) {
			return "x", &Report{Action: ActionBlock, Validator: "shadow", ShadowMode: true}, nil
		},
	}
	passer := &fakeValidator{
		name: "pass",
		validate: func(context.Context, string) (string, *Report, error) {
			return "x", &Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	p := NewPipeline(WithSlowPath(blockShadow, passer))
	result, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionPass {
		t.Errorf("Action = %v, want pass (shadow block should not stop pipeline)", rep.Action)
	}
}

func TestPipeline_ShadowBlock_CallsObserver(t *testing.T) {
	var calls int32
	shadowBlock := &fakeValidator{
		name: "shadow",
		validate: func(context.Context, string) (string, *Report, error) {
			return "x", &Report{Action: ActionBlock, ShadowMode: true, Validator: "shadow", Reason: "seen"}, nil
		},
	}
	p := NewPipeline(
		WithObserver[string](func(ctx context.Context, _ *Report) {
			if ctx == nil {
				t.Error("observer must receive non-nil context")
			}
			atomic.AddInt32(&calls, 1)
		}),
		WithFastPath(shadowBlock),
	)
	result, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionPass {
		t.Errorf("Action = %v, want pass", rep.Action)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("observer calls = %d, want 1", calls)
	}
}

func TestPipeline_PolicyShadowBlock_CallsObserverAndContinues(t *testing.T) {
	var calls int32
	pass := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "pass"}, nil
		},
	}
	shadowPolicy := PolicyFunc[string](func(_ context.Context, input string, _ Attributes) (string, *Report, error) {
		return input, &Report{
			Action:     ActionBlock,
			ShadowMode: true,
			Validator:  "policy-shadow",
			Reason:     "would block",
		}, nil
	})
	attrs := Attributes{"principal.role": "sales"}
	ctx := WithAttributes(context.Background(), attrs)
	var observedRole string
	p := NewPipeline(
		WithFastPath(pass),
		WithPolicyValidators(shadowPolicy),
		WithObserver[string](func(observerCtx context.Context, _ *Report) {
			atomic.AddInt32(&calls, 1)
			if a, ok := AttributesFromContext(observerCtx); ok {
				if v, ok := a["principal.role"].(string); ok {
					observedRole = v
				}
			}
		}),
	)
	result, err := p.Run(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionPass {
		t.Errorf("Action = %v, want pass (shadow block ignored)", rep.Action)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("observer calls = %d, want 1", calls)
	}
	if observedRole != "sales" {
		t.Errorf("observed role = %q, want sales", observedRole)
	}
}

func TestPipeline_WordlistRedact(t *testing.T) {
	v := &fakeValidator{
		name: "wordlist",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			if text == "hello spam world" {
				out := "hello [REDACTED] world"
				rep := &Report{
					Action:      ActionRedact,
					Validator:   "wordlist",
					MutatedText: out,
				}
				return out, rep, nil
			}
			return text, &Report{Action: ActionPass, Validator: "wordlist"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	result, err := p.Run(context.Background(), "hello spam world")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionRedact {
		t.Errorf("Action = %v, want redact", rep.Action)
	}
	if result.Output != "hello [REDACTED] world" {
		t.Errorf("Output = %q, want hello [REDACTED] world", result.Output)
	}
}

func TestPipeline_ValidatorError(t *testing.T) {
	errFail := errors.New("validator failed")
	v := &fakeValidator{
		name: "fail",
		validate: func(context.Context, string) (string, *Report, error) {
			return "", nil, errFail
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
		validate: func(ctx context.Context, _ string) (string, *Report, error) {
			runCount.Add(1)
			<-ctx.Done()
			return "", nil, ctx.Err()
		},
	}
	v2 := &fakeValidator{
		name: "v2",
		validate: func(context.Context, string) (string, *Report, error) {
			runCount.Add(1)
			return "x", &Report{Action: ActionBlock, Validator: "v2"}, nil
		},
	}
	p := NewPipeline(WithSlowPath(v1, v2))
	result, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionBlock || rep.Validator != "v2" {
		t.Errorf("report = %+v", rep)
	}
	if runCount.Load() < 1 {
		t.Error("at least one validator should have run")
	}
}

func TestPipeline_SlowPath_InvalidActionReturnsError(t *testing.T) {
	badSlow := &fakeValidator{
		name: "bad",
		validate: func(context.Context, string) (string, *Report, error) {
			return "x", &Report{Action: ActionRedact, Validator: "bad", MutatedText: "x"}, nil
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
		validate: func(context.Context, string) (string, *Report, error) {
			return "x", &Report{Action: ActionBlock, Validator: "blocker", Reason: "policy"}, nil
		},
	}
	failer := &fakeValidator{
		name: "failer",
		validate: func(context.Context, string) (string, *Report, error) {
			return "", nil, errInfra
		},
	}
	p := NewPipeline(WithSlowPath(blocker, failer))
	result, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatalf("expected block to win, got err: %v", err)
	}
	rep := result.Decision()
	if rep.Action != ActionBlock || rep.Validator != "blocker" {
		t.Errorf("report = %+v, want block from blocker", rep)
	}
}

// TestPipeline_RetryShortCircuit ensures Retry short-circuits and subsequent validators (e.g. Block) are not called.
func TestPipeline_RetryShortCircuit(t *testing.T) {
	retryV := &fakeValidator{
		name: "retry",
		validate: func(context.Context, string) (string, *Report, error) {
			return "x", &Report{Action: ActionRetry, Validator: "retry", Feedback: "fix it"}, nil
		},
	}
	blockV := &fakeValidator{
		name: "block",
		validate: func(context.Context, string) (string, *Report, error) {
			t.Error("block validator must not be called when retry short-circuits")
			return "x", &Report{Action: ActionBlock, Validator: "block"}, nil
		},
	}
	p := NewPipeline(WithFastPath(retryV, blockV))
	result, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	rep := result.Decision()
	if rep.Action != ActionRetry || rep.Validator != "retry" {
		t.Errorf("Action = %v Validator = %q, want Retry from retry", rep.Action, rep.Validator)
	}
}

// TestPipeline_MiddlewareOrder verifies middleware wraps validators in HTTP-like order (outer → inner → validator).
func TestPipeline_MiddlewareOrder(t *testing.T) {
	var order []string
	outer := func(next Validator[string]) Validator[string] {
		return ValidatorFunc[string](func(ctx context.Context, s string) (string, *Report, error) {
			order = append(order, "outer-before")
			defer func() { order = append(order, "outer-after") }()
			return next.Validate(ctx, s)
		})
	}
	inner := func(next Validator[string]) Validator[string] {
		return ValidatorFunc[string](func(ctx context.Context, s string) (string, *Report, error) {
			order = append(order, "inner-before")
			defer func() { order = append(order, "inner-after") }()
			return next.Validate(ctx, s)
		})
	}
	v := &fakeValidator{
		name: "v",
		validate: func(context.Context, string) (string, *Report, error) {
			order = append(order, "validator")
			return "ok", &Report{Action: ActionPass, Validator: "v"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	p = p.Use(outer, inner)
	_, err := p.Run(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"outer-before", "inner-before", "validator", "inner-after", "outer-after"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
}

// TestRunResult_Decision_BlockOverRetry verifies Block > Retry priority regardless of Reports order.
func TestRunResult_Decision_BlockOverRetry(t *testing.T) {
	blockRep := Report{Action: ActionBlock, Validator: "blocker", Reason: "policy"}
	retryRep := Report{Action: ActionRetry, Validator: "retry", Feedback: "fix it"}
	passRep := Report{Action: ActionPass, Validator: "pass"}

	// Retry first, Block second (nondeterministic slow-path order)
	r1 := RunResult[string]{Output: "x", Reports: []Report{retryRep, blockRep, passRep}}
	if got := r1.Decision(); got.Action != ActionBlock || got.Validator != "blocker" {
		t.Errorf("Decision(Retry,Block,Pass) = %v, want Block from blocker", got)
	}

	// Block first, Retry second
	r2 := RunResult[string]{Output: "x", Reports: []Report{blockRep, retryRep}}
	if got := r2.Decision(); got.Action != ActionBlock || got.Validator != "blocker" {
		t.Errorf("Decision(Block,Retry) = %v, want Block", got)
	}

	// Only Retry
	r3 := RunResult[string]{Output: "x", Reports: []Report{passRep, retryRep}}
	if got := r3.Decision(); got.Action != ActionRetry || got.Validator != "retry" {
		t.Errorf("Decision(Pass,Retry) = %v, want Retry", got)
	}

	// ShadowMode Block should not override Retry (Block ignored)
	shadowBlock := Report{Action: ActionBlock, Validator: "shadow", ShadowMode: true}
	r4 := RunResult[string]{Output: "x", Reports: []Report{shadowBlock, retryRep}}
	if got := r4.Decision(); got.Action != ActionRetry {
		t.Errorf("Decision(ShadowBlock,Retry) = %v, want Retry (shadow block ignored)", got)
	}
}

func TestPipeline_UseImmutable(t *testing.T) {
	v := &fakeValidator{
		name: "v",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "v"}, nil
		},
	}
	var mwCalls atomic.Int32
	mw := func(next Validator[string]) Validator[string] {
		return ValidatorFunc[string](func(ctx context.Context, s string) (string, *Report, error) {
			mwCalls.Add(1)
			return next.Validate(ctx, s)
		})
	}

	original := NewPipeline(WithFastPath(v))
	derived := original.Use(mw)

	if original == derived {
		t.Fatal("Use must return a new pipeline instance")
	}

	if _, err := original.Run(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if got := mwCalls.Load(); got != 0 {
		t.Fatalf("middleware calls for original pipeline = %d, want 0", got)
	}

	if _, err := derived.Run(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if got := mwCalls.Load(); got != 1 {
		t.Fatalf("middleware calls after derived pipeline run = %d, want 1", got)
	}
}

func TestPipeline_PolicyPhaseRunsBetweenFastAndSlow(t *testing.T) {
	t.Parallel()
	var order []string
	fastV := &fakeValidator{
		name: "fast",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			order = append(order, "fast")
			return text, &Report{Action: ActionPass, Validator: "fast"}, nil
		},
	}
	slowV := &fakeValidator{
		name: "slow",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			order = append(order, "slow")
			return text, &Report{Action: ActionPass, Validator: "slow"}, nil
		},
	}
	policyPV := PolicyFunc[string](func(_ context.Context, text string, _ Attributes) (string, *Report, error) {
		order = append(order, "policy")
		return text, &Report{Action: ActionPass, Validator: "policy"}, nil
	})
	p := NewPipeline(
		WithFastPath(fastV),
		WithPolicyValidators(policyPV),
		WithSlowPath(slowV),
	)
	ctx := WithAttributes(context.Background(), Attributes{"k": "v"})
	if _, err := p.Run(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	want := []string{"fast", "policy", "slow"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
}

func TestPipeline_PhaseContext(t *testing.T) {
	var fastSeen, slowSeen atomic.Bool
	fastV := &fakeValidator{
		name: "fast",
		validate: func(ctx context.Context, text string) (string, *Report, error) {
			phase, ok := ValidationPhaseFromContext(ctx)
			if ok && phase == ValidationPhaseFast {
				fastSeen.Store(true)
			}
			return text, &Report{Action: ActionPass, Validator: "fast"}, nil
		},
	}
	slowV := &fakeValidator{
		name: "slow",
		validate: func(ctx context.Context, text string) (string, *Report, error) {
			phase, ok := ValidationPhaseFromContext(ctx)
			if ok && phase == ValidationPhaseSlow {
				slowSeen.Store(true)
			}
			return text, &Report{Action: ActionPass, Validator: "slow"}, nil
		},
	}

	p := NewPipeline(WithFastPath(fastV), WithSlowPath(slowV))
	if _, err := p.Run(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if !fastSeen.Load() {
		t.Fatal("fast phase context was not set")
	}
	if !slowSeen.Load() {
		t.Fatal("slow phase context was not set")
	}
}

func TestPipeline_MapJSONRawMessage_RedactUpdatesOutput(t *testing.T) {
	t.Parallel()
	inner := ValidatorFunc[string](func(_ context.Context, _ string) (string, *Report, error) {
		out := `{"email":"[REDACTED]"}`
		return out, &Report{
			Action:      ActionRedact,
			Validator:   "test",
			MutatedText: out,
		}, nil
	})
	type pipelineToolArgs struct {
		ToolArgs json.RawMessage `json:"tool_args"`
	}
	mapped := MapJSONRawMessage(
		inner,
		func(d *pipelineToolArgs) json.RawMessage { return d.ToolArgs },
		func(d *pipelineToolArgs, raw json.RawMessage) *pipelineToolArgs {
			d.ToolArgs = raw
			return d
		},
	)
	p := NewPipeline[pipelineToolArgs](WithFastPath(mapped))
	in := pipelineToolArgs{ToolArgs: json.RawMessage(`{"email":"a@b.com"}`)}
	result, err := p.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != ActionRedact {
		t.Fatalf("decision = %+v", result.Decision())
	}
	if !json.Valid(result.Output.ToolArgs) {
		t.Fatalf("invalid JSON: %s", result.Output.ToolArgs)
	}
	if string(result.Output.ToolArgs) != `{"email":"[REDACTED]"}` {
		t.Fatalf("ToolArgs = %s", result.Output.ToolArgs)
	}
}
