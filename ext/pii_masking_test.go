package ext

import (
	"context"
	"strings"
	"testing"

	"github.com/skosovsky/guardy"
)

func TestPIIValidator_Block_WithCode_NotRetryable(t *testing.T) {
	p := NewPIIValidator(
		WithAction(guardy.ActionBlock),
		WithCode("PII"),
	)
	_, rep, err := p.Validate(context.Background(), "Contact user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionBlock {
		t.Fatalf("action = %v", rep.Action)
	}
	if rep.Code != "PII" {
		t.Fatalf("Code = %q, want PII", rep.Code)
	}
	if rep.Retryable {
		t.Fatal("block must not be Retryable")
	}
}

func TestPIIValidator_DefaultConfig_EmptyCode(t *testing.T) {
	p := NewPIIValidator()
	_, rep, err := p.Validate(context.Background(), "Contact user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Fatalf("action = %v", rep.Action)
	}
	if rep.Code != "" {
		t.Fatalf("Code = %q, want empty without WithCode (use ext.WithCode in production)", rep.Code)
	}
}

func TestPIIValidator_NoPII_Pass(t *testing.T) {
	p := NewPIIValidator()
	ctx := context.Background()
	_, rep, err := p.Validate(ctx, "Hello world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Errorf("got Action=%v", rep.Action)
	}
}

func TestPIIValidator_NoPII_PassPreservesMetadata(t *testing.T) {
	p := NewPIIValidator(WithCode("PII_RULE"), WithSeverity(guardy.SeverityMedium))
	_, rep, err := p.Validate(context.Background(), "Hello world")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionPass {
		t.Fatalf("action = %v", rep.Action)
	}
	if rep.Code != "PII_RULE" {
		t.Fatalf("code = %q", rep.Code)
	}
	if rep.Severity != guardy.SeverityMedium {
		t.Fatalf("severity = %q", rep.Severity)
	}
}

func TestPIIValidator_Email_Redact(t *testing.T) {
	p := NewPIIValidator()
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
	if rep.Validator != "pii_validator" {
		t.Errorf("Validator = %q", rep.Validator)
	}
}

func TestPIIValidator_Phone_Redact(t *testing.T) {
	p := NewPIIValidator()
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

func TestPIIValidator_CustomReplacement(t *testing.T) {
	p := NewPIIValidator(WithRedactionReplacement("[PII]"))
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

func TestPIIValidator_WithName(t *testing.T) {
	p := NewPIIValidator(WithName("custom-pii"))
	_, rep, err := p.Validate(context.Background(), "Email: a@b.co")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Validator != "custom-pii" {
		t.Errorf("Validator = %q, want custom-pii", rep.Validator)
	}
}

func TestPIIValidator_WithTokenVault(t *testing.T) {
	vault := NewInMemoryTokenVault()
	p := NewPIIValidator(WithTokenVault(vault))
	_, rep, err := p.Validate(context.Background(), "Email: user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Fatalf("action = %v", rep.Action)
	}
	if !strings.Contains(rep.MutatedText, "[GUARDY_TOKEN_PII_") {
		t.Fatalf("expected tokenized output, got %q", rep.MutatedText)
	}
	restored := UnredactText(rep.MutatedText, vault)
	if !strings.Contains(restored, "user@example.com") {
		t.Fatalf("restored text = %q", restored)
	}
}

func TestPIIValidator_WithTypedNilTokenVault_FallbackReplacement(t *testing.T) {
	var vault *InMemoryTokenVault
	p := NewPIIValidator(
		WithTokenVault(vault),
		WithRedactionReplacement("[X]"),
	)
	_, rep, err := p.Validate(context.Background(), "Email: user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Action != guardy.ActionRedact {
		t.Fatalf("action = %v", rep.Action)
	}
	if strings.Contains(rep.MutatedText, "user@example.com") {
		t.Fatalf("expected fallback replacement redaction, got %q", rep.MutatedText)
	}
	if !strings.Contains(rep.MutatedText, "[X]") {
		t.Fatalf("expected replacement marker, got %q", rep.MutatedText)
	}
}
