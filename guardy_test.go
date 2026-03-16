package guardy

import (
	"testing"
)

func TestAction_String(t *testing.T) {
	if got := ActionBlock.String(); got != "block" {
		t.Errorf("ActionBlock.String() = %q, want block", got)
	}
	if got := ActionPass.String(); got != "pass" {
		t.Errorf("ActionPass.String() = %q, want pass", got)
	}
	if got := ActionRedact.String(); got != "redact" {
		t.Errorf("ActionRedact.String() = %q, want redact", got)
	}
}

func TestReport_ZeroValue(t *testing.T) {
	var rep Report
	if rep.Action != ActionPass {
		t.Errorf("zero Report Action = %v, want ActionPass", rep.Action)
	}
	if rep.Validator != "" {
		t.Errorf("zero Report Validator = %q", rep.Validator)
	}
}
