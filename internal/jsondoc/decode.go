// Package jsondoc defines the unambiguous JSON decoding contract used by guards.
package jsondoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Decode reads exactly one JSON value, rejects duplicate object keys and keeps
// numbers as [json.Number]. Null and scalar top-level values are valid documents.
func Decode(input string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	value, err := readValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON document must contain exactly one value")
	}
	return value, nil
}

// Bind applies Decode's syntax contract before binding into caller-owned types.
// Interface values retain [json.Number]; explicit numeric types follow encoding/json.
func Bind(input string, target any) error {
	if _, err := Decode(input); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("bind JSON: %w", err)
	}
	return nil
}

func readValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read JSON value: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delimiter {
	case '{':
		return readObject(decoder)
	case '[':
		return readArray(decoder)
	default:
		return nil, errors.New("unexpected JSON delimiter")
	}
}

func readObject(decoder *json.Decoder) (any, error) {
	object := make(map[string]any)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read JSON key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("JSON object key must be a string")
		}
		if _, exists := object[key]; exists {
			return nil, errors.New("duplicate JSON object key")
		}
		value, err := readValue(decoder)
		if err != nil {
			return nil, err
		}
		object[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("close JSON object: %w", err)
	}
	return object, nil
}

func readArray(decoder *json.Decoder) (any, error) {
	array := make([]any, 0)
	for decoder.More() {
		value, err := readValue(decoder)
		if err != nil {
			return nil, err
		}
		array = append(array, value)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("close JSON array: %w", err)
	}
	return array, nil
}
