package ext

import (
	"context"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestMapSlicePointerCopyOnWriteRetainsOriginalAfterDeny(t *testing.T) {
	type message struct{ text string }
	// Arrange: setters clone each element; nested pointers/maps would also need cloning.
	input := []*message{{text: "secret"}, {text: "deny"}}
	rule := guardy.ValidatorFunc[string](func(_ context.Context, text string) (string, *guardy.Report, error) {
		if text == "deny" {
			return text, &guardy.Report{Action: guardy.ActionBlock}, nil
		}
		return "clean", &guardy.Report{Action: guardy.ActionRedact}, nil
	})
	mapped := MapSlice(func(m *message) string { return m.text },
		func(m *message, text string) *message { copied := *m; copied.text = text; return &copied }, rule)
	// Act.
	output, report, err := mapped.Validate(context.Background(), input)
	// Assert: private rewritten copies are discarded, no rollback of aliases is required.
	if err != nil || !guardy.DecisionFromReport(report).IsTerminal() || output[0] != input[0] ||
		input[0].text != "secret" ||
		input[1].text != "deny" {
		t.Fatal("copy-on-write mapping changed caller aliases after deny")
	}
}
