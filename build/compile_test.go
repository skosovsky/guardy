package build_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/build"
)

func TestCompileStringGuard_WordlistBlock(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{
		WordlistBlock: []string{"bad"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Run(context.Background(), nil, "this is bad")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != guardy.ActionBlock {
		t.Fatalf("action = %v", result.Decision().Action)
	}
}

func TestCompileStringGuard_PolicyRequiresScope(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{
		PolicyRules: []build.PolicyRuleSpec{{
			Kind: build.PolicyAttributePresent,
			Key:  "resource.id",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if keys := p.RequiredScopeKeys(); len(keys) != 1 || keys[0] != "resource.id" {
		t.Fatalf("keys = %v", keys)
	}
	_, err = p.Run(context.Background(), guardy.MapScope{}, "x")
	if !errors.Is(err, guardy.ErrScopeIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompileStringGuard_WithJSONSchema(t *testing.T) {
	t.Parallel()
	schema := []byte(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
	p, err := build.CompileStringGuard(build.GuardSpec{}, build.WithJSONSchema(schema))
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Run(context.Background(), nil, `{"name":"Ada"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != guardy.ActionPass {
		t.Fatalf("action = %v", result.Decision().Action)
	}
}

func TestCompileStringGuard_UserChannelWithClassifier(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(
		build.GuardSpec{},
		build.WithUserChannel(),
		build.WithUserChannelFallback("blocked"),
		build.WithOutputClassifier(),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Run(context.Background(), nil, `{"tool":"search"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Decision().IsTerminalDeny() {
		t.Fatalf("disposition = %v", result.Decision().Disposition)
	}
	if result.OutputKind != guardy.PayloadTechnicalPayload {
		t.Fatalf("OutputKind = %v", result.OutputKind)
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty on user-channel block", result.Output)
	}
}

func TestCompileStringGuard_PIIRedact(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{PIIRedact: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Run(context.Background(), nil, "contact john@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != guardy.ActionRedact {
		t.Fatalf("action = %v", result.Decision().Action)
	}
	if !strings.Contains(result.Output, "[REDACTED]") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestCompileStringGuard_LengthMax(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{LengthMax: 5})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Run(context.Background(), nil, "123456")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Decision().IsTerminalDeny() {
		t.Fatalf("disposition = %v", result.Decision().Disposition)
	}
}

func TestCompileStringGuard_PolicyAttributeEquals(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{
		PolicyRules: []build.PolicyRuleSpec{{
			Kind:  build.PolicyAttributeEquals,
			Key:   "principal.role",
			Value: "admin",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Run(context.Background(), guardy.MapScope{"principal.role": "viewer"}, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Decision().IsTerminalDeny() {
		t.Fatalf("disposition = %v", result.Decision().Disposition)
	}
}

func TestCompileStringGuard_SensitivityStrict(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{
		LengthMax:   100,
		Sensitivity: build.SensitivityStrict,
	})
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", 76)
	result, err := p.Run(context.Background(), nil, long)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Decision().IsTerminalDeny() {
		t.Fatalf("expected length block at 75 max, disposition = %v", result.Decision().Disposition)
	}
}

func TestCompileStringGuard_SensitivityPermissive(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{
		PIIRedact:   true,
		LengthMax:   5,
		Sensitivity: build.SensitivityPermissive,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Run(context.Background(), nil, "john@example.com long text")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != guardy.ActionPass {
		t.Fatalf("permissive should skip PII/length, action = %v", result.Decision().Action)
	}
}

func TestCompileStringGuard_SensitivityNormal(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{
		PIIRedact:   true,
		LengthMax:   100,
		Sensitivity: build.SensitivityNormal,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Run(context.Background(), nil, "contact john@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != guardy.ActionRedact {
		t.Fatalf("normal should keep PIIRedact, action = %v", result.Decision().Action)
	}
}

func TestCompileStringGuard_WithJSONSchema_Invalid(t *testing.T) {
	t.Parallel()
	_, err := build.CompileStringGuard(build.GuardSpec{}, build.WithJSONSchema([]byte(`not json`)))
	if err == nil {
		t.Fatal("expected error for invalid schema bytes")
	}
}

func TestCompileStringGuard_UnknownPolicyRuleKind(t *testing.T) {
	t.Parallel()
	_, err := build.CompileStringGuard(build.GuardSpec{
		PolicyRules: []build.PolicyRuleSpec{{
			Kind: build.PolicyRuleKind(99),
			Key:  "x",
		}},
	})
	if err == nil {
		t.Fatal("expected error for unknown policy rule kind")
	}
}
