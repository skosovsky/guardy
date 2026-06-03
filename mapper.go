package guardy

import (
	"bytes"
	"context"
	"encoding/json"
)

const mapJSONRawMessageValidatorName = "map_json_raw_message"

// mappedValidator adapts Validator[U] to Validator[T] via extract/inject (Lens pattern).
type mappedValidator[T any, U any] struct {
	inner   Validator[U]
	extract func(T) U
	inject  func(T, U) T
}

// Validate delegates to the inner validator; on ActionRedact applies inject to mutate T.
// MutatedText is cleared at T level (it refers to U, not T) to avoid misleading telemetry.
func (m *mappedValidator[T, U]) Validate(ctx context.Context, input T) (T, *Report, error) {
	subInput := m.extract(input)
	newSub, rep, err := m.inner.Validate(ctx, subInput)
	if err != nil {
		return input, rep, err
	}
	if rep != nil && rep.Action == ActionRedact {
		input = m.inject(input, newSub)
		rep = rep.CloneWithoutState()
	}
	return input, rep, nil
}

// Map returns a Validator[T] that wraps Validator[U] with extract (getter) and inject (setter).
// When the inner validator returns ActionRedact, inject is called to apply the mutation to T.
func Map[T any, U any](v Validator[U], extract func(T) U, inject func(T, U) T) Validator[T] {
	return &mappedValidator[T, U]{
		inner:   v,
		extract: extract,
		inject:  inject,
	}
}

type jsonRawMessageValidator[T any] struct {
	inner   Validator[string]
	extract func(*T) json.RawMessage
	inject  func(*T, json.RawMessage) *T
}

func rawMessageIsEmpty(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	return bytes.Equal(raw, []byte("null"))
}

// MapJSONRawMessage adapts Validator[string] to Validator[T] for a [json.RawMessage] field on T.
// Use when T is a struct value (Validator[MyDTO]), not Validator[*MyDTO].
// extract and inject must be non-nil (panics if nil). inject must not return nil after ActionRedact.
// Empty, nil, and exact JSON null literals (not " null " with spaces) skip the inner validator.
// After ActionRedact, mutated text must pass [json.Valid] before inject; otherwise ActionRetry
// with [CodeJSONRedactCorrupted] is returned (distinct from [CodeJSONInvalid] for parse/bind errors).
func MapJSONRawMessage[T any](
	v Validator[string],
	extract func(*T) json.RawMessage,
	inject func(*T, json.RawMessage) *T,
) Validator[T] {
	if extract == nil {
		panic("guardy: MapJSONRawMessage: extract must not be nil")
	}
	if inject == nil {
		panic("guardy: MapJSONRawMessage: inject must not be nil")
	}
	return &jsonRawMessageValidator[T]{
		inner:   v,
		extract: extract,
		inject:  inject,
	}
}

func (m *jsonRawMessageValidator[T]) Validate(ctx context.Context, input T) (T, *Report, error) {
	if err := ctx.Err(); err != nil {
		return input, nil, err
	}
	raw := m.extract(&input)
	if rawMessageIsEmpty(raw) {
		return input, FinishReport(&Report{
			Action:    ActionPass,
			Validator: mapJSONRawMessageValidatorName,
		}, ControlSpec{Action: ActionPass}), nil
	}

	newStr, rep, err := m.inner.Validate(ctx, string(raw))
	if err != nil {
		return input, rep, err
	}
	if rep == nil {
		return input, rep, nil
	}
	switch rep.Action {
	case ActionBlock, ActionRetry:
		return input, rep, nil
	case ActionRedact:
		if !json.Valid([]byte(newStr)) {
			const msg = "redaction corrupted JSON structure"
			return input, FinishReport(&Report{
				Action:    ActionRetry,
				Validator: mapJSONRawMessageValidatorName,
				Code:      CodeJSONRedactCorrupted,
				Reason:    msg,
				Feedback:  msg,
			}, ControlSpec{Action: ActionRetry}), nil
		}
		out := m.inject(&input, json.RawMessage(newStr))
		if out == nil {
			panic("guardy: MapJSONRawMessage: inject returned nil")
		}
		input = *out
		rep = rep.CloneWithoutState()
		return input, rep, nil
	default:
		return input, rep, nil
	}
}
