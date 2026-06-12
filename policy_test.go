package guardy

import (
	"context"
	"errors"
	"testing"
)

func TestNewAttributeEquals_BlocksOnMismatch(t *testing.T) {
	t.Parallel()
	pv := NewAttributeEquals[string]("principal.role", "admin", WithPolicyName("role"))
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

func TestNewAttributeEquals_MissingKeyUsesMissingCode(t *testing.T) {
	t.Parallel()
	pv := NewAttributeEquals[string]("resource.id", "x")
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
	p := NewPipeline(
		WithPolicyValidators(NewAttributePresent[string]("resource.id")),
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
	p := NewPipeline(
		WithPolicyValidators(NewAttributePresent[string]("resource.id")),
	)
	_, err := p.Run(context.Background(), MapScope{}, "x")
	if !errors.Is(err, ErrScopeIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewPolicyFunc_RequiredScopeKeys(t *testing.T) {
	t.Parallel()
	pv := NewPolicyFunc[string]([]string{"tenant.id"}, func(
		_ context.Context,
		input string,
		_ ExecutionScope,
	) (string, *Report, error) {
		return input, &Report{Action: ActionPass, Validator: "custom"}, nil
	})
	if got := pv.RequiredScopeKeys(); len(got) != 1 || got[0] != "tenant.id" {
		t.Fatalf("keys = %v", got)
	}
	p := NewPipeline(WithPolicyValidators(pv))
	if keys := p.RequiredScopeKeys(); len(keys) != 1 || keys[0] != "tenant.id" {
		t.Fatalf("pipeline keys = %v", keys)
	}
}

func TestPipeline_NewPolicyFunc_FailClosedMissingScope(t *testing.T) {
	t.Parallel()
	pv := NewPolicyFunc[string]([]string{"tenant.id"}, func(
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
