package guardy

import (
	"context"
	"errors"
	"testing"
)

func TestNewTypedAttributeEquals_BlocksOnMismatch(t *testing.T) {
	t.Parallel()
	roleKey := NewScopeKey[string]("principal.role")
	pv := NewTypedAttributeEquals[string, string](roleKey, "admin", WithPolicyName("role"))
	scope := MapScope{"principal.role": "viewer"}
	_, rep, err := pv.Validate(context.Background(), "hello", scope)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Action != ActionBlock {
		t.Fatalf("expected block, got %+v", rep)
	}
	if rep.Code != CodeAttributeMismatch {
		t.Fatalf("code = %q", rep.Code)
	}
	if rep.Retryable {
		t.Fatal("policy block should not be Retryable by default")
	}
	if rep.Disposition != DispositionTerminalDeny {
		t.Fatalf("disposition = %v", rep.Disposition)
	}
}

func TestNewTypedAttributeEquals_MissingKeyUsesMissingCode(t *testing.T) {
	t.Parallel()
	resourceKey := NewScopeKey[string]("resource.id")
	pv := NewTypedAttributeEquals[string, string](resourceKey, "x")
	_, rep, err := pv.Validate(context.Background(), "in", MapScope{})
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Code != CodeAttributeMissing {
		t.Fatalf("code = %q, want %s", rep.Code, CodeAttributeMissing)
	}
}

func TestPipeline_PolicyBlocks(t *testing.T) {
	t.Parallel()
	resourceKey := NewScopeKey[string]("resource.id")
	p := NewPipeline(
		WithPolicyValidators(NewTypedAttributePresent[string, string](resourceKey)),
	)
	result, err := p.Run(context.Background(), MapScope{"resource.id": "r1"}, "x")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != ActionPass {
		t.Fatalf("expected pass, got %v", result.Decision().Action)
	}
}

func TestPipeline_PolicyBlocksMissingValue(t *testing.T) {
	t.Parallel()
	resourceKey := NewScopeKey[string]("resource.id")
	p := NewPipeline(
		WithPolicyValidators(NewTypedAttributePresent[string, string](resourceKey)),
	)
	_, err := p.Run(context.Background(), MapScope{}, "x")
	if !errors.Is(err, ErrScopeIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewPolicyFuncWithScope_RequiredScope(t *testing.T) {
	t.Parallel()
	tenantKey := NewScopeKey[string]("tenant.id")
	pv := NewPolicyFuncWithScope[string]([]ScopeRequirement{tenantKey.Requirement()}, func(
		_ context.Context,
		input string,
		_ ExecutionScope,
	) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "custom"}, nil
	})
	if got := pv.RequiredScope(); len(got) != 1 || got[0].Key != "tenant.id" || got[0].Type != "string" {
		t.Fatalf("requirements = %+v", got)
	}
	p := NewPipeline(WithPolicyValidators(pv))
	if keys := p.RequiredScopeKeys(); len(keys) != 1 || keys[0] != "tenant.id" {
		t.Fatalf("pipeline keys = %v", keys)
	}
}

func TestPipeline_NewPolicyFunc_FailClosedMissingScope(t *testing.T) {
	t.Parallel()
	tenantKey := NewScopeKey[string]("tenant.id")
	pv := NewPolicyFuncWithScope[string]([]ScopeRequirement{tenantKey.Requirement()}, func(
		_ context.Context,
		input string,
		_ ExecutionScope,
	) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "custom"}, nil
	})
	p := NewPipeline(WithPolicyValidators(pv))
	_, err := p.Run(context.Background(), MapScope{}, "x")
	if !errors.Is(err, ErrScopeIncomplete) {
		t.Fatalf("err = %v", err)
	}
}
