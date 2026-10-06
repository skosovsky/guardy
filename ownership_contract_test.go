package guardy

import (
	"context"
	"testing"
)

func TestStaticScopeOwnsKeysButBorrowsReachableValues(t *testing.T) {
	// Arrange: serial mutation probes the alias boundary, not a safe reload recipe.
	key := NewScopeKey[map[string]string]("facts")
	value := map[string]string{"role": "reader"}
	bindings := []ScopeBinding{ScopeValue(key, value)}
	scope := NewScope(bindings...)
	// Act.
	bindings[0] = ScopeValue(key, map[string]string{"role": "unbound"})
	value["role"] = "writer"
	bound, ok := key.Lookup(scope)
	// Assert: binding structure was copied, but the original bound value was borrowed.
	if !ok || bound["role"] != "writer" {
		t.Fatal("scope unexpectedly froze values or borrowed binding structure")
	}
}

func TestBoundaryDeclarationDoesNotEnforceAnUnwiredSink(t *testing.T) {
	// Arrange: a declaration intentionally exists without an adapter around this sink.
	profile, err := CompileBoundaryProfile("declared", []Boundary{BoundaryDelivery}, []Boundary{BoundaryDelivery})
	if err != nil {
		t.Fatal(err)
	}
	var delivered string
	sink := func(value string) { delivered = value }
	pipeline := MustNewPipeline(WithSequential(ValidatorFunc[string](
		func(_ context.Context, value string) (string, *Report, error) {
			return value, &Report{Action: ActionBlock}, nil
		},
	)))
	// Act.
	sink("unwired")
	declarationDidNotIntercept := delivered
	delivered = ""
	guarded, guardErr := pipeline.GuardOutput(context.Background(), nil, "secret")
	if value, approved := guarded.DeliverableValue(); approved {
		sink(value)
	}
	// Assert: declaration alone cannot prove coverage; wired adapter actually enforces.
	if !profile.Covers(BoundaryDelivery) || declarationDidNotIntercept != "unwired" || guardErr == nil ||
		delivered != "" {
		t.Fatal("declaration or real guarded sink contract changed")
	}
}
