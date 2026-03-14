package guardy

import (
	"testing"
)

func TestAction_String(t *testing.T) {
	if got := string(ActionBlock); got != "block" {
		t.Errorf("ActionBlock = %q, want block", got)
	}
	if got := string(ActionPass); got != "pass" {
		t.Errorf("ActionPass = %q, want pass", got)
	}
	if got := string(ActionRedact); got != "redact" {
		t.Errorf("ActionRedact = %q, want redact", got)
	}
}

func TestReport_ZeroValue(t *testing.T) {
	var rep Report
	if rep.Action != "" {
		t.Errorf("zero Report Action = %q", rep.Action)
	}
	if rep.Validator != "" {
		t.Errorf("zero Report Validator = %q", rep.Validator)
	}
}
