package ext

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/skosovsky/guardy"
)

const defaultTechnicalJSONClassifierName = "technical_json_classifier"

type technicalJSONClassifier struct {
	cfg      RuleConfig
	toolKeys []string
}

// Ensure technicalJSONClassifier implements guardy.Validator[string] at compile time.
var _ guardy.Validator[string] = (*technicalJSONClassifier)(nil)

// NewTechnicalJSONClassifier heuristically matches tool-like JSON keys and sets
// [guardy.PayloadTechnicalPayload] for [guardy.WithUserChannel]. This signal may
// have false positives/negatives; it is not trusted provenance or authorization.
// Trusted classification must be supplied by a host adapter independently of text.
func NewTechnicalJSONClassifier(opts ...Option) (guardy.Validator[string], error) {
	cfg, err := applyOptions("technical_json", RuleConfig{
		Name:     defaultTechnicalJSONClassifierName,
		Code:     "TECHNICAL_JSON",
		Action:   guardy.ActionPass,
		Severity: guardy.SeverityMedium,
	}, opts...)
	if err != nil {
		return nil, err
	}
	if err := validateRuleOptions("technical_json", cfg); err != nil {
		return nil, err
	}

	return &technicalJSONClassifier{
		cfg: cfg,
		toolKeys: append([]string(nil),
			"tool", "function", "arguments", "tool_calls",
		),
	}, nil
}

// MustTechnicalJSONClassifier panics when NewTechnicalJSONClassifier rejects configuration.
func MustTechnicalJSONClassifier(opts ...Option) guardy.Validator[string] {
	v, err := NewTechnicalJSONClassifier(opts...)
	if err != nil {
		panic(err)
	}
	return v
}

func (v *technicalJSONClassifier) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	trimmed := strings.TrimSpace(input)
	if !looksLikeJSON(trimmed) {
		return input, passReport(v.cfg), nil
	}
	if !hasToolLikeKeys(trimmed, v.toolKeys) {
		return input, passReport(v.cfg), nil
	}
	rep := violationReport(v.cfg, guardy.ActionPass, "tool-like JSON detected")
	rep.PayloadKind = guardy.PayloadTechnicalPayload
	return input, rep, nil
}

func looksLikeJSON(s string) bool {
	if s == "" {
		return false
	}
	switch s[0] {
	case '{', '[':
		return true
	default:
		return false
	}
}

func hasToolLikeKeys(s string, keys []string) bool {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &obj); err == nil {
		for k := range obj {
			if containsToolKey(k, keys) {
				return true
			}
		}
		return false
	}
	lower := strings.ToLower(s)
	for _, key := range keys {
		if strings.Contains(lower, `"`+key+`"`) {
			return true
		}
	}
	return false
}

func containsToolKey(key string, keys []string) bool {
	kl := strings.ToLower(key)
	for _, want := range keys {
		if kl == strings.ToLower(want) {
			return true
		}
	}
	return false
}
