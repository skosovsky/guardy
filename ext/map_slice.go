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

// MapSlice adapts Validator[string] to Validator[[]T] for BYOT multi-turn workflows.
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
	mutated := false
	var lastRedact *guardy.Report

	for i := range input {
		current := input[i]
		subInput := m.extract(current)
		newSub, rep, err := m.validator.Validate(ctx, subInput)
		if err != nil {
			return input, rep, err
		}
		if rep == nil {
			continue
		}
		switch rep.Action {
		case guardy.ActionPass:
			// No per-item mutation; continue to next element.
		case guardy.ActionRedact:
			out[i] = m.inject(current, newSub)
			mutated = true
			lastRedact = rep.CloneWithoutState()
			prefixIndex(lastRedact, i)
		case guardy.ActionBlock, guardy.ActionRetry:
			blocking := rep.Clone()
			prefixIndex(blocking, i)
			return input, blocking, nil
		}
	}

	if mutated {
		if lastRedact == nil {
			lastRedact = guardy.FinishReport(&guardy.Report{
				Action: guardy.ActionRedact, Validator: mapSliceValidatorName,
			}, guardy.ControlSpec{Action: guardy.ActionRedact})
		}
		return out, lastRedact, nil
	}
	return input, guardy.FinishReport(&guardy.Report{
		Action: guardy.ActionPass, Validator: mapSliceValidatorName,
	}, guardy.ControlSpec{Action: guardy.ActionPass}), nil
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
