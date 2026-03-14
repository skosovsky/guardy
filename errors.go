package guardy

import "errors"

var (
	// ErrBlocked is returned when the pipeline result is Block (e.g. by GuardWriter).
	ErrBlocked = errors.New("guardy: input blocked")

	// ErrValidatorFailed is returned when a validator returns a system error.
	ErrValidatorFailed = errors.New("guardy: validator failed")
)
