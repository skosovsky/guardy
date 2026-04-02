package guardy

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestWrapInput_BlockSkipsNext(t *testing.T) {
	t.Parallel()
	var nextCalls atomic.Int32
	v := &fakeValidator{
		name: "block",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionBlock, Validator: "block", Reason: "nope"}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	wrapped := WrapInput(p, func(_ context.Context, s string) (string, error) {
		nextCalls.Add(1)
		return s, nil
	})
	_, err := wrapped(context.Background(), "x")
	if nextCalls.Load() != 0 {
		t.Fatalf("next called %d times, want 0", nextCalls.Load())
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v, want ErrBlocked", err)
	}
}

func TestWrapInput_RetryReturnsRetryError(t *testing.T) {
	t.Parallel()
	v := &fakeValidator{
		name: "retry",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{
				Action: ActionRetry, Validator: "retry", Feedback: "fix it",
			}, nil
		},
	}
	p := NewPipeline(WithFastPath(v))
	wrapped := WrapInput(p, func(_ context.Context, _ string) (string, error) {
		t.Error("next must not run on retry")
		return "", errors.New("unexpected next")
	})
	_, err := wrapped(context.Background(), "x")
	var re *RetryError
	if !errors.As(err, &re) {
		t.Fatalf("errors.As RetryError: %v", err)
	}
	if re.Feedback != "fix it" {
		t.Errorf("Feedback = %q", re.Feedback)
	}
	if re.Report.Action != ActionRetry {
		t.Errorf("Report.Action = %v", re.Report.Action)
	}
	if !errors.Is(err, ErrRetryRequested) {
		t.Errorf("errors.Is ErrRetryRequested: %v", err)
	}
}

func TestWrapInput_PassAndRedact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("pass", func(t *testing.T) {
		t.Parallel()
		v := &fakeValidator{
			name: "pass",
			validate: func(_ context.Context, text string) (string, *Report, error) {
				return text, &Report{Action: ActionPass, Validator: "pass"}, nil
			},
		}
		p := NewPipeline(WithFastPath(v))
		wrapped := WrapInput(p, func(_ context.Context, s string) (string, error) {
			return "out:" + s, nil
		})
		got, err := wrapped(ctx, "in")
		if err != nil {
			t.Fatal(err)
		}
		if got != "out:in" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("redact", func(t *testing.T) {
		t.Parallel()
		v := &fakeValidator{
			name: "redact",
			validate: func(_ context.Context, _ string) (string, *Report, error) {
				return "clean", &Report{
					Action: ActionRedact, Validator: "redact", MutatedText: "clean",
				}, nil
			},
		}
		p := NewPipeline(WithFastPath(v))
		wrapped := WrapInput(p, func(_ context.Context, s string) (string, error) {
			if s != "clean" {
				t.Fatalf("next got %q, want clean", s)
			}
			return "done", nil
		})
		got, err := wrapped(ctx, "dirty")
		if err != nil {
			t.Fatal(err)
		}
		if got != "done" {
			t.Errorf("got %q", got)
		}
	})
}

func TestWrapOutput_BlockAfterNext(t *testing.T) {
	t.Parallel()
	var nextCalls atomic.Int32
	inV := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "in"}, nil
		},
	}
	outV := &fakeValidator{
		name: "block",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionBlock, Validator: "out", Reason: "bad output"}, nil
		},
	}
	inP := NewPipeline(WithFastPath(inV))
	outP := NewPipeline(WithFastPath(outV))
	wrapped := WrapOutput(outP, WrapInput(inP, func(_ context.Context, s string) (string, error) {
		nextCalls.Add(1)
		return "llm:" + s, nil
	}))
	_, err := wrapped(context.Background(), "prompt")
	if nextCalls.Load() != 1 {
		t.Fatalf("next calls = %d, want 1", nextCalls.Load())
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v", err)
	}
}

func TestWrapOutput_Pass(t *testing.T) {
	t.Parallel()
	inV := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "in"}, nil
		},
	}
	outV := &fakeValidator{
		name: "pass2",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "out"}, nil
		},
	}
	inP := NewPipeline(WithFastPath(inV))
	outP := NewPipeline(WithFastPath(outV))
	wrapped := WrapOutput(outP, WrapInput(inP, func(_ context.Context, _ string) (string, error) {
		return "resp", nil
	}))
	got, err := wrapped(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if got != "resp" {
		t.Errorf("got %q", got)
	}
}

func TestWrapInput_PipelineErrorSkipsNext(t *testing.T) {
	t.Parallel()
	var nextCalls atomic.Int32
	v := &fakeValidator{
		name: "fail",
		validate: func(_ context.Context, _ string) (string, *Report, error) {
			return "", nil, errors.New("validate boom")
		},
	}
	p := NewPipeline(WithFastPath(v))
	wrapped := WrapInput(p, func(_ context.Context, _ string) (string, error) {
		nextCalls.Add(1)
		return "", nil
	})
	_, err := wrapped(context.Background(), "x")
	if nextCalls.Load() != 0 {
		t.Fatalf("next called %d times, want 0", nextCalls.Load())
	}
	if !errors.Is(err, ErrValidatorFailed) {
		t.Fatalf("err = %v, want wrap ErrValidatorFailed", err)
	}
}

func TestWrapOutput_RetryAfterNext(t *testing.T) {
	t.Parallel()
	var nextCalls atomic.Int32
	inV := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "in"}, nil
		},
	}
	outV := &fakeValidator{
		name: "retry",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{
				Action: ActionRetry, Validator: "out", Feedback: "rewrite",
			}, nil
		},
	}
	inP := NewPipeline(WithFastPath(inV))
	outP := NewPipeline(WithFastPath(outV))
	wrapped := WrapOutput(outP, WrapInput(inP, func(_ context.Context, _ string) (string, error) {
		nextCalls.Add(1)
		return "model out", nil
	}))
	_, err := wrapped(context.Background(), "in")
	if nextCalls.Load() != 1 {
		t.Fatalf("next calls = %d, want 1", nextCalls.Load())
	}
	var re *RetryError
	if !errors.As(err, &re) || re.Feedback != "rewrite" {
		t.Fatalf("err = %v", err)
	}
}

func TestWrapOutput_RedactAfterNext(t *testing.T) {
	t.Parallel()
	inV := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "in"}, nil
		},
	}
	outV := &fakeValidator{
		name: "redact",
		validate: func(_ context.Context, _ string) (string, *Report, error) {
			return "clean-out", &Report{
				Action: ActionRedact, Validator: "out", MutatedText: "clean-out",
			}, nil
		},
	}
	inP := NewPipeline(WithFastPath(inV))
	outP := NewPipeline(WithFastPath(outV))
	wrapped := WrapOutput(outP, WrapInput(inP, func(_ context.Context, _ string) (string, error) {
		return "dirty-out", nil
	}))
	got, err := wrapped(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if got != "clean-out" {
		t.Errorf("got %q, want clean-out", got)
	}
}

func TestWrapOutput_PipelineErrorAfterNext(t *testing.T) {
	t.Parallel()
	var nextCalls atomic.Int32
	inV := &fakeValidator{
		name: "pass",
		validate: func(_ context.Context, text string) (string, *Report, error) {
			return text, &Report{Action: ActionPass, Validator: "in"}, nil
		},
	}
	outV := &fakeValidator{
		name: "fail",
		validate: func(_ context.Context, _ string) (string, *Report, error) {
			return "", nil, errors.New("output validate boom")
		},
	}
	inP := NewPipeline(WithFastPath(inV))
	outP := NewPipeline(WithFastPath(outV))
	wrapped := WrapOutput(outP, WrapInput(inP, func(_ context.Context, _ string) (string, error) {
		nextCalls.Add(1)
		return "ok", nil
	}))
	_, err := wrapped(context.Background(), "in")
	if nextCalls.Load() != 1 {
		t.Fatalf("next calls = %d, want 1", nextCalls.Load())
	}
	if !errors.Is(err, ErrValidatorFailed) {
		t.Fatalf("err = %v, want wrap ErrValidatorFailed", err)
	}
}
