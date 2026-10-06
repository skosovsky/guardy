package guardy

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestSemanticConstructionFiniteCallerScale(t *testing.T) {
	t.Parallel()
	// Arrange.
	matcher := fakeMatcher{match: func(context.Context, string) (float64, error) { return 7, nil }}
	for _, threshold := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		// Act.
		v, err := NewSemanticValidator(matcher, threshold, false)
		// Assert.
		if v != nil || !errors.Is(err, ErrConfiguration) {
			t.Fatalf("threshold=%v v=%v err=%v", threshold, v, err)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("Must accepted invalid threshold")
				}
			}()
			MustSemanticValidator(matcher, threshold, false)
		}()
	}
	for _, threshold := range []float64{-2, 0, 7, 12} {
		// Act.
		v, err := NewSemanticValidator(matcher, threshold, false)
		// Assert.
		if err != nil || v == nil {
			t.Fatalf("finite threshold=%v err=%v", threshold, err)
		}
	}
	var typedNil *fakeMatcher
	v, err := NewSemanticValidator(typedNil, 7, false)
	if v != nil || !errors.Is(err, ErrConfiguration) {
		t.Fatalf("typed nil v=%v err=%v", v, err)
	}
}
