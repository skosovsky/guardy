package guardyotel

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/skosovsky/guardy"
)

func TestExporterCanonicalDecisionsAndTransparentMiddleware(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report *guardy.Report
		fault  error
	}{
		{name: "pass", report: &guardy.Report{Action: guardy.ActionPass}},
		{name: "redact", report: &guardy.Report{Action: guardy.ActionRedact, MutatedText: "clean"}},
		{name: "deny", report: &guardy.Report{Action: guardy.ActionBlock}},
		{name: "correction", report: &guardy.Report{Action: guardy.ActionRetry, Retryable: true}},
		{name: "explicit fault", report: &guardy.Report{Action: guardy.ActionPass, Disposition: guardy.DispositionSystemFault}},
		{name: "error without report", fault: errors.New("private provider error")},
		{name: "invalid action", report: &guardy.Report{Action: guardy.Action(-1)}},
		{name: "invalid disposition", report: &guardy.Report{Disposition: guardy.FailureDisposition(-1)}},
		{name: "invalid kind", report: &guardy.Report{PayloadKind: guardy.PayloadKind(-1)}},
		{name: "NaN", report: &guardy.Report{Score: math.NaN()}},
		{name: "Inf", report: &guardy.Report{Score: math.Inf(1)}},
		{name: "shadow block", report: &guardy.Report{Action: guardy.ActionBlock, ShadowMode: true}},
		{name: "fatal shadow", report: &guardy.Report{Action: guardy.ActionBlock, ShadowMode: true, Fatal: true}},
		{name: "invalid shadow", report: &guardy.Report{Action: guardy.ActionBlock, ShadowMode: true, Score: math.Inf(-1)}},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) { checkExporterCanonicalDecisionsAndTransparentMiddleware(t, tc) },
		)
	}
}

func sameReports(a, b []guardy.Report) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		left, right := a[i], b[i]
		if math.IsNaN(left.Score) && math.IsNaN(right.Score) {
			left.Score, right.Score = 0, 0
		}
		if !reflect.DeepEqual(left, right) {
			return false
		}
	}
	return true
}

func checkDecisionAttrs(t *testing.T, attrs attribute.Set, decision, observed guardy.Decision, observation bool) {
	t.Helper()
	outcome := decision.Disposition.String()
	if decision.Disposition == guardy.DispositionNone {
		outcome = decision.Action.String()
	}
	for key, want := range map[attribute.Key]string{
		"guardy.outcome": outcome, "guardy.disposition": decision.Disposition.String(), "guardy.observed_disposition": observed.Disposition.String()} {
		value, ok := attrs.Value(key)
		if !ok || value.AsString() != want {
			t.Fatalf("%s=%v want=%s", key, value, want)
		}
	}
	for key, want := range map[attribute.Key]bool{"guardy.error": decision.IsSystemFault(), "guardy.observation": observation} {
		value, ok := attrs.Value(key)
		if !ok || value.AsBool() != want {
			t.Fatalf("%s=%v want=%v", key, value, want)
		}
	}
	for _, key := range []attribute.Key{"guardy.score", "guardy.feedback", "guardy.reason", "guardy.input", "guardy.output"} {
		if _, ok := attrs.Value(key); ok {
			t.Fatalf("unexpected %s", key)
		}
	}
}

func checkMetricDecisions(
	t *testing.T,
	metrics metricdata.ResourceMetrics,
	decision, observed guardy.Decision,
	observation bool,
) {
	t.Helper()
	seen := 0
	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					checkDecisionAttrs(
						t,
						point.Attributes,
						decision,
						observed,
						observation,
					)
					seen++
				}
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					checkDecisionAttrs(
						t,
						point.Attributes,
						decision,
						observed,
						observation,
					)
					seen++
				}
			}
		}
	}
	if seen != 2 {
		t.Fatalf("metric points=%d", seen)
	}
}

func testCallPipeline(rule guardy.Validator[string], report *guardy.Report) (*guardy.Pipeline[string], int) {
	// Redaction is a sequential phase operation; parallel phase redaction is an orchestration
	// fault detected after the middleware returns, outside per-call telemetry.
	if report != nil && report.Action == guardy.ActionRedact {
		return guardy.MustNewPipeline(guardy.WithSequential(rule)), 0
	}
	return guardy.MustNewPipeline(guardy.WithParallel(rule)), 1
}

func checkExporterCanonicalDecisionsAndTransparentMiddleware(t *testing.T, tc struct {
	name   string
	report *guardy.Report
	fault  error
}) {
	t.Helper()
	// Arrange: collect real spans and metrics for a single slow rule invocation.
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	reader := sdkmetric.NewManualReader()
	metricsProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		_ = metricsProvider.Shutdown(context.Background())
	})
	rule := guardy.ValidatorFunc[string](
		func(context.Context, string) (string, *guardy.Report, error) { return "clean", tc.report, tc.fault },
	)
	base, expectedSpans := testCallPipeline(rule, tc.report)
	traced := base.MustUse(
		MustMiddleware[string](WithTracer(provider.Tracer("test")), WithMeter(metricsProvider.Meter("test"))),
	)
	// Act.
	original, originalErr := base.Run(t.Context(), nil, "raw")
	actual, actualErr := traced.Run(t.Context(), nil, "raw")
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &metrics); err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	// Assert: no changes to outputs, errors or report content (including NaN scores).
	if actual.Output != original.Output || actual.PolicyDecision() != original.PolicyDecision() ||
		errors.Is(actualErr, guardy.ErrValidatorFailed) != errors.Is(originalErr, guardy.ErrValidatorFailed) ||
		!sameReports(actual.Reports, original.Reports) {
		t.Fatalf("original=%+v actual=%+v errors=%v/%v", original, actual, originalErr, actualErr)
	}
	if tc.fault != nil && !errors.Is(actualErr, tc.fault) {
		t.Fatal("lost original cause")
	}
	if len(spans) != expectedSpans {
		t.Fatalf("spans=%d", len(spans))
	}
	decision := actual.PolicyDecision()
	if expectedSpans > 0 && decision.IsSystemFault() && spans[0].Status.Code != codes.Error {
		t.Fatalf("fault status=%+v", spans[0].Status)
	}
	if expectedSpans > 0 && !decision.IsSystemFault() && spans[0].Status.Code != codes.Ok {
		t.Fatalf("policy status=%+v", spans[0].Status)
	}
	observed := decision
	if tc.report != nil && tc.fault == nil {
		rep := *tc.report
		rep.ShadowMode = false
		observed = guardy.DecisionFromReport(&rep)
	}
	if expectedSpans > 0 {
		checkDecisionAttrs(
			t,
			attribute.NewSet(spans[0].Attributes...),
			decision,
			observed,
			tc.fault == nil && tc.report.IsObservation(),
		)
	}
	checkMetricDecisions(t, metrics, decision, observed, tc.fault == nil && tc.report.IsObservation())
}
