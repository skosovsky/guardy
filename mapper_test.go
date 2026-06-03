package guardy

import (
	"context"
	"testing"
)

func TestMap_PanicsOnNilExtract(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil extract")
		}
	}()
	_ = Map(
		ValidatorFunc[string](func(context.Context, string) (string, *Report, error) {
			return "", nil, nil
		}),
		nil,
		func(s string, _ string) string { return s },
	)
}

func TestMap_PanicsOnNilInject(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil inject")
		}
	}()
	_ = Map(
		ValidatorFunc[string](func(context.Context, string) (string, *Report, error) {
			return "", nil, nil
		}),
		func(s string) string { return s },
		nil,
	)
}
