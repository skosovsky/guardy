package jsondoc

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type bindingCallback struct {
	calls int
	raw   string
	err   error
}

func (c *bindingCallback) UnmarshalJSON(raw []byte) error {
	c.calls++
	c.raw = string(raw)
	return c.err
}

func TestBindRejectsBeforeCustomCallback(t *testing.T) {
	for _, raw := range []string{
		``, `{`, `[1,]`, `{"a":1,"a":2}`, `{"a":1, "a":2}`, `{"a":1,"\u0061":2}`, `{ "a": 1,  "\u0061": 2 }`,
		`{"outer":[{"a":1,"a":2}]}`, `{ "a": 1, "a": `,
		`{} []`, `[1]x`, `{"a":1} garbage`, `{"\ud800":1,"\ufffd":2}`,
		`{"\ud800":1,"\udfff":2}`, "{\"\xff\":1,\"\ufffd\":2}",
		strings.Repeat("[", 10001) + "0" + strings.Repeat("]", 10001),
	} {
		t.Run(rawName(raw), func(t *testing.T) {
			// Arrange.
			target := bindingCallback{}
			_, oldErr := Decode(raw)
			// Act.
			err := Bind(raw, &target)
			// Assert.
			if oldErr == nil || err == nil || err.Error() != oldErr.Error() {
				t.Fatalf("syntax error changed: old=%v bind=%v", oldErr, err)
			}
			if target.calls != 0 {
				t.Fatalf("invalid document invoked custom binding %d times", target.calls)
			}
		})
	}
}

func rawName(raw string) string {
	if len(raw) > 100 {
		return "deep"
	}
	return raw
}

func TestBindEncodingJSONCompatibility(t *testing.T) {
	for _, raw := range []string{
		`null`, `true`, `"hello"`, `9007199254740993`, `0.100000000000000001`,
		`1e100`, `[]`, `{}`, `{"a":1,"nested":[{"a":2}]}`, `{ "a": 1, "nested": [ { "a": 2 } ] }`,
		`{"a\"b":1,"a\\b":2,"\ud83d\ude00":3,"\ud800":4}`,
		`{"values":[null,true,"x",[1,2],{"key":9007199254740993}]}`,
		"{\"\xff\":1}", strings.Repeat("[", 10000) + "0" + strings.Repeat("]", 10000),
	} {
		t.Run(rawName(raw), func(t *testing.T) {
			// Arrange.
			var expected, actual any
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.UseNumber()
			if err := decoder.Decode(&expected); err != nil {
				t.Fatal(err)
			}
			// Act.
			err := Bind(raw, &actual)
			// Assert.
			if err != nil || !reflect.DeepEqual(expected, actual) {
				t.Fatalf("binding differs from encoding/json: %v", err)
			}
		})
	}
}

func TestBindTypedAndCustomSemantics(t *testing.T) {
	// Arrange.
	type arguments struct {
		Count int             `json:"count"`
		Value any             `json:"value"`
		Raw   json.RawMessage `json:"raw"`
	}
	var target arguments
	raw := `{"count":16,"value":9007199254740993,"raw":{"nested":[true,null]}}`
	// Act.
	err := Bind(raw, &target)
	// Assert.
	if err != nil || target.Count != 16 || target.Value != json.Number("9007199254740993") ||
		string(target.Raw) != `{"nested":[true,null]}` {
		t.Fatalf("typed binding: %+v, %v", target, err)
	}

	// Arrange.
	sentinel := errors.New("custom binding failure")
	custom := bindingCallback{err: sentinel}
	// Act.
	err = Bind(raw, &custom)
	// Assert.
	if !errors.Is(err, sentinel) || custom.calls != 1 || custom.raw != raw {
		t.Fatalf("custom semantics changed: %+v, %v", custom, err)
	}

	// Arrange.
	var number int
	// Act.
	err = Bind(`0.5`, &number)
	// Assert.
	if _, ok := errors.AsType[*json.UnmarshalTypeError](err); !ok {
		t.Fatalf("typed error lost: %v", err)
	}
}

func FuzzBindSyntaxContract(f *testing.F) {
	for _, raw := range []string{`null`, `{"a":1,"\u0061":2}`, `{"a":1, "a":2}`, `[1,{},"x"]`, "{\"\xff\":1,\"\ufffd\":2}", `{"\ud800":1,"\udfff":2}`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		// Arrange.
		_, expectedErr := Decode(raw)
		target := bindingCallback{}
		// Act.
		err := Bind(raw, &target)
		// Assert.
		if (err == nil) != (expectedErr == nil) {
			t.Fatalf("syntax contract differs: Decode=%v Bind=%v", expectedErr, err)
		}
		if expectedErr != nil {
			if target.calls != 0 {
				t.Fatal("custom callback invoked before syntax admission")
			}
			if err.Error() != expectedErr.Error() {
				t.Fatalf("syntax diagnostic differs: Decode=%v Bind=%v", expectedErr, err)
			}
		}
	})
}
