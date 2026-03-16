package guardy

import "context"

// Validator is the generic contract for validating input of type T.
// Returns (possibly mutated) input, a Report pointer, and an error for infrastructure failures.
// Report.Validator should be set by the implementation to identify the validator.
type Validator[T any] interface {
	Validate(ctx context.Context, input T) (T, *Report, error)
}

// ValidatorFunc adapts a function to Validator[T] for use in tests and middleware.
type ValidatorFunc[T any] func(ctx context.Context, input T) (T, *Report, error)

// Validate implements Validator[T].
func (f ValidatorFunc[T]) Validate(ctx context.Context, input T) (T, *Report, error) {
	return f(ctx, input)
}
