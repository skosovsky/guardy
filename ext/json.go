package ext

import (
	"context"
	"encoding/json"

	"github.com/skosovsky/guardy"
)

// Ensure JSON implements guardy.Validator at compile time.
var _ guardy.Validator = (*JSON)(nil)

// JSON is a validator that checks text is valid JSON and optionally has required keys.
type JSON struct {
	requiredKeys []string
	action       guardy.Action
	code         string
	name         string
}

// JSONOption configures a JSON validator.
type JSONOption func(*JSON)

// WithJSONName sets the validator name (default "json").
func WithJSONName(name string) JSONOption {
	return func(j *JSON) {
		j.name = name
	}
}

// NewJSON creates a validator that blocks when text is not valid JSON
// or when RequiredKeys is set and the JSON object does not contain all those keys.
func NewJSON(requiredKeys []string, action guardy.Action, code string, opts ...JSONOption) *JSON {
	keys := make([]string, len(requiredKeys))
	copy(keys, requiredKeys)
	j := &JSON{
		requiredKeys: keys,
		action:       action,
		code:         code,
		name:         "json",
	}
	for _, opt := range opts {
		opt(j)
	}
	return j
}

// Validate checks that input.Text is valid JSON and has required keys if set.
// When requiredKeys is empty, any valid JSON (object, array, string, number, etc.) passes.
// When requiredKeys is set, the top-level value must be a JSON object containing all those keys.
func (j *JSON) Validate(ctx context.Context, input guardy.Input) (guardy.Result, error) {
	if ctx.Err() != nil {
		return guardy.Result{}, ctx.Err()
	}
	text := input.Text
	if len(j.requiredKeys) == 0 {
		if !json.Valid([]byte(text)) {
			return guardy.Result{
				Passed: false,
				Action: j.action,
				Code:   j.code,
				Reason: "invalid JSON",
			}, nil
		}
		return guardy.Result{Passed: true, Action: guardy.Pass}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return guardy.Result{
			Passed: false,
			Action: j.action,
			Code:   j.code,
			Reason: "invalid JSON",
		}, nil
	}
	for _, key := range j.requiredKeys {
		if _, ok := m[key]; !ok {
			return guardy.Result{
				Passed: false,
				Action: j.action,
				Code:   j.code,
				Reason: "missing required key: " + key,
			}, nil
		}
	}
	return guardy.Result{Passed: true, Action: guardy.Pass}, nil
}

// Name returns the validator name.
func (j *JSON) Name() string {
	return j.name
}
