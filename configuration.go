package guardy

import (
	"errors"
	"fmt"
	"reflect"
)

// ErrConfiguration identifies invalid built-in compilation settings.
var ErrConfiguration = errors.New("guardy: invalid configuration")

// ConfigurationError contains a component, field path and diagnostic code.
// These identifiers must be library-owned; never put caller values in them.
// Detailed third-party errors may be inspected through Cause, not default Error text.
type ConfigurationError struct {
	Component string
	Field     string
	Code      string
	Cause     error
}

// Error returns safe configuration identifiers without caller data.
func (e *ConfigurationError) Error() string {
	return fmt.Sprintf("%s: %s.%s (%s)", ErrConfiguration, e.Component, e.Field, e.Code)
}

// Unwrap preserves the configuration category and optional diagnostic cause.
func (e *ConfigurationError) Unwrap() error {
	return errors.Join(ErrConfiguration, e.Cause)
}

func configurationError(component, field, code string) error {
	return &ConfigurationError{Component: component, Field: field, Code: code, Cause: nil}
}

// nilImplementation identifies absent implementations hidden in an interface.
func nilImplementation(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	case reflect.Invalid,
		reflect.Bool,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr,
		reflect.Float32,
		reflect.Float64,
		reflect.Complex64,
		reflect.Complex128,
		reflect.Array,
		reflect.String,
		reflect.Struct,
		reflect.UnsafePointer:
		return false
	default:
		return false
	}
}
