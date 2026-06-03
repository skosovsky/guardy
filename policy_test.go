package guardy

import (
	"context"
	"testing"
)

func TestNewAttributeEquals_BlocksOnMismatch(t *testing.T) {
	t.Parallel()
	pv := NewAttributeEquals[string]("principal.role", "admin", WithPolicyName("role"))
	ctx := WithAttributes(context.Background(), Attributes{"principal.role": "viewer"})
	_, rep, err := pv.Validate(ctx, "hello", Attributes{"principal.role": "viewer"})
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
}

func TestNewAttributeEquals_NoOpWithoutAttrsInCtx(t *testing.T) {
	t.Parallel()
	p := NewPipeline(
		WithPolicyValidators(NewAttributeEquals[string]("principal.role", "admin")),
		WithFastPath(ValidatorFunc[string](func(_ context.Context, input string) (string, *Report, error) {
			return input, &Report{Action: ActionPass, Validator: "pass"}, nil
		})),
	)
	result, err := p.Run(context.Background(), "ok")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != ActionPass {
		t.Fatalf("expected pass, got %v", result.Decision().Action)
	}
}

func TestNewAttributeEquals_MissingKeyUsesMissingCode(t *testing.T) {
	t.Parallel()
	pv := NewAttributeEquals[string]("resource.id", "x")
	_, rep, err := pv.Validate(context.Background(), "in", Attributes{})
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
	ctx := WithAttributes(context.Background(), Attributes{})
	result, err := p.Run(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != ActionBlock {
		t.Fatalf("expected block, got %v", result.Decision().Action)
	}
}
