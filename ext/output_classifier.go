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

// NewTechnicalJSONClassifier classifies JSON tool-call payloads as [guardy.PayloadTechnicalPayload].
// It returns ActionPass and sets PayloadKind so [guardy.WithUserChannel] can block without host-side heuristics.
func NewTechnicalJSONClassifier(opts ...Option) guardy.Validator[string] {
	cfg := applyOptions(RuleConfig{
		Name:     defaultTechnicalJSONClassifierName,
		Code:     "TECHNICAL_JSON",
		Action:   guardy.ActionPass,
		Severity: guardy.SeverityMedium,
	}, opts...)
	return &technicalJSONClassifier{
		cfg: cfg,
		toolKeys: append([]string(nil),
			"tool", "function", "arguments", "tool_calls",
		),
	}
}

func (v *technicalJSONClassifier) Validate(_ context.Context, input string) (string, *guardy.Report, error) {
	trimmed := strings.TrimSpace(input)
	if !looksLikeJSON(trimmed) {
		return input, passReport(v.cfg), nil
	}
	if !hasToolLikeKeys(trimmed, v.toolKeys) {
		return input, passReport(v.cfg), nil
	}
	rep := passReport(v.cfg)
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
