// Package jsonredact recursively redacts string leaves in JSON documents.
package jsonredact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/skosovsky/guardy"
	"github.com/skosovsky/guardy/internal/jsondoc"
)

// LeafValidator validates or redacts individual string leaves during JSON traversal.
type LeafValidator = guardy.Validator[string]

// JSONRedactValidator walks sorted object keys and array indices, composing independent
// string-leaf checks. Correction/deny do not stop traversal; fault/error/cancel do.
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
		return jsonFault(input, lastRep, walkErr)
	}
	if guardy.DecisionFromReport(lastRep).Disposition != guardy.DispositionNone {
		return input, lastRep.CloneWithoutState(), nil
	}
	out, err := json.Marshal(root)
	if err != nil {
		return jsonFault(input, lastRep, fmt.Errorf("jsonredact: marshal: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return jsonFault(input, lastRep, err)
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
	if guardy.DecisionFromReport(*lastRep).IsSystemFault() {
		return nil
	}
	switch val := (*node).(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for key := range val {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, k := range keys {
			c := val[k]
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
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = errors.Join(err, ctxErr)
		}
		if err != nil {
			return err
		}
		if rep != nil {
			*lastRep = guardy.ComposeReports(*lastRep, rep)
			if guardy.DecisionFromReport(rep).Disposition == guardy.DispositionNone &&
				rep.Action == guardy.ActionRedact {
				*changed = true
				*node = out
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

func jsonFault(input string, completed *guardy.Report, cause error) (string, *guardy.Report, error) {
	err := guardy.WithCompletedObservations(cause, completed)
	return input, guardy.CompletedReportFromError(err), err
}
