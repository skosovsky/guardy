package build_test

import (
	"context"
	"errors"
	"slices"
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

func TestCompileStringGuard_ExplicitPIIAndLength(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{
		LengthMax: 75,
		PIIRedact: true,
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

func TestCompileStringGuard_ExplicitRulesOnly(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{
		PIIRedact: false,
		LengthMax: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Run(context.Background(), nil, "john@example.com long text")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != guardy.ActionPass {
		t.Fatalf("explicit disabled PII/length should pass, action = %v", result.Decision().Action)
	}
}

func TestCompileStringGuard_ExplicitPIIRedaction(t *testing.T) {
	t.Parallel()
	p, err := build.CompileStringGuard(build.GuardSpec{
		PIIRedact: true,
		LengthMax: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Run(context.Background(), nil, "contact john@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision().Action != guardy.ActionRedact {
		t.Fatalf("explicit PIIRedact should redact, action = %v", result.Decision().Action)
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

func TestCompileStringGuardExplicitRuleComposition(t *testing.T) {
	tests := []struct {
		name         string
		spec         build.GuardSpec
		options      []build.CompileOption
		input        string
		scope        guardy.ExecutionScope
		names, codes []string
	}{
		{name: "empty", input: "hello"},
		{
			name:  "PII only",
			spec:  build.GuardSpec{PIIRedact: true},
			input: "hello",
			names: []string{"pii_validator"},
			codes: []string{"PII_DETECTED"},
		},
		{
			name:  "wordlist only",
			spec:  build.GuardSpec{WordlistBlock: []string{"forbidden"}},
			input: "hello",
			names: []string{"wordlist_validator"},
			codes: []string{"WORDLIST_BLOCK"},
		},
		{
			name:  "length only",
			spec:  build.GuardSpec{LengthMax: 100},
			input: "hello",
			names: []string{"length_validator"},
			codes: []string{"LENGTH_EXCEEDED"},
		},
		{
			name:  "combined",
			spec:  build.GuardSpec{PIIRedact: true, WordlistBlock: []string{"forbidden"}, LengthMax: 100},
			input: "hello",
			names: []string{"pii_validator", "wordlist_validator", "length_validator"},
			codes: []string{"PII_DETECTED", "WORDLIST_BLOCK", "LENGTH_EXCEEDED"},
		},
		{
			name:  "redaction composition",
			spec:  build.GuardSpec{PIIRedact: true, WordlistBlock: []string{"forbidden"}, LengthMax: 100},
			input: "user@example.com",
			names: []string{"pii_validator", "wordlist_validator", "length_validator"},
			codes: []string{"PII_DETECTED", "WORDLIST_BLOCK", "LENGTH_EXCEEDED"},
		},
		{
			name: "policy only",
			spec: build.GuardSpec{
				PolicyRules: []build.PolicyRuleSpec{
					{Kind: build.PolicyAttributeEquals, Key: "tenant", Value: "allowed"},
				},
			},
			input: "hello",
			scope: guardy.MapScope{"tenant": "other"},
			names: []string{"typed_attribute_equals"},
			codes: []string{guardy.CodeAttributeMismatch},
		},
		{
			name: "all fast rules",
			spec: build.GuardSpec{PIIRedact: true, WordlistBlock: []string{"forbidden"}, LengthMax: 100},
			options: []build.CompileOption{
				build.WithJSONSchema([]byte(`{"type":"object","required":["message"]}`)),
				build.WithOutputClassifier(),
			},
			input: `{"message":"hello"}`,
			names: []string{
				"pii_validator",
				"wordlist_validator",
				"length_validator",
				"jsonschema",
				"technical_json_classifier",
			},
			codes: []string{
				"PII_DETECTED",
				"WORDLIST_BLOCK",
				"LENGTH_EXCEEDED",
				"JSON_SCHEMA_INVALID",
				"TECHNICAL_JSON",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			pipeline, err := build.CompileStringGuard(tc.spec, tc.options...)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			result, err := pipeline.Run(context.Background(), tc.scope, tc.input)
			// Assert: actual reports prove the selected composition and execution order.
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(result.Reports))
			codes := make([]string, 0, len(result.Reports))
			for _, report := range result.Reports {
				names = append(names, report.Validator)
				codes = append(codes, report.Code)
			}
			if !slices.Equal(names, tc.names) || !slices.Equal(codes, tc.codes) {
				t.Fatalf("names=%v codes=%v expected=%v/%v", names, codes, tc.names, tc.codes)
			}
			if tc.name == "redaction composition" && result.Output != "[REDACTED]" {
				t.Fatalf("output=%q", result.Output)
			}
		})
	}
}
