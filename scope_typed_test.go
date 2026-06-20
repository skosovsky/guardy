package guardy

import (
	"context"
	"errors"
	"testing"
)

func TestTypedScopeRequirement_Success(t *testing.T) {
	t.Parallel()
	// Arrange.
	roleKey := NewScopeKey[string]("principal.role")
	pipeline := NewPipeline(
		WithPolicyValidators(NewTypedAttributeEquals[string, string](roleKey, "admin")),
	)

	// Act.
	result, err := pipeline.Run(context.Background(), NewScope(ScopeValue(roleKey, "admin")), "ok")

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if result.PolicyDecision().Action != ActionPass {
		t.Fatalf("Decision = %+v", result.PolicyDecision())
	}
	requirements := pipeline.RequiredScope()
	if len(requirements) != 1 {
		t.Fatalf("RequiredScope len = %d", len(requirements))
	}
	if requirements[0].Key != "principal.role" || requirements[0].Type != "string" {
		t.Fatalf("RequiredScope = %+v", requirements)
	}
}

func TestTypedScopeRequirement_MissingMetadata(t *testing.T) {
	t.Parallel()
	// Arrange.
	tenantKey := NewScopeKey[int]("tenant.id")
	pipeline := NewPipeline(
		WithPolicyValidators(NewTypedAttributePresent[string, int](tenantKey)),
	)

	// Act.
	_, err := pipeline.Run(context.Background(), MapScope{}, "ok")

	// Assert.
	if !errors.Is(err, ErrScopeIncomplete) {
		t.Fatalf("err = %v", err)
	}
	if got := MissingScopeKeys(err); len(got) != 1 || got[0] != "tenant.id" {
		t.Fatalf("MissingScopeKeys = %v", got)
	}
	if got := MissingScopeRequirements(err); len(got) != 1 || got[0].Key != "tenant.id" || got[0].Type != "int" {
		t.Fatalf("MissingScopeRequirements = %+v", got)
	}
	var scopeErr *ScopeIncompleteError
	if !errors.As(err, &scopeErr) {
		t.Fatal("expected ScopeIncompleteError")
	}
	if scopeErr.MissingRequirements[0].Type != "int" {
		t.Fatalf("MissingRequirements = %+v", scopeErr.MissingRequirements)
	}
}

func TestTypedScopeRequirement_TypeMismatchPolicyDecision(t *testing.T) {
	t.Parallel()
	// Arrange.
	tenantKey := NewScopeKey[int]("tenant.id")
	wrongTypeKey := NewScopeKey[string]("tenant.id")
	pipeline := NewPipeline(
		WithPolicyValidators(NewTypedAttributePresent[string, int](tenantKey)),
	)

	// Act.
	result, err := pipeline.Run(context.Background(), NewScope(ScopeValue(wrongTypeKey, "wrong-type")), "ok")

	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	decision := result.PolicyDecision()
	if !decision.IsTerminal() {
		t.Fatalf("Decision = %+v", decision)
	}
	if decision.Code != CodeAttributeTypeMismatch {
		t.Fatalf("Code = %q", decision.Code)
	}
}
