package guardyotel

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/guardy"
)

type setupFailMeter struct {
	metric.Meter

	counterErr, histogramErr     error
	emptyCounter, emptyHistogram bool
	typedCounter, typedHistogram bool
}

func (m setupFailMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if m.typedCounter {
		return (*absentCounter)(nil), nil
	}
	if m.counterErr != nil || m.emptyCounter {
		return nil, m.counterErr
	}
	return m.Meter.Int64Counter(name, opts...)
}

func (m setupFailMeter) Float64Histogram(
	name string,
	opts ...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	if m.typedHistogram {
		return (*absentHistogram)(nil), nil
	}
	if m.histogramErr != nil || m.emptyHistogram {
		return nil, m.histogramErr
	}
	return m.Meter.Float64Histogram(name, opts...)
}

type providerSetupError struct{}

func (*providerSetupError) Error() string { return "private provider credentials" }

func TestMiddlewareSetupErrorsAreExplicitSafeAndPreserveCause(t *testing.T) {
	for _, histogram := range []bool{false, true} {
		// Arrange.
		cause := &providerSetupError{}
		meter := setupFailMeter{Meter: noop.NewMeterProvider().Meter("test")}
		if histogram {
			meter.histogramErr = cause
		} else {
			meter.counterErr = cause
		}
		// Act.
		middleware, err := NewMiddleware[string](WithMeter(meter), WithTracer(nil))
		// Assert.
		var preserved *providerSetupError
		if middleware != nil || !errors.Is(err, guardy.ErrConfiguration) || !errors.Is(err, cause) ||
			!errors.As(err, &preserved) ||
			strings.Contains(err.Error(), cause.Error()) {
			t.Fatalf("setup failure admitted or exposed/lost cause: %v", err)
		}
		var escaped any
		func() { defer func() { escaped = recover() }(); MustMiddleware[string](WithMeter(meter)) }()
		panicErr, ok := escaped.(error)
		if !ok || !errors.Is(panicErr, cause) {
			t.Fatal("MustMiddleware did not preserve setup error")
		}
	}
}

func TestMiddlewareRejectsNilOptionsAndAbsentInstruments(t *testing.T) {
	var absentMeter *setupFailMeter
	var absentTracer *absentTrace

	for _, options := range [][]Option{{nil}, {WithMeter(absentMeter)}, {WithTracer(absentTracer)},
		{WithMeter(setupFailMeter{Meter: noop.NewMeterProvider().Meter("test"), typedCounter: true})},
		{WithMeter(setupFailMeter{Meter: noop.NewMeterProvider().Meter("test"), typedHistogram: true})},
		{WithMeter(setupFailMeter{Meter: noop.NewMeterProvider().Meter("test"), emptyCounter: true})},
		{WithMeter(setupFailMeter{Meter: noop.NewMeterProvider().Meter("test"), emptyHistogram: true})},
	} {
		// Arrange/Act.
		middleware, err := NewMiddleware[string](options...)
		// Assert.
		if middleware != nil || !errors.Is(err, guardy.ErrConfiguration) {
			t.Fatal("invalid setup admitted")
		}
	}
}

func TestExplicitDisabledTelemetryPreservesValidation(t *testing.T) {
	// Arrange: host intentionally chooses disabled channels after detecting setup failure.
	rule := guardy.ValidatorFunc[string](func(_ context.Context, value string) (string, *guardy.Report, error) {
		return value, &guardy.Report{Action: guardy.ActionBlock, Code: "DENY"}, nil
	})
	base := guardy.MustNewPipeline(guardy.WithSequential(rule))
	middleware, err := NewMiddleware[string](WithMeter(nil), WithTracer(nil))
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := base.Use(middleware)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	original, originalErr := base.Run(context.Background(), nil, "secret")
	observed, observedErr := wrapped.Run(context.Background(), nil, "secret")
	// Assert.
	if originalErr != nil || observedErr != nil || original.PolicyDecision() != observed.PolicyDecision() ||
		!observed.PolicyDecision().IsTerminal() {
		t.Fatal("explicitly disabled telemetry changed business validation")
	}
}

type absentTrace struct{ trace.Tracer }

type absentCounter struct{ metric.Int64Counter }
type absentHistogram struct{ metric.Float64Histogram }
