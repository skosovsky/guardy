package guardy

import "context"

// Validator analyzes text and returns a Report.
// Error is reserved for infrastructure failures, not policy decisions.
type Validator interface {
	Name() string
	Validate(ctx context.Context, text string) (Report, error)
}
