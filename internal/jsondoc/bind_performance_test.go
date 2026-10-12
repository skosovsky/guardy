package jsondoc

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func BenchmarkBindNumbers(b *testing.B) {
	for _, count := range []int{16, 256, 4096} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			// Arrange: only the document fixture is outside the invocation timer.
			input := "[" + strings.TrimSuffix(strings.Repeat("1,", count), ",") + "]"
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var values []int
				// Act.
				err := Bind(input, &values)
				// Assert: every invocation binds into a fresh caller-owned target.
				if err != nil || len(values) != count || values[0] != 1 || values[count-1] != 1 {
					b.Fatalf("incorrect numeric binding: length=%d, error=%v", len(values), err)
				}
			}
		})
	}
}

func BenchmarkBindDocuments(b *testing.B) {
	for name, input := range map[string]string{
		"object": `{"id":9007199254740993,"name":"example","active":true,"empty":null}`,
		"mixed":  `[9007199254740993,"text",true,null,{"a":1},[0.100000000000000001]]`,
		"nested": strings.Repeat(`{"child":`, 32) + `[1,2,3]` + strings.Repeat("}", 32),
	} {
		b.Run(name, func(b *testing.B) {
			// Arrange.
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var value any
				// Act.
				err := Bind(input, &value)
				// Assert.
				if err != nil || !benchmarkDocumentMatches(name, value) {
					b.Fatalf("incorrect document binding: %v", err)
				}
			}
		})
	}
}

func BenchmarkBindAdmission(b *testing.B) {
	for name, input := range map[string]string{
		"malformed": `{"a":[1,2,]}`,
		"duplicate": `{"a":1,"a":2}`,
		"escaped":   `{"a":1,"\u0061":2}`,
		"trailing":  `{"a":1} []`,
	} {
		b.Run(name, func(b *testing.B) {
			// Arrange.
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var target json.RawMessage
				// Act.
				err := Bind(input, &target)
				// Assert.
				if err == nil || target != nil {
					b.Fatalf("invalid input reached custom binding: %v", err)
				}
			}
		})
	}
}

func BenchmarkBindCustom(b *testing.B) {
	// Arrange: RawMessage implements the standard UnmarshalJSON callback.
	input := `{"id":9007199254740993,"items":[1,2,3],"metadata":{"source":"custom"}}`
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var target json.RawMessage
		// Act.
		err := Bind(input, &target)
		// Assert.
		if err != nil || string(target) != input {
			b.Fatalf("incorrect custom binding: %v", err)
		}
	}
}

func BenchmarkBindConstruction(b *testing.B) {
	// Arrange: measure cold document/target construction separately from warm Bind.
	b.ReportAllocs()
	for b.Loop() {
		// Act.
		input := fmt.Sprintf(`{"id":%d,"values":[1,2,3]}`, 17)
		var value struct {
			ID     int   `json:"id"`
			Values []int `json:"values"`
		}
		err := Bind(input, &value)
		// Assert.
		if err != nil || value.ID != 17 || len(value.Values) != 3 {
			b.Fatalf("incorrect cold binding: %v", err)
		}
	}
}

// benchmarkDocumentMatches verifies each bound payload without allocating an
// expected tree, which would contaminate the allocation measurements.
func benchmarkDocumentMatches(name string, value any) bool {
	switch name {
	case "object":
		object, ok := value.(map[string]any)
		return ok && len(object) == 4 && object["id"] == json.Number("9007199254740993") &&
			object["name"] == "example" && object["active"] == true && object["empty"] == nil
	case "mixed":
		items, ok := value.([]any)
		if !ok || len(items) != 6 || items[0] != json.Number("9007199254740993") ||
			items[1] != "text" || items[2] != true || items[3] != nil {
			return false
		}
		object, objectOK := items[4].(map[string]any)
		numbers, numbersOK := items[5].([]any)
		return objectOK && len(object) == 1 && object["a"] == json.Number("1") &&
			numbersOK && len(numbers) == 1 && numbers[0] == json.Number("0.100000000000000001")
	case "nested":
		for range 32 {
			object, ok := value.(map[string]any)
			if !ok || len(object) != 1 {
				return false
			}
			value = object["child"]
		}
		items, ok := value.([]any)
		return ok && len(items) == 3 && items[0] == json.Number("1") &&
			items[1] == json.Number("2") && items[2] == json.Number("3")
	default:
		return false
	}
}
