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

func TestReport_Clone(t *testing.T) {
	rep := &Report{
		Action:      ActionRedact,
		Validator:   "v",
		Code:        "RULE_1",
		Severity:    SeverityHigh,
		Reason:      "x",
		Feedback:    "y",
		Score:       0.7,
		ShadowMode:  true,
		MutatedText: "clean",
	}
	cp := rep.Clone()
	if cp == rep {
		t.Fatal("Clone must return a copy")
	}
	if *cp != *rep {
		t.Fatalf("clone mismatch: %+v vs %+v", cp, rep)
	}
}

func TestReport_CloneWithoutState(t *testing.T) {
	rep := &Report{
		Action:      ActionRedact,
		Code:        "RULE_1",
		Severity:    SeverityCritical,
		MutatedText: "secret",
	}
	cp := rep.CloneWithoutState()
	if cp.MutatedText != "" {
		t.Fatalf("MutatedText must be cleared, got %q", cp.MutatedText)
	}
	if cp.Code != "RULE_1" || cp.Severity != SeverityCritical {
		t.Fatalf("metadata must be preserved, got %+v", cp)
	}
}
