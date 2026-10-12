// Package jsondoc defines the unambiguous JSON decoding contract used by guards.
package jsondoc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
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
	if err := validateBindingDocument(input); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("bind JSON: %w", err)
	}
	return nil
}

// validateBindingDocument checks keys and grammar without materializing values.
// Only preliminarily valid documents reach encoding/json's final syntax check;
// invalid documents retain Decode's diagnostic errors without constructing two
// independent sets of standard-library syntax errors.
func validateBindingDocument(input string) error {
	scanner := bindingScanner{input: input, pos: 0, duplicate: false}
	if !scanner.value(0) {
		if scanner.duplicate {
			return errors.New("duplicate JSON object key")
		}
		_, err := Decode(input)
		return err
	}
	scanner.space()
	if scanner.pos != len(input) {
		return errors.New("JSON document must contain exactly one value")
	}
	if !json.Valid([]byte(input)) {
		_, err := Decode(input)
		return err
	}
	return nil
}

// bindingScanner is a bounded preliminary grammar and duplicate-key scanner.
// The standard library still validates every admitted document before binding.
// All key sets belong to this invocation; there is no cache or retained buffer.
type bindingScanner struct {
	input     string
	pos       int
	duplicate bool
}

func (s *bindingScanner) space() {
	for s.pos < len(s.input) {
		switch s.input[s.pos] {
		case ' ', '\t', '\r', '\n':
			s.pos++
		default:
			return
		}
	}
}

func (s *bindingScanner) take(want byte) bool {
	s.space()
	if s.pos >= len(s.input) || s.input[s.pos] != want {
		return false
	}
	s.pos++
	return true
}

func (s *bindingScanner) stringToken() (int, int, bool, bool) {
	s.space()
	start := s.pos
	escaped := false
	if !s.take('"') {
		return start, s.pos, escaped, false
	}
	for s.pos < len(s.input) {
		c := s.input[s.pos]
		s.pos++
		switch {
		case c == '"':
			return start, s.pos, escaped, true
		case c < ' ':
			return start, s.pos, escaped, false
		case c == '\\':
			escaped = true
			if !s.escape() {
				return start, s.pos, escaped, false
			}
		}
	}
	return start, s.pos, escaped, false
}

func (s *bindingScanner) escape() bool {
	if s.pos == len(s.input) {
		return false
	}
	c := s.input[s.pos]
	s.pos++
	switch c {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return true
	case 'u':
		const hexDigits = 4
		for range hexDigits {
			if s.pos == len(s.input) || !strings.ContainsRune("0123456789abcdefABCDEF", rune(s.input[s.pos])) {
				return false
			}
			s.pos++
		}
		return true
	default:
		return false
	}
}

func (s *bindingScanner) value(depth int) bool {
	const maxDepth = 10000 // encoding/json's maximum container nesting depth.
	s.space()
	if s.pos == len(s.input) {
		return false
	}
	switch s.input[s.pos] {
	case '{':
		return depth < maxDepth && s.object(depth+1)
	case '[':
		return depth < maxDepth && s.array(depth+1)
	case '"':
		_, _, _, ok := s.stringToken()
		return ok
	case 't':
		return s.literal("true")
	case 'f':
		return s.literal("false")
	case 'n':
		return s.literal("null")
	default:
		return s.number()
	}
}

func (s *bindingScanner) key(start, end int, escaped bool) string {
	plain := s.input[start+1 : end-1]
	if !escaped && utf8.ValidString(plain) {
		return plain
	}
	// Match encoding/json's escapes, surrogate and invalid UTF-8 replacement.
	var decoded string
	// The preliminary string scanner has already validated the quoted token.
	_ = json.Unmarshal([]byte(s.input[start:end]), &decoded)
	return decoded
}

func (s *bindingScanner) object(depth int) bool {
	s.pos++
	if s.take('}') {
		return true
	}
	keys := make(map[string]struct{})
	for {
		start, end, escaped, valid := s.stringToken()
		if !valid {
			return false
		}
		key := s.key(start, end, escaped)
		if _, exists := keys[key]; exists {
			s.duplicate = true
			return false
		}
		keys[key] = struct{}{}
		if !s.take(':') || !s.value(depth) {
			return false
		}
		if s.take('}') {
			return true
		}
		if !s.take(',') {
			return false
		}
	}
}

func (s *bindingScanner) array(depth int) bool {
	s.pos++
	if s.take(']') {
		return true
	}
	for {
		if !s.value(depth) {
			return false
		}
		if s.take(']') {
			return true
		}
		if !s.take(',') {
			return false
		}
	}
}

func (s *bindingScanner) literal(literal string) bool {
	if !strings.HasPrefix(s.input[s.pos:], literal) {
		return false
	}
	s.pos += len(literal)
	return true
}

func (s *bindingScanner) digits() bool {
	start := s.pos
	for s.pos < len(s.input) && s.input[s.pos] >= '0' && s.input[s.pos] <= '9' {
		s.pos++
	}
	return s.pos > start
}

func (s *bindingScanner) number() bool {
	if s.input[s.pos] == '-' {
		s.pos++
	}
	if s.pos == len(s.input) {
		return false
	}
	if s.input[s.pos] == '0' {
		s.pos++
	} else if !s.digits() {
		return false
	}
	if s.pos < len(s.input) && s.input[s.pos] == '.' {
		s.pos++
		if !s.digits() {
			return false
		}
	}
	if s.pos < len(s.input) && (s.input[s.pos] == 'e' || s.input[s.pos] == 'E') {
		s.pos++
		if s.pos < len(s.input) && (s.input[s.pos] == '+' || s.input[s.pos] == '-') {
			s.pos++
		}
		if !s.digits() {
			return false
		}
	}
	return true
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
