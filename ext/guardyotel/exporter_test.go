package guardyotel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/guardy"
)

func TestExporterSafeModeOmitsPayloadReportAndErrors(t *testing.T) {
	// Arrange: arbitrary third-party report metadata and errors can contain secrets.
	const secret = "private-secret-alice@example.com"
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()); _ = meterProvider.Shutdown(context.Background()) })
	validator := guardy.ValidatorFunc[string](func(context.Context, string) (string, *guardy.Report, error) {
		return secret, &guardy.Report{
			Action:    guardy.ActionBlock,
			Validator: secret,
			Code:      secret,
			Severity: guardy.Severity(
				secret,
			),
			Reason:          secret,
			Feedback:        secret,
			MutatedText:     secret,
			SafeUserMessage: secret,
		}, errors.New(secret)
	})
	pipeline := guardy.NewPipeline(guardy.WithSlowPath(validator)).
		Use(NewMiddleware[string](WithTracer(provider.Tracer("test")), WithMeter(meterProvider.Meter("test"))))
	// Act: traverse the real pipeline and collect actual exported spans and metric points.
	_, _ = pipeline.Run(context.Background(), nil, secret)
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("exported spans = %d, want 1", len(spans))
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	spanJSON, err := json.Marshal(spans)
	if err != nil {
		t.Fatal(err)
	}
	metricsJSON, err := json.Marshal(metrics)
	if err != nil {
		t.Fatal(err)
	}
	// Assert: every exported representation is free of payload, raw errors and arbitrary labels.
	for _, encoded := range [][]byte{spanJSON, metricsJSON} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("secret in exported telemetry: %s", encoded)
		}
	}
	if len(spans[0].Events) != 0 || spans[0].Status.Description != "validator error" {
		t.Fatalf("unexpected error representation: %+v", spans[0])
	}
	if len(metrics.ScopeMetrics) == 0 || len(metrics.ScopeMetrics[0].Metrics) != 2 {
		t.Fatalf("expected actual counter and latency export: %+v", metrics)
	}
}

func TestExporterExplicitPayloadAndApprovedMetadata(t *testing.T) {
	// Arrange.
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	validator := guardy.ValidatorFunc[string](func(_ context.Context, input string) (string, *guardy.Report, error) {
		return input, &guardy.Report{
			Action:    guardy.ActionPass,
			Validator: "approved-rule",
			Code:      "APPROVED",
			Severity:  guardy.SeverityLow,
		}, nil
	})
	pipeline := guardy.NewPipeline(guardy.WithSlowPath(validator)).
		Use(NewMiddleware[string](WithTracer(provider.Tracer("test")), WithMeter(nil), WithIncludePayloads(true), WithAllowedMetadata([]string{"approved-rule"}, []string{"APPROVED"})))
	// Act.
	_, err := pipeline.Run(context.Background(), nil, "explicit payload")
	if err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	// Assert.
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	attrs := make(map[string]string)
	for _, attr := range spans[0].Attributes {
		attrs[string(attr.Key)] = attr.Value.String()
	}
	for key, want := range map[string]string{"guardy.input": "explicit payload", "guardy.output": "explicit payload", "guardy.validator": "approved-rule", "guardy.code": "APPROVED"} {
		if attrs[key] != want {
			t.Fatalf("%s = %q, want %q", key, attrs[key], want)
		}
	}
}
