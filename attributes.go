package guardy

import "context"

type attributesKey struct{}

// Attributes holds host-defined key/value context for policy validators.
// Keys are domain-agnostic (for example "principal.role", "resource.id").
type Attributes map[string]any

// WithAttributes stores attrs in ctx for policy validators.
func WithAttributes(ctx context.Context, attrs Attributes) context.Context {
	if attrs == nil {
		attrs = Attributes{}
	}
	return context.WithValue(ctx, attributesKey{}, attrs)
}

// AttributesFromContext returns attributes previously stored with [WithAttributes].
func AttributesFromContext(ctx context.Context) (Attributes, bool) {
	v, ok := ctx.Value(attributesKey{}).(Attributes)
	return v, ok
}
