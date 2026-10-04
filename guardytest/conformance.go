package guardytest

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/guardy"
)

// BoundaryObservation records actual adapter behavior, including consumer bytes.
type BoundaryObservation struct {
	Decision     guardy.Decision
	Err          error
	HandlerCalls int
	Delivered    string
	CanonicalRaw string
}

// BoundaryCase is a concrete benign or adversarial integration scenario.
// Keep deterministic and semantic fixtures in separately named suites.
type BoundaryCase struct {
	Name         string
	Invoke       func(context.Context) BoundaryObservation
	Disposition  guardy.FailureDisposition
	HandlerCalls int
	Delivered    string
	CanonicalRaw string
}

// CheckBoundaryCases tests routing, dispatch, serialized output and canonical
// data together. Integration tests must capture the real sink, not UI-only output.
func CheckBoundaryCases(t *testing.T, cases []BoundaryCase) {
	t.Helper()
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			// Arrange is supplied by the integration; Act invokes actual wiring.
			observed := fixture.Invoke(context.Background())
			// Assert.
			if observed.Decision.Disposition != fixture.Disposition || observed.HandlerCalls != fixture.HandlerCalls ||
				observed.Delivered != fixture.Delivered ||
				observed.CanonicalRaw != fixture.CanonicalRaw {
				t.Fatalf("boundary observation: %+v; expected %+v", observed, fixture)
			}
			if fixture.Disposition != guardy.DispositionNone {
				var failure *guardy.PolicyFailure
				if !errors.As(observed.Err, &failure) || failure.Decision.Disposition != fixture.Disposition {
					t.Fatalf("lost canonical failure: %v", observed.Err)
				}
			} else if observed.Err != nil {
				t.Fatal(observed.Err)
			}
		})
	}
}
