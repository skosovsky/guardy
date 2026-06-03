// Package jsonredact recursively redacts string leaves in JSON documents.
package jsonredact

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/skosovsky/guardy"
)

// LeafValidator validates or redacts individual string leaves during JSON traversal.
type LeafValidator = guardy.Validator[string]

// JSONRedactValidator walks JSON and applies leafValidator to each string value.
//
//nolint:revive // JSONRedactValidator is the public name in this submodule API.
type JSONRedactValidator struct {
	leafValidator LeafValidator
	name          string
}

// NewJSONRedactValidator creates a validator for JSON text inputs.
func NewJSONRedactValidator(leaf LeafValidator, name string) *JSONRedactValidator {
	if name == "" {
		name = "jsonredact"
	}
	return &JSONRedactValidator{leafValidator: leaf, name: name}
}

// Validate implements [guardy.Validator[string]].
func (v *JSONRedactValidator) Validate(ctx context.Context, input string) (string, *guardy.Report, error) {
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	if !json.Valid([]byte(input)) {
		return input, invalidJSONReport("invalid JSON syntax"), nil
	}
	var root any
	if err := json.Unmarshal([]byte(input), &root); err != nil {
		return input, invalidJSONReport(err.Error()), nil
	}
	var (
		changed bool
		lastRep *guardy.Report
	)
	walkErr := v.walk(ctx, &root, &changed, &lastRep)
	if walkErr != nil {
		return input, nil, walkErr
	}
	if lastRep != nil && (lastRep.Action == guardy.ActionBlock || lastRep.Action == guardy.ActionRetry) {
		return input, lastRep, nil
	}
	out, err := json.Marshal(root)
	if err != nil {
		return input, nil, fmt.Errorf("jsonredact: marshal: %w", err)
	}
	if !changed {
		if lastRep != nil {
			return string(out), lastRep, nil
		}
		return string(out), guardy.FinishReport(&guardy.Report{
			Action: guardy.ActionPass, Validator: v.name,
		}, guardy.ControlSpec{Action: guardy.ActionPass}), nil
	}
	rep := guardy.FinishReport(&guardy.Report{
		Action:      guardy.ActionRedact,
		Validator:   v.name,
		MutatedText: string(out),
	}, guardy.ControlSpec{Action: guardy.ActionRedact})
	if lastRep != nil {
		rep.Code = lastRep.Code
		rep.Severity = lastRep.Severity
	}
	return string(out), rep, nil
}

func invalidJSONReport(feedback string) *guardy.Report {
	return guardy.FinishReport(&guardy.Report{
		Action:   guardy.ActionRetry,
		Code:     guardy.CodeJSONInvalid,
		Reason:   "invalid JSON",
		Feedback: feedback,
	}, guardy.ControlSpec{Action: guardy.ActionRetry})
}

func (v *JSONRedactValidator) walk(ctx context.Context, node *any, changed *bool, lastRep **guardy.Report) error {
	switch val := (*node).(type) {
	case map[string]any:
		for k, child := range val {
			c := child
			if err := v.walk(ctx, &c, changed, lastRep); err != nil {
				return err
			}
			val[k] = c
		}
	case []any:
		for i, child := range val {
			c := child
			if err := v.walk(ctx, &c, changed, lastRep); err != nil {
				return err
			}
			val[i] = c
		}
	case string:
		out, rep, err := v.leafValidator.Validate(ctx, val)
		if err != nil {
			return err
		}
		if rep != nil {
			*lastRep = rep
			switch rep.Action {
			case guardy.ActionBlock, guardy.ActionRetry:
				return nil
			case guardy.ActionRedact:
				*changed = true
				*node = out
			case guardy.ActionPass:
				// no-op
			default:
				return fmt.Errorf("jsonredact: unsupported leaf action %s", rep.Action)
			}
		}
	}
	return nil
}
