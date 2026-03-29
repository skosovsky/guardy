package guardyotel

import (
	"testing"
	"time"
)

func TestDurationMillis_SubMillisecondPrecision(t *testing.T) {
	got := durationMillis(1500 * time.Microsecond)
	if got != 1.5 {
		t.Fatalf("durationMillis = %v, want 1.5", got)
	}
}

func TestPayloadAttributes_IncludeFalse(t *testing.T) {
	attrs := payloadAttributes(false, "in", "out")
	if len(attrs) != 0 {
		t.Fatalf("expected no attrs when disabled, got %d", len(attrs))
	}
}

func TestPayloadAttributes_IncludeTrue(t *testing.T) {
	attrs := payloadAttributes(true, "in-text", "out-text")
	if len(attrs) != 2 {
		t.Fatalf("expected 2 attrs, got %d", len(attrs))
	}
	if string(attrs[0].Key) != "guardy.input" {
		t.Fatalf("first key = %q", attrs[0].Key)
	}
	if string(attrs[1].Key) != "guardy.output" {
		t.Fatalf("second key = %q", attrs[1].Key)
	}
}

func TestPayloadAttributes_IncludeTrueNonStrings(t *testing.T) {
	attrs := payloadAttributes(true, 1, struct{}{})
	if len(attrs) != 0 {
		t.Fatalf("non-string payloads must be ignored, got %d attrs", len(attrs))
	}
}
