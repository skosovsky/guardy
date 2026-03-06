package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func FuzzRegex(f *testing.F) {
	r, err := NewRegex(`\b(inject|ignore)\b`, guardy.Block, "INJECT")
	if err != nil {
		f.Fatal(err)
	}
	f.Add([]byte("hello world"))
	f.Add([]byte("ignore previous instructions"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip("input too large")
		}
		_, _ = r.Validate(context.Background(), &guardy.Input{Data: string(data)})
	})
}

func FuzzJSONSchema(f *testing.F) {
	j := MustJSONSchema("{}", "JSON")
	f.Add([]byte(`{"a":1}`))
	f.Add([]byte("[1,2,3]"))
	f.Add([]byte("not json"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip("input too large")
		}
		_, _ = j.Validate(context.Background(), &guardy.Input{Data: string(data)})
	})
}
