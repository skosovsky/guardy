package guardy

import (
	"context"
	"errors"
	"testing"
)

func TestMapScope_Lookup(t *testing.T) {
	t.Parallel()
	s := MapScope{"a": 1}
	v, ok := s.Lookup("a")
	if !ok || v.(int) != 1 {
		t.Fatalf("Lookup(a) = %v, %v", v, ok)
	}
	if _, ok := s.Lookup("missing"); ok {
		t.Fatal("expected missing key")
	}
	var nilScope MapScope
	if _, ok := nilScope.Lookup("x"); ok {
		t.Fatal("nil MapScope should miss")
	}
}

func TestCheckScopeRequirements(t *testing.T) {
	t.Parallel()
	// Arrange.
	requirements := []ScopeRequirement{NewScopeKey[string]("k").Requirement()}
	// Act / Assert.
	if err := checkScopeRequirements(MapScope{"k": "v"}, requirements); err != nil {
		t.Fatal(err)
	}
	if err := checkScopeRequirements(MapScope{}, requirements); !errors.Is(err, ErrScopeIncomplete) {
		t.Fatalf("err = %v", err)
	}
	if err := checkScopeRequirements(MapScope{}, nil); err != nil {
		t.Fatal("empty required keys should pass")
	}
}

func TestMergeRequiredKeys(t *testing.T) {
	t.Parallel()
	got := mergeRequiredKeys([]string{"a"}, []string{"b", "a"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestPipeline_RequiredKeysCompileTime(t *testing.T) {
	t.Parallel()
	resourceKey := NewScopeKey[string]("resource.id")
	p := MustNewPipeline(
		WithPolicyValidators(MustTypedAttributePresent[string, string](resourceKey)),
	)
	if len(p.requiredKeys) != 1 || p.requiredKeys[0] != "resource.id" {
		t.Fatalf("requiredKeys = %v", p.requiredKeys)
	}
	derived := p.MustUse()
	if len(derived.requiredKeys) != 1 {
		t.Fatalf("clone requiredKeys = %v", derived.requiredKeys)
	}
}

func TestPipeline_RequiredScopeKeysPublicAPI(t *testing.T) {
	t.Parallel()
	resourceKey := NewScopeKey[string]("resource.id")
	roleKey := NewScopeKey[string]("principal.role")
	p := MustNewPipeline(
		WithPolicyValidators(
			MustTypedAttributePresent[string, string](resourceKey),
			MustTypedAttributeEquals[string, string](roleKey, "admin"),
		),
	)
	keys := p.RequiredScopeKeys()
	if len(keys) != 2 {
		t.Fatalf("keys = %v", keys)
	}
	if keys[0] != "resource.id" || keys[1] != "principal.role" {
		t.Fatalf("keys = %v", keys)
	}
}

func TestPipeline_Run_FailClosedMissingScope(t *testing.T) {
	t.Parallel()
	resourceKey := NewScopeKey[string]("resource.id")
	p := MustNewPipeline(
		WithPolicyValidators(MustTypedAttributePresent[string, string](resourceKey)),
	)
	_, err := p.Run(context.Background(), MapScope{}, "x")
	if !errors.Is(err, ErrScopeIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestPipeline_Run_ScopePresentPolicyRuns(t *testing.T) {
	t.Parallel()
	resourceKey := NewScopeKey[string]("resource.id")
	p := MustNewPipeline(
		WithPolicyValidators(MustTypedAttributePresent[string, string](resourceKey)),
	)
	result, err := p.Run(context.Background(), MapScope{"resource.id": "r1"}, "x")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != ActionPass {
		t.Fatalf("action = %v", result.Decision().Action)
	}
}
