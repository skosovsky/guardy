package guardy

import "errors"

var (
	// ErrBlocked is returned when the pipeline result is Block.
	ErrBlocked = errors.New("guardy: input blocked")

	// ErrOverridden is returned when the pipeline result is Override.
	ErrOverridden = errors.New("guardy: input overridden")

	// ErrValidatorFailed is returned when a validator returns a system error.
	ErrValidatorFailed = errors.New("guardy: validator failed")
)
