package guardy

import "context"

// Validator checks input and returns a result.
type Validator interface {
	Validate(ctx context.Context, input Input) (Result, error)
	Name() string
}

// ConditionalValidator wraps a validator with a predicate.
// The inner validator runs only when Predicate(input) returns true.
// If Predicate is nil, the inner validator always runs.
type ConditionalValidator struct {
	Validator Validator
	Predicate func(Input) bool
}

// Validate runs the inner validator only when the predicate returns true.
// Otherwise returns a Pass result.
// Panics if Validator is nil (programmer error; fail-fast).
func (c *ConditionalValidator) Validate(ctx context.Context, input Input) (Result, error) {
	if c.Validator == nil {
		panic("guardy: ConditionalValidator.Validator is nil")
	}
	if c.Predicate != nil && !c.Predicate(input) {
		return Result{Passed: true, Action: Pass}, nil
	}
	return c.Validator.Validate(ctx, input)
}

// Name returns the name of the inner validator.
// Panics if Validator is nil (programmer error; fail-fast).
func (c *ConditionalValidator) Name() string {
	if c.Validator == nil {
		panic("guardy: ConditionalValidator.Validator is nil")
	}
	return c.Validator.Name()
}
