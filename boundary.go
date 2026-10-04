package guardy

import (
	"context"
	"errors"
)

// ScopeFactory constructs current request-local policy facts at every invocation,
// including host-controlled resume. Nil means an empty scope, not a cached verdict.
type ScopeFactory func(context.Context) (ExecutionScope, error)

func (f ScopeFactory) scope(ctx context.Context) (ExecutionScope, error) {
	if err := ctx.Err(); err != nil {
		return nil, validatorFaultError(err)
	}
	if f == nil {
		return NewScope(), nil
	}
	scope, err := f(ctx)
	if err != nil {
		return nil, validatorFaultError(err)
	}
	return scope, nil
}

// Boundary identifies a configured data interception point, not an authorization gate.
type Boundary string

const (
	BoundaryInput       Boundary = "input"
	BoundaryArgs        Boundary = "arguments"
	BoundaryResult      Boundary = "handler_result"
	BoundaryContext     Boundary = "context"
	BoundaryPersistence Boundary = "persistence"
	BoundaryExport      Boundary = "export"
	BoundaryDelivery    Boundary = "delivery"
)

// BoundaryProfile is an immutable compiled declaration of actual adapter coverage.
// Caller owns wiring: declaring coverage does not intercept a remote backend.
type BoundaryProfile struct {
	identity string
	covered  map[Boundary]bool
}

// ErrBoundaryConfiguration identifies a rejected mapping/coverage contract.
var ErrBoundaryConfiguration = errors.New("guardy: invalid boundary configuration")

// BoundaryConfigurationError distinguishes incompatible mappings from unsupported
// mandatory coverage without embedding caller data in public error text.
type BoundaryConfigurationError struct {
	Boundary Boundary
	Code     string
}

func (e *BoundaryConfigurationError) Error() string {
	return ErrBoundaryConfiguration.Error() + ": " + e.Code
}
func (e *BoundaryConfigurationError) Unwrap() error { return ErrBoundaryConfiguration }

// CompileBoundaryProfile rejects unsupported mandatory boundaries before execution.
// Supported must list only boundaries that the integration really enforces.
func CompileBoundaryProfile(identity string, supported, mandatory []Boundary) (*BoundaryProfile, error) {
	if identity == "" {
		return nil, &BoundaryConfigurationError{Boundary: "", Code: "missing_identity"}
	}
	p := &BoundaryProfile{identity: identity, covered: make(map[Boundary]bool)}
	for _, b := range supported {
		if !knownBoundary(b) {
			return nil, &BoundaryConfigurationError{Boundary: b, Code: "incompatible_boundary"}
		}
		p.covered[b] = true
	}
	for _, b := range mandatory {
		if !p.covered[b] {
			return nil, &BoundaryConfigurationError{Boundary: b, Code: "unsupported_mandatory_boundary"}
		}
	}
	return p, nil
}

func knownBoundary(b Boundary) bool {
	switch b {
	case BoundaryInput,
		BoundaryArgs,
		BoundaryResult,
		BoundaryContext,
		BoundaryPersistence,
		BoundaryExport,
		BoundaryDelivery:
		return true
	default:
		return false
	}
}

// Identity is the caller-supplied mapping/configuration fingerprint.
func (p *BoundaryProfile) Identity() string { return p.identity }

// Covers reports only the explicitly configured boundary coverage.
func (p *BoundaryProfile) Covers(b Boundary) bool { return p.covered[b] }

// DeliveryProjection is the only object an adapter should serialize to a consumer.
// It carries no original values, reports, raw payload or correction feedback.
type DeliveryProjection[T any] struct {
	Value    T      `json:"value"`
	Channel  string `json:"channel"`
	Fallback bool   `json:"fallback"`
}

// Projection extracts the permitted consumer representation.
func (o GuardedDelivery[T]) Projection() (DeliveryProjection[T], bool) {
	v, ok := o.DeliverableValue()
	if !ok {
		return DeliveryProjection[T]{}, false
	}
	return DeliveryProjection[T]{Value: v, Channel: o.Channel, Fallback: o.Fallback}, true
}
