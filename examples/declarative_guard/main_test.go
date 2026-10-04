package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMainReachesIndependentBoundaryScenarios(t *testing.T) {
	// Arrange.
	var output bytes.Buffer
	// Act: execute the actual example wiring.
	runExample(&output)
	// Assert: each independent scenario reached its intended result.
	for _, marker := range []string{"policy scope mismatch", "user channel + classifier", "wordlist + PII + length"} {
		if !strings.Contains(output.String(), marker) {
			t.Fatalf("missing %q in example output: %s", marker, output.String())
		}
	}
}
