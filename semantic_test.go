package guardy

import (
	"context"
	"errors"
	"testing"
)

type fakeMatcher struct {
	match func(context.Context, string) (float64, error)
}

func (f fakeMatcher) Match(ctx context.Context, text string) (float64, error) {
	return f.match(ctx, text)
}

func TestSemanticValidator_NilMatcher_ReturnsError(t *testing.T) {
	v := NewSemanticValidator(nil, 0.5, false)
	_, _, err := v.Validate(context.Background(), "x")
	if !errors.Is(err, errSemanticMatcherNil) {
		t.Fatalf("err = %v, want errSemanticMatcherNil", err)
	}
}

func TestSemanticValidator_BlocksAboveThreshold(t *testing.T) {
	v := NewSemanticValidator(fakeMatcher{match: func(context.Context, string) (float64, error) {
		return 0.9, nil
	}}, 0.5, false)
	_, rep, err := v.Validate(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != ActionBlock || rep.Score != 0.9 {
		t.Fatalf("got %+v", rep)
	}
}

func TestSemanticValidator_ShadowMarksBlock(t *testing.T) {
	v := NewSemanticValidator(fakeMatcher{match: func(context.Context, string) (float64, error) {
		return 0.9, nil
	}}, 0.5, true)
	_, rep, err := v.Validate(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != ActionBlock || !rep.ShadowMode {
		t.Fatalf("got %+v", rep)
	}
}

func TestSemanticValidator_PassAtOrBelowThreshold(t *testing.T) {
	v := NewSemanticValidator(fakeMatcher{match: func(context.Context, string) (float64, error) {
		return 0.5, nil
	}}, 0.5, false)
	_, rep, err := v.Validate(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != ActionPass {
		t.Fatalf("got %+v", rep)
	}
}

func TestSemanticValidator_PropagatesMatcherError(t *testing.T) {
	want := errors.New("matcher down")
	v := NewSemanticValidator(fakeMatcher{match: func(context.Context, string) (float64, error) {
		return 0, want
	}}, 0.5, false)
	_, _, err := v.Validate(context.Background(), "x")
	if err != want {
		t.Fatalf("err = %v", err)
	}
}
