package ext

import (
	"context"
	"errors"
	"fmt"

	"github.com/skosovsky/guardy"
)

const mapSliceValidatorName = "map_slice"

type mapSliceValidator[T any] struct {
	extract   func(T) string
	inject    func(T, string) T
	validator guardy.Validator[string]
}

// MapSlice applies an independent Validator[string] to each element of a BYOT slice.
// It aggregates decisions; it does not analyze relationships between messages.
func MapSlice[T any](
	extract func(T) string,
	inject func(T, string) T,
	validator guardy.Validator[string],
) guardy.Validator[[]T] {
	return &mapSliceValidator[T]{
		extract:   extract,
		inject:    inject,
		validator: validator,
	}
}

func (m *mapSliceValidator[T]) Validate(ctx context.Context, input []T) ([]T, *guardy.Report, error) {
	if m.extract == nil || m.inject == nil || m.validator == nil {
		return input, nil, errors.New("ext: MapSlice requires non-nil extract, inject, and validator")
	}
	if len(input) == 0 {
		return input, guardy.FinishReport(&guardy.Report{
			Action: guardy.ActionPass, Validator: mapSliceValidatorName,
		}, guardy.ControlSpec{Action: guardy.ActionPass}), nil
	}

	out := append([]T(nil), input...)
	combined := guardy.ComposeReports()

	for i := range input {
		if err := ctx.Err(); err != nil {
			return input, combined, err
		}
		current := input[i]
		newSub, rep, err := m.validator.Validate(ctx, m.extract(current))
		if err != nil {
			return input, rep, err
		}
		if err := ctx.Err(); err != nil {
			return input, combined, err
		}
		indexed := rep.CloneWithoutState()
		prefixIndex(indexed, i)
		combined = guardy.ComposeReports(combined, indexed)
		decision := guardy.DecisionFromReport(combined)
		if decision.Disposition != guardy.DispositionNone {
			return input, combined, nil
		}
		if rep != nil && rep.Action == guardy.ActionRedact {
			out[i] = m.inject(current, newSub)
		}
	}
	if err := ctx.Err(); err != nil {
		return input, combined, err
	}
	return out, combined, nil
}

func prefixIndex(rep *guardy.Report, idx int) {
	if rep == nil {
		return
	}
	if rep.Reason != "" {
		rep.Reason = fmt.Sprintf("item[%d]: %s", idx, rep.Reason)
	}
	if rep.Feedback != "" {
		rep.Feedback = fmt.Sprintf("item[%d]: %s", idx, rep.Feedback)
	}
}
