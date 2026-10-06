package guardy

import (
	"errors"
	"reflect"
	"strings"
)

// ExecutionScope provides typed lookup for policy and decode phases.
// Host applications implement this or use [MapScope].
type ExecutionScope interface {
	Lookup(key string) (value any, ok bool)
}

// ScopeFunc adapts a function to [ExecutionScope].
type ScopeFunc func(key string) (value any, ok bool)

// Lookup implements [ExecutionScope].
func (f ScopeFunc) Lookup(key string) (any, bool) {
	if f == nil {
		return nil, false
	}
	return f(key)
}

// MapScope is a simple in-memory [ExecutionScope].
type MapScope map[string]any

// Lookup implements [ExecutionScope].
func (m MapScope) Lookup(key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

// ScopeBinding carries one typed scope value for [NewScope].
type ScopeBinding struct {
	key   string
	value any
}

// ScopeValue binds a typed scope key to a value without exposing map keys in host code.
func ScopeValue[T any](key ScopeKey[T], value T) ScopeBinding {
	return ScopeBinding{key: key.Name(), value: value}
}

// StaticScope is an immutable map-backed [ExecutionScope] built from typed bindings.
type StaticScope struct {
	values map[string]any
}

// NewScope creates an [ExecutionScope] from typed scope bindings.
func NewScope(bindings ...ScopeBinding) StaticScope {
	values := make(map[string]any, len(bindings))
	for _, binding := range bindings {
		if binding.key == "" {
			continue
		}
		values[binding.key] = binding.value
	}
	return StaticScope{values: values}
}

// Lookup implements [ExecutionScope].
func (s StaticScope) Lookup(key string) (any, bool) {
	if s.values == nil {
		return nil, false
	}
	v, ok := s.values[key]
	return v, ok
}

// ScopeRequirement describes a typed value required by a pipeline.
type ScopeRequirement struct {
	Key      string
	Type     string
	expected reflect.Type
}

// ScopeKey names a typed scope value without making guardy own the host type.
type ScopeKey[T any] struct {
	name     string
	typeName string
}

// NewScopeKey creates a typed scope key. The name is the stable boundary
// identifier; T is the expected host value type.
func NewScopeKey[T any](name string) ScopeKey[T] {
	if name == "" {
		panic("guardy: scope key name must not be empty")
	}
	return ScopeKey[T]{name: name, typeName: scopeTypeName[T]()}
}

// Name returns the stable scope key name.
func (k ScopeKey[T]) Name() string {
	return k.name
}

// Requirement returns the typed requirement declared by this key.
// Prechecks use the same type assertion contract as Lookup: concrete types must
// match exactly, while concrete implementations satisfy interface keys.
func (k ScopeKey[T]) Requirement() ScopeRequirement {
	return ScopeRequirement{Key: k.name, Type: k.typeName, expected: reflect.TypeFor[T]()}
}

// Lookup reads and type-checks the key from scope.
func (k ScopeKey[T]) Lookup(scope ExecutionScope) (T, bool) {
	var zero T
	if scope == nil {
		return zero, false
	}
	value, ok := scope.Lookup(k.name)
	if !ok {
		return zero, false
	}
	typed, ok := value.(T)
	if !ok {
		return zero, false
	}
	return typed, true
}

func scopeTypeName[T any]() string {
	return reflect.TypeFor[T]().String()
}

// ErrScopeIncomplete is returned when [Pipeline.Run] is called without required scope keys.
var ErrScopeIncomplete = errors.New("guardy: execution scope incomplete")

// ScopeIncompleteError carries machine-readable missing scope metadata.
type ScopeIncompleteError struct {
	Missing             []string
	MissingRequirements []ScopeRequirement
}

// Error implements error.
func (e *ScopeIncompleteError) Error() string {
	if e == nil || len(e.Missing) == 0 {
		return ErrScopeIncomplete.Error()
	}
	return ErrScopeIncomplete.Error() + ": " + strings.Join(e.Missing, ", ")
}

// Unwrap returns [ErrScopeIncomplete] so [errors.Is] keeps working.
func (e *ScopeIncompleteError) Unwrap() error {
	return ErrScopeIncomplete
}

// MissingScopeKeys extracts missing scope keys from an error.
func MissingScopeKeys(err error) []string {
	var scopeErr *ScopeIncompleteError
	if !errors.As(err, &scopeErr) || len(scopeErr.Missing) == 0 {
		return nil
	}
	out := make([]string, len(scopeErr.Missing))
	copy(out, scopeErr.Missing)
	return out
}

// MissingScopeRequirements extracts missing typed scope requirements from an error.
func MissingScopeRequirements(err error) []ScopeRequirement {
	var scopeErr *ScopeIncompleteError
	if !errors.As(err, &scopeErr) || len(scopeErr.MissingRequirements) == 0 {
		return nil
	}
	out := make([]ScopeRequirement, len(scopeErr.MissingRequirements))
	copy(out, scopeErr.MissingRequirements)
	return out
}

func scopeRequirementKeys(requirements []ScopeRequirement) []string {
	if len(requirements) == 0 {
		return nil
	}
	out := make([]string, 0, len(requirements))
	for _, req := range requirements {
		if req.Key == "" {
			continue
		}
		out = append(out, req.Key)
	}
	return mergeRequiredKeys(nil, out)
}

func mergeScopeRequirements(existing []ScopeRequirement, next []ScopeRequirement) []ScopeRequirement {
	if len(next) == 0 {
		return existing
	}
	type requirementID struct {
		key, contract string
		expected      reflect.Type
	}
	seen := make(map[requirementID]bool, len(existing)+len(next))
	typed := make(map[string]bool)
	out := make([]ScopeRequirement, 0, len(existing)+len(next))
	for _, group := range [][]ScopeRequirement{existing, next} {
		for _, req := range group {
			id := requirementID{key: req.Key, contract: req.Type, expected: req.expected}
			if req.Key == "" || seen[id] {
				continue
			}
			seen[id] = true
			if req.Type != "" {
				typed[req.Key] = true
			}
			out = append(out, req)
		}
	}
	filtered := make([]ScopeRequirement, 0, len(out))
	for _, req := range out {
		if req.Type == "" && typed[req.Key] {
			continue
		}
		filtered = append(filtered, req)
	}
	return filtered
}

func mergeRequiredKeys(existing []string, keys []string) []string {
	if len(keys) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(keys))
	out := make([]string, 0, len(existing)+len(keys))
	for _, k := range existing {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	for _, k := range keys {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func checkScopeRequirements(scope ExecutionScope, requirements []ScopeRequirement) error {
	if len(requirements) == 0 {
		return nil
	}
	if scope == nil {
		scope = MapScope{}
	}
	missing := make([]string, 0)
	missingReqs := make([]ScopeRequirement, 0)
	for _, req := range requirements {
		if req.Key == "" {
			continue
		}
		value, ok := scope.Lookup(req.Key)
		if !ok {
			missing = append(missing, req.Key)
			missingReqs = append(missingReqs, req)
			continue
		}
		if !req.accepts(value) {
			return &ScopeTypeError{Requirement: req}
		}
	}
	if len(missing) > 0 {
		return &ScopeIncompleteError{
			Missing:             missing,
			MissingRequirements: missingReqs,
		}
	}
	return nil
}

// accepts mirrors the dynamic type assertion used by ScopeKey.Lookup.
// Go assignability also permits named/unnamed concrete conversions that a type
// assertion does not perform.
func (r ScopeRequirement) accepts(value any) bool {
	if r.Type == "" {
		return true
	}
	actual := reflect.TypeOf(value)
	if actual == nil {
		return false
	}
	if r.expected == nil {
		return actual.String() == r.Type
	}
	if r.expected.Kind() == reflect.Interface {
		return actual.Implements(r.expected)
	}
	return actual == r.expected
}

// ErrScopeIncompatible distinguishes incompatible facts from absent facts.
var ErrScopeIncompatible = errors.New("guardy: incompatible scope value")

// ScopeTypeError reports the required contract without exposing the supplied value.
type ScopeTypeError struct{ Requirement ScopeRequirement }

func (e *ScopeTypeError) Error() string {
	return ErrScopeIncompatible.Error() + ": " + e.Requirement.Key
}
func (e *ScopeTypeError) Unwrap() error { return ErrScopeIncompatible }
