package jsondoc

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDecodeContract(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{}`, `true`, `"hello"`, `9007199254740993`, `[1,0.100000000000000001,1e100]`} {
		// Arrange / Act.
		value, err := Decode(raw)
		// Assert.
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := Decode(string(encoded))
		if err != nil || !reflect.DeepEqual(value, restored) {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{``, `{`, `[1,]`, `{"a":1,"a":2}`, `{"a":1,"\u0061":2}`, `{} []`, `[1]x`} {
		// Arrange / Act / Assert.
		if _, err := Decode(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
