package jsonschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	schemaengine "github.com/santhosh-tekuri/jsonschema/v6"
)

func checkNumbers(value any) error {
	switch v := value.(type) {
	case json.Number:
		if _, ok := new(big.Rat).SetString(string(v)); !ok {
			return errors.New("JSON number exceeds the schema engine's exact arithmetic range")
		}
	case map[string]any:
		for _, child := range v {
			if err := checkNumbers(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := checkNumbers(child); err != nil {
				return err
			}
		}
	}
	return nil
}

// numericProbe makes a private compilation copy with safe numeric operands. Its
// graph identifies actual assertions, including references into annotations and
// active vocabularies. Only the original documents can become a runtime schema.
func numericProbe(value any) any {
	switch v := value.(type) {
	case json.Number:
		_, ok := new(big.Rat).SetString(string(v))
		if !ok {
			return json.Number("1")
		}
	case map[string]any:
		cloned := make(map[string]any, len(v))
		for key, child := range v {
			cloned[key] = numericProbe(child)
		}
		return cloned
	case []any:
		cloned := make([]any, len(v))
		for i, child := range v {
			cloned[i] = numericProbe(child)
		}
		return cloned
	}
	return value
}

func checkCompiledNumbers(
	schema *schemaengine.Schema,
	documents map[string]any,
	seen map[*schemaengine.Schema]bool,
) error {
	if schema == nil || seen[schema] {
		return nil
	}
	seen[schema] = true
	if original := schemaDocument(schema.Location, documents); original != nil {
		if err := checkAssertions(schema, original); err != nil {
			return fmt.Errorf("%s: %w", schema.Location, err)
		}
	}
	// Traverse only exported schema graph fields; no private engine state or unsafe.
	value := reflect.ValueOf(schema).Elem()
	for _, field := range value.Fields() {
		if field.CanInterface() {
			if err := checkGraphField(field, documents, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkGraphField(value reflect.Value, documents map[string]any, seen map[*schemaengine.Schema]bool) error {
	if value.Kind() == reflect.Interface && !value.IsNil() {
		return checkGraphField(value.Elem(), documents, seen)
	}
	if value.CanInterface() {
		if schema, ok := reflect.TypeAssert[*schemaengine.Schema](value); ok {
			return checkCompiledNumbers(schema, documents, seen)
		}
		if reference, ok := reflect.TypeAssert[*schemaengine.DynamicRef](value); ok && reference != nil {
			return checkCompiledNumbers(reference.Ref, documents, seen)
		}
	}
	return checkGraphContainer(value, documents, seen)
}

func checkGraphContainer(value reflect.Value, documents map[string]any, seen map[*schemaengine.Schema]bool) error {
	switch value.Kind() { //nolint:exhaustive // Only graph containers contain schema edges.
	case reflect.Slice:
		for i := range value.Len() {
			if err := checkGraphField(value.Index(i), documents, seen); err != nil {
				return err
			}
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if err := checkGraphField(iterator.Value(), documents, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkAssertions(schema *schemaengine.Schema, original map[string]any) error {
	assertions := map[string]bool{
		"minimum":          schema.Minimum != nil || schema.ExclusiveMinimum != nil,
		"maximum":          schema.Maximum != nil || schema.ExclusiveMaximum != nil,
		"exclusiveMinimum": schema.ExclusiveMinimum != nil, "exclusiveMaximum": schema.ExclusiveMaximum != nil,
		"multipleOf": schema.MultipleOf != nil, "enum": schema.Enum != nil, "const": schema.Const != nil,
	}
	for keyword, active := range assertions {
		if active {
			if err := checkNumbers(original[keyword]); err != nil {
				return err
			}
		}
	}
	counts := map[string]*int{
		"minLength": schema.MinLength, "maxLength": schema.MaxLength,
		"minItems": schema.MinItems, "maxItems": schema.MaxItems,
		"minProperties": schema.MinProperties, "maxProperties": schema.MaxProperties,
		"minContains": schema.MinContains, "maxContains": schema.MaxContains,
	}
	for keyword, active := range counts {
		if active != nil {
			if err := checkCount(original[keyword]); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkCount(value any) error {
	number, ok := value.(json.Number)
	if !ok {
		return nil
	}
	rational, ok := new(big.Rat).SetString(string(number))
	if !ok || !rational.IsInt() || !rational.Num().IsInt64() {
		return errors.New("schema count exceeds exact integer range")
	}
	if strconv.IntSize == 32 && rational.Num().Int64() > 1<<31-1 {
		return errors.New("schema count exceeds platform integer range")
	}
	return nil
}

func schemaDocument(location string, documents map[string]any) map[string]any {
	uri, fragment, _ := strings.Cut(location, "#")
	value := documents[uri]
	pointer, err := url.PathUnescape(fragment)
	if err != nil {
		return nil
	}
	if pointer != "" {
		for token := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
			token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
			switch parent := value.(type) {
			case map[string]any:
				value = parent[token]
			case []any:
				index, err := strconv.Atoi(token)
				if err != nil || index < 0 || index >= len(parent) {
					return nil
				}
				value = parent[index]
			default:
				return nil
			}
		}
	}
	object, _ := value.(map[string]any)
	return object
}

// Check dynamic anchors that the engine registers even when they are not exposed
// as public graph edges. Failed lookups are annotations, not registered anchors.
func checkDynamicAnchors(
	compiler *schemaengine.Compiler,
	value any,
	base string,
	documents map[string]any,
	seen map[*schemaengine.Schema]bool,
) error {
	switch node := value.(type) {
	case map[string]any:
		return checkDynamicObject(compiler, node, base, documents, seen)
	case []any:
		for _, child := range node {
			if err := checkDynamicAnchors(compiler, child, base, documents, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkDynamicObject(
	compiler *schemaengine.Compiler,
	node map[string]any,
	base string,
	documents map[string]any,
	seen map[*schemaengine.Schema]bool,
) error {
	if id, ok := node["$id"].(string); ok {
		parent, err := url.Parse(base)
		if err != nil {
			return nil //nolint:nilerr // A malformed annotation identifier does not identify a resource.
		}
		reference, err := url.Parse(id)
		if err != nil {
			return nil //nolint:nilerr // A malformed annotation identifier does not identify a resource.
		}
		base = parent.ResolveReference(reference).String()
	}
	if anchor, ok := node["$dynamicAnchor"].(string); ok {
		if schema, err := compiler.Compile(base + "#" + anchor); err == nil {
			if err := checkCompiledNumbers(schema, documents, seen); err != nil {
				return err
			}
		}
	}
	for _, child := range node {
		if err := checkDynamicAnchors(compiler, child, base, documents, seen); err != nil {
			return err
		}
	}
	return nil
}
