package guardy

import "context"

// ValidationPhase identifies which pipeline phase is currently executing.
type ValidationPhase string

// Known pipeline phases for telemetry/middleware.
const (
	ValidationPhaseFast ValidationPhase = "fast"
	// ValidationPhasePolicy identifies scope-aware policy validators.
	ValidationPhasePolicy ValidationPhase = "policy"
	ValidationPhaseSlow   ValidationPhase = "slow"
)

type validationPhaseKey struct{}

func withValidationPhase(ctx context.Context, phase ValidationPhase) context.Context {
	return context.WithValue(ctx, validationPhaseKey{}, phase)
}

// ValidationPhaseFromContext extracts the current validation phase from context.
func ValidationPhaseFromContext(ctx context.Context) (ValidationPhase, bool) {
	phase, ok := ctx.Value(validationPhaseKey{}).(ValidationPhase)
	return phase, ok
}
