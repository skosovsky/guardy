// Package jsonredact recursively redacts string leaves in JSON documents.
package jsonredact

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/internal/jsondoc"
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
// It panics on a nil (including typed nil) leaf validator at configuration time.
func NewJSONRedactValidator(leaf LeafValidator, name string) *JSONRedactValidator {
	if leaf == nil || isNilLeaf(leaf) {
		panic("jsonredact: nil leaf validator")
	}
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
	root, err := jsondoc.Decode(input)
	if err != nil {
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
	if guardy.DecisionFromReport(lastRep).Disposition != guardy.DispositionNone {
		return input, lastRep, nil
	}
	out, err := json.Marshal(root)
	if err != nil {
		return input, nil, fmt.Errorf("jsonredact: marshal: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	if !changed {
		if lastRep != nil {
			return string(out), lastRep, nil
		}
		return string(out), guardy.FinishReport(&guardy.Report{
			Action: guardy.ActionPass, Validator: v.name,
		}, guardy.ControlSpec{Action: guardy.ActionPass}), nil
	}
	rep := lastRep.CloneWithoutState()
	rep.MutatedText = string(out)
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
	if err := ctx.Err(); err != nil {
		return err
	}
	if guardy.DecisionFromReport(*lastRep).Disposition != guardy.DispositionNone {
		return nil
	}
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
		if err := ctx.Err(); err != nil {
			return err
		}
		if rep != nil {
			*lastRep = guardy.ComposeReports(*lastRep, rep)
			if guardy.DecisionFromReport(*lastRep).Disposition != guardy.DispositionNone {
				return nil
			}
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

func isNilLeaf(leaf LeafValidator) bool {
	value := reflect.ValueOf(leaf)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
