package ext

import (
	"context"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestPIIMasking_NoPII_Pass(t *testing.T) {
	p := NewPIIMasking()
	ctx := context.Background()
	_, rep, err := p.Validate(ctx, "Hello world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Errorf("got Action=%v", rep.Action)
	}
}

func TestPIIMasking_Email_Redact(t *testing.T) {
	p := NewPIIMasking()
	ctx := context.Background()
	_, rep, err := p.Validate(ctx, "Contact me at user@example.com please")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Errorf("got Action=%v", rep.Action)
	}
	if rep.MutatedText == "" || strings.Contains(rep.MutatedText, "user@example.com") {
		t.Errorf("MutatedText should redact email, got %q", rep.MutatedText)
	}
	if rep.Validator != "pii_masking" {
		t.Errorf("Validator = %q", rep.Validator)
	}
}

func TestPIIMasking_Phone_Redact(t *testing.T) {
	p := NewPIIMasking()
	ctx := context.Background()
	_, rep, err := p.Validate(ctx, "Call 555-123-4567")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Errorf("got Action=%v", rep.Action)
	}
	if strings.Contains(rep.MutatedText, "555") {
		t.Errorf("MutatedText should redact phone, got %q", rep.MutatedText)
	}
}

func TestPIIMasking_CustomReplacement(t *testing.T) {
	p := NewPIIMasking(WithPIIReplacement("[PII]"))
	ctx := context.Background()
	_, rep, err := p.Validate(ctx, "Email: a@b.co")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Errorf("got Action=%v", rep.Action)
	}
	if !strings.Contains(rep.MutatedText, "[PII]") {
		t.Errorf("MutatedText = %q, should contain [PII]", rep.MutatedText)
	}
}

func TestPIIMasking_Name(t *testing.T) {
	p := NewPIIMasking(WithPIIName("custom-pii"))
	if p.Name() != "custom-pii" {
		t.Errorf("Name() = %q, want custom-pii", p.Name())
	}
}
