package guardy

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type hookCounterContextKey struct{}

type countedPostBindArgs struct {
	Amount int `json:"amount"`
}

func (v *countedPostBindArgs) ValidatePostBind(ctx context.Context) error {
	if counter, ok := ctx.Value(hookCounterContextKey{}).(*atomic.Int64); ok {
		counter.Add(1)
	}
	if v.Amount <= 0 {
		return errors.New("nonpositive amount")
	}
	return nil
}

func checkPostBindForm[T any](t *testing.T, raw string, permitted bool, expectedHooks int64) Decision {
	t.Helper()
	// Arrange.
	var hooks atomic.Int64
	ctx := context.WithValue(context.Background(), hookCounterContextKey{}, &hooks)
	p := MustCompileArgs[T](NewPipeline[string]())
	calls := 0
	handler := WrapArgs(p, nil, func(context.Context, T) (string, error) { calls++; return "ok", nil })
	// Act.
	_, boundary, err := handler(ctx, raw)
	// Assert.
	if hooks.Load() != expectedHooks {
		t.Fatalf("raw=%s hooks=%d want=%d", raw, hooks.Load(), expectedHooks)
	}
	if permitted {
		if err != nil || calls != 1 || boundary.Decision.Disposition != DispositionNone {
			t.Fatalf("%+v %v calls=%d", boundary, err, calls)
		}
	} else if !errors.Is(err, ErrRetryRequested) || calls != 0 || boundary.Decision.Code != CodePostBindViolation {
		t.Fatalf("%+v %v calls=%d", boundary, err, calls)
	}
	return boundary.Decision
}

func TestPointerValuePostBindFullMatrix(t *testing.T) {
	for _, fixture := range []struct {
		raw     string
		allowed bool
	}{
		{raw: `{"amount":1}`, allowed: true},
		{raw: `{"amount":-1}`, allowed: false},
		{raw: `null`, allowed: false},
	} {
		t.Run(fixture.raw, func(t *testing.T) {
			value := checkPostBindForm[countedPostBindArgs](t, fixture.raw, fixture.allowed, 1)
			hooks := int64(1)
			if fixture.raw == "null" {
				hooks = 0
			}
			pointer := checkPostBindForm[*countedPostBindArgs](t, fixture.raw, fixture.allowed, hooks)
			if pointer.Disposition != value.Disposition || pointer.Code != value.Code {
				t.Fatalf("value=%+v pointer=%+v", value, pointer)
			}
		})
	}
}
