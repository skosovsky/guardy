package main

import (
	"testing"

	"github.com/skosovsky/guardy"
)

func TestStreamingExampleDeliversCheckedBytes(t *testing.T) {
	for _, profile := range []guardy.ReleaseProfile{guardy.ReleaseWholeResponse, guardy.ReleaseValidatedUnits, guardy.ReleaseBestEffort} {
		// Arrange.
		selected := profile
		// Act.
		out, err := streamExample(selected)
		// Assert.
		if err != nil || out != "hello\nworld\n" {
			t.Fatalf("profile=%s output=%q err=%v", selected, out, err)
		}
	}
}
