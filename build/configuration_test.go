package build_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/build"
)

func TestInvalidBuiltInConfigurationReturnsSafeError(t *testing.T) {
	for _, tc := range []struct {
		name        string
		spec        build.GuardSpec
		options     []build.CompileOption
		field, code string
	}{
		{name: "empty token", spec: build.GuardSpec{WordlistBlock: []string{""}}, field: "WordlistBlock", code: "invalid_token"},
		{name: "multiple tokens", spec: build.GuardSpec{WordlistBlock: []string{"secret value"}}, field: "WordlistBlock", code: "invalid_token"},
		{name: "punctuation", spec: build.GuardSpec{WordlistBlock: []string{"secret!"}}, field: "WordlistBlock", code: "invalid_token"},
		{name: "key", spec: build.GuardSpec{PolicyRules: []build.PolicyRuleSpec{{Kind: build.PolicyAttributePresent}}}, field: "PolicyRules[0].Key", code: "required"},
		{name: "kind", spec: build.GuardSpec{PolicyRules: []build.PolicyRuleSpec{{Kind: build.PolicyRuleKind(-1), Key: "secret", Value: "secret"}}}, field: "PolicyRules[0].Kind", code: "invalid_kind"},
		{name: "negative", spec: build.GuardSpec{LengthMax: -1}, field: "LengthMax", code: "negative"},
		{name: "nil schema", options: []build.CompileOption{build.WithJSONSchema(nil)}, field: "JSONSchema", code: "empty"},
		{name: "empty schema", options: []build.CompileOption{build.WithJSONSchema([]byte{})}, field: "JSONSchema", code: "empty"},
		{name: "invalid schema", options: []build.CompileOption{build.WithJSONSchema([]byte(`{"type":"secret"}`))}, field: "JSONSchema", code: "invalid_schema"},
		{name: "invalid syntax", options: []build.CompileOption{build.WithJSONSchema([]byte(`secret`))}, field: "JSONSchema", code: "invalid_schema"},
		{name: "fallback", options: []build.CompileOption{build.WithUserChannelFallback("secret")}, field: "UserChannelFallback", code: "requires_user_channel"},
		{name: "empty fallback", options: []build.CompileOption{build.WithUserChannelFallback("")}, field: "UserChannelFallback", code: "requires_user_channel"},
		{name: "nil option", options: []build.CompileOption{nil}, field: "options[0]", code: "required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			spec, options := tc.spec, tc.options
			// Act.
			p, err := build.CompileStringGuard(spec, options...)
			// Assert: a panic fails the test directly; no recover masks constructor failures.
			var cfg *guardy.ConfigurationError
			if p != nil || !errors.Is(err, guardy.ErrConfiguration) || !errors.As(err, &cfg) {
				t.Fatalf("pipeline=%v error=%v", p, err)
			}
			if cfg.Component != "build" || cfg.Field != tc.field || cfg.Code != tc.code ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("error=%+v", cfg)
			}
		})
	}
}

func TestValidEmptyAndPermissiveConfiguration(t *testing.T) {
	for _, options := range [][]build.CompileOption{nil, {build.WithJSONSchema([]byte(`{}`))}, {build.WithUserChannelFallback(""), build.WithUserChannel()}} {
		// Arrange.
		spec := build.GuardSpec{LengthMax: 0}
		// Act.
		p, err := build.CompileStringGuard(spec, options...)
		if err != nil {
			t.Fatal(err)
		}
		result, err := p.Run(context.Background(), nil, `{"any":42}`)
		// Assert.
		if err != nil || result.PolicyDecision().IsTerminal() {
			t.Fatalf("%+v %v", result, err)
		}
	}
}

func TestDeclarativeDeepEqualityDoesNotCoerceTypes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want any
		deny      bool
	}{
		{name: "maps", got: map[string]any{"x": []int{1}}, want: map[string]any{"x": []int{1}}},
		{name: "slices", got: []int{1}, want: []int{1}},
		{name: "different types", got: int64(1), want: int(1), deny: true},
		{name: "nil versus empty", got: []int(nil), want: []int{}, deny: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange.
			p, err := build.CompileStringGuard(
				build.GuardSpec{
					PolicyRules: []build.PolicyRuleSpec{
						{Kind: build.PolicyAttributeDeepEqual, Key: "fact", Value: tc.want},
					},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			result, err := p.Run(context.Background(), guardy.MapScope{"fact": tc.got}, "payload")
			// Assert.
			if err != nil || result.PolicyDecision().IsTerminal() != tc.deny {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}
