package guardy

import "context"

// ValidatorPanicError is a Validate callback panic. Its default text is safe;
// trusted diagnostics can explicitly inspect [ValidatorPanicError.PanicValue].
type ValidatorPanicError struct{ value any }

// Error returns a fixed message without formatting caller-controlled panic data.
func (e *ValidatorPanicError) Error() string { return "guardy: validator panicked" }

// PanicValue returns the original panic value. It may contain sensitive data.
func (e *ValidatorPanicError) PanicValue() any { return e.value }

// Unwrap retains an error-valued panic as its original typed cause.
func (e *ValidatorPanicError) Unwrap() error {
	cause, _ := e.value.(error)
	return cause
}

// validateSafely covers Validate (including its middleware), not construction,
// observer, scope or host callbacks. The completion flag also covers panic(nil)
// under legacy GODEBUG=panicnil=1 without mistaking it for a successful return.
//
//nolint:nonamedreturns // Recovery must replace named results before the caller receives them.
func validateSafely[T any](ctx context.Context, v Validator[T], input T) (out T, rep *Report, err error) {
	if nilImplementation(v) {
		return out, nil, configurationError("pipeline", "runtime_wrapper", "nil")
	}
	completed := false
	defer func() {
		if !completed {
			value := recover()
			var zero T
			out, rep, err = zero, nil, &ValidatorPanicError{value: value}
		}
	}()
	out, rep, err = v.Validate(ctx, input)
	completed = true
	return out, rep, err
}
