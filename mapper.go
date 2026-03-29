package guardy

import "context"

// mappedValidator adapts Validator[U] to Validator[T] via extract/inject (Lens pattern).
type mappedValidator[T any, U any] struct {
	inner   Validator[U]
	extract func(T) U
	inject  func(T, U) T
}

// Validate delegates to the inner validator; on ActionRedact applies inject to mutate T.
// MutatedText is cleared at T level (it refers to U, not T) to avoid misleading telemetry.
func (m *mappedValidator[T, U]) Validate(ctx context.Context, input T) (T, *Report, error) {
	subInput := m.extract(input)
	newSub, rep, err := m.inner.Validate(ctx, subInput)
	if err != nil {
		return input, rep, err
	}
	if rep != nil && rep.Action == ActionRedact {
		input = m.inject(input, newSub)
		rep = rep.CloneWithoutState()
	}
	return input, rep, nil
}

// Map returns a Validator[T] that wraps Validator[U] with extract (getter) and inject (setter).
// When the inner validator returns ActionRedact, inject is called to apply the mutation to T.
func Map[T any, U any](v Validator[U], extract func(T) U, inject func(T, U) T) Validator[T] {
	return &mappedValidator[T, U]{
		inner:   v,
		extract: extract,
		inject:  inject,
	}
}
