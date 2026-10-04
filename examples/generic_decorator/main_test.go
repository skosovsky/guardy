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
	for _, marker := range []string{"input scope policy blocked:", "input wordlist blocked:", "output user channel blocked:", "ok: Hello! How can I help? true"} {
		if !strings.Contains(output.String(), marker) {
			t.Fatalf("missing %q in example output: %s", marker, output.String())
		}
	}
}
