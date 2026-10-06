package guardyotel

import (
	"context"
	"reflect"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/guardy"
)

// Config controls telemetry behavior.
type Config struct {
	Tracer            trace.Tracer
	Meter             metric.Meter
	IncludePayloads   bool
	AllowedValidators map[string]struct{}
	AllowedCodes      map[string]struct{}
}

// Option configures guardyotel middleware.
type Option func(*Config)

// WithTracer overrides default tracer.
func WithTracer(tracer trace.Tracer) Option {
	return func(c *Config) {
		c.Tracer = tracer
	}
}

// WithMeter overrides default meter.
func WithMeter(meter metric.Meter) Option {
	return func(c *Config) {
		c.Meter = meter
	}
}

// WithIncludePayloads enables raw string payload capture in parallel phase spans.
// Disabled payload capture does not authorize raw validator errors or report text;
// neither is exported. This middleware is not an audit ledger of all decisions.
func WithIncludePayloads(include bool) Option {
	return func(c *Config) {
		c.IncludePayloads = include
	}
}

// WithAllowedMetadata permits exact, caller-approved validator and code labels.
// Unlisted labels are omitted. Values must be static non-sensitive identifiers;
// identifiers longer than 128 bytes are ignored to bound exported dimensions.
func WithAllowedMetadata(validators, codes []string) Option {
	return func(c *Config) {
		c.AllowedValidators = allowedLabels(validators)
		c.AllowedCodes = allowedLabels(codes)
	}
}

func allowedLabels(labels []string) map[string]struct{} {
	const maxLabelBytes = 128
	out := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		if label != "" && len(label) <= maxLabelBytes {
			out[label] = struct{}{}
		}
	}
	return out
}

type recorder[T any] struct {
	cfg     Config
	calls   metric.Int64Counter
	latency metric.Float64Histogram
}

// NewMiddleware builds ValidatorMiddleware with sequential phase metrics and parallel phase tracing.
// It exports canonical per-call decisions, not final delivery or an authorization ledger.
// Its own partial/unit/final support never upgrades delegate or inner middleware capabilities.
// Instrument setup errors return nil middleware and a safe ConfigurationError with
// the original cause. Nil meter/tracer explicitly disable a channel; typed-nil
// providers and instruments are invalid. Provider construction panics are not recovered.
func NewMiddleware[T any](opts ...Option) (guardy.ValidatorMiddleware[T], error) {
	cfg := Config{
		Tracer:            otel.Tracer("guardy/ext/guardyotel"),
		Meter:             otel.Meter("guardy/ext/guardyotel"),
		IncludePayloads:   false,
		AllowedValidators: nil,
		AllowedCodes:      nil,
	}
	for _, opt := range opts {
		if opt == nil {
			return nil, setupError("options", "nil", nil)
		}
		opt(&cfg)
	}
	if cfg.Meter != nil && nilTelemetryObject(cfg.Meter) {
		return nil, setupError("meter", "nil_provider", nil)
	}
	if cfg.Tracer != nil && nilTelemetryObject(cfg.Tracer) {
		return nil, setupError("tracer", "nil_provider", nil)
	}
	rec, err := newRecorder[T](cfg)
	if err != nil {
		return nil, err
	}
	return func(next guardy.Validator[T]) guardy.Validator[T] {
		return telemetryValidator[T]{next: next, recorder: rec}
	}, nil
}

// MustMiddleware builds telemetry middleware or panics on NewMiddleware's setup error.
func MustMiddleware[T any](opts ...Option) guardy.ValidatorMiddleware[T] {
	middleware, err := NewMiddleware[T](opts...)
	if err != nil {
		panic(err)
	}
	return middleware
}

func setupError(field, code string, cause error) error {
	return &guardy.ConfigurationError{Component: "guardyotel", Field: field, Code: code, Cause: cause}
}

// telemetryValidator declares this wrapper's own stage support. Core independently
// checks its delegate and all other middleware layers before compiling a stream.
type telemetryValidator[T any] struct {
	next     guardy.Validator[T]
	recorder *recorder[T]
}

func (v telemetryValidator[T]) Validate(ctx context.Context, input T) (T, *guardy.Report, error) {
	return v.recorder.validate(ctx, v.next, input)
}

func (telemetryValidator[T]) StreamCapabilities() guardy.StreamCapabilities {
	return guardy.StreamCapabilities{Partial: true, Unit: true, Final: true, Lookbehind: 0, Lookahead: 0}
}

func nilTelemetryObject(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Map, reflect.Slice, reflect.Interface, reflect.Chan:
		return reflected.IsNil()
	default:
		return false
	}
}

func newRecorder[T any](cfg Config) (*recorder[T], error) {
	r := &recorder[T]{cfg: cfg, calls: nil, latency: nil}
	if cfg.Meter != nil {
		calls, err := cfg.Meter.Int64Counter("guardy.validator.calls")
		if err != nil {
			return nil, setupError("counter", "creation_failed", err)
		}
		if nilTelemetryObject(calls) {
			return nil, setupError("counter", "nil_instrument", nil)
		}
		latency, err := cfg.Meter.Float64Histogram("guardy.validator.latency_ms")
		if err != nil {
			return nil, setupError("histogram", "creation_failed", err)
		}
		if nilTelemetryObject(latency) {
			return nil, setupError("histogram", "nil_instrument", nil)
		}
		r.calls, r.latency = calls, latency
	}
	return r, nil
}

func (r *recorder[T]) validate(ctx context.Context, next guardy.Validator[T], input T) (T, *guardy.Report, error) {
	phase, ok := guardy.ValidationPhaseFromContext(ctx)
	if !ok {
		phase = guardy.ValidationPhaseSequential
	}

	start := time.Now()
	if phase == guardy.ValidationPhaseParallel && r.cfg.Tracer != nil {
		var span trace.Span
		ctx, span = r.cfg.Tracer.Start(ctx, "guardy.validator.parallel")
		defer span.End()

		out, rep, err := next.Validate(ctx, input)
		attrs := r.reportAttrs(phase, rep, err)
		span.SetAttributes(attrs...)
		if callDecision(rep, err).IsSystemFault() {
			span.SetStatus(codes.Error, "validator error")
		} else {
			span.SetStatus(codes.Ok, "ok")
		}
		if payloadAttrs := payloadAttributes(r.cfg.IncludePayloads, input, out); len(payloadAttrs) > 0 {
			span.SetAttributes(payloadAttrs...)
		}
		r.recordMetrics(ctx, phase, rep, err, time.Since(start))
		return out, rep, err
	}

	out, rep, err := next.Validate(ctx, input)
	r.recordMetrics(ctx, phase, rep, err, time.Since(start))
	return out, rep, err
}

func (r *recorder[T]) reportAttrs(phase guardy.ValidationPhase, rep *guardy.Report, err error) []attribute.KeyValue {
	decision := callDecision(rep, err)
	observed := decision
	if rep != nil && err == nil {
		copyReport := *rep
		copyReport.ShadowMode = false
		observed = guardy.DecisionFromReport(&copyReport)
	}
	outcome := decision.Disposition.String()
	if decision.Disposition == guardy.DispositionNone {
		outcome = decision.Action.String()
	}
	attrs := []attribute.KeyValue{
		attribute.String("guardy.phase", string(phase)),
		attribute.Bool("guardy.error", decision.IsSystemFault()),
		attribute.String("guardy.disposition", decision.Disposition.String()),
		attribute.String("guardy.outcome", outcome),
		attribute.String("guardy.observed_disposition", observed.Disposition.String()),
		attribute.Bool("guardy.observation", err == nil && rep.IsObservation()),
	}
	if rep != nil {
		attrs = append(attrs,
			attribute.String("guardy.action", rep.Action.String()),
		)
		if _, approved := r.cfg.AllowedValidators[rep.Validator]; approved {
			attrs = append(attrs, attribute.String("guardy.validator", rep.Validator))
		}
		if _, approved := r.cfg.AllowedCodes[rep.Code]; approved {
			attrs = append(attrs, attribute.String("guardy.code", rep.Code))
		}
		if rep.Severity == guardy.SeverityLow || rep.Severity == guardy.SeverityMedium ||
			rep.Severity == guardy.SeverityHigh ||
			rep.Severity == guardy.SeverityCritical {
			attrs = append(attrs, attribute.String("guardy.severity", string(rep.Severity)))
		}
	}
	return attrs
}

// callDecision delegates report validity, escalation and shadow semantics to core.
func callDecision(rep *guardy.Report, err error) guardy.Decision {
	if err != nil {
		return guardy.DecisionFromReport(
			&guardy.Report{Action: guardy.ActionPass, Disposition: guardy.DispositionSystemFault},
		)
	}
	return guardy.DecisionFromReport(rep)
}

func (r *recorder[T]) recordMetrics(
	ctx context.Context,
	phase guardy.ValidationPhase,
	rep *guardy.Report,
	err error,
	elapsed time.Duration,
) {
	attrs := r.reportAttrs(phase, rep, err)
	if r.calls != nil {
		r.calls.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if r.latency != nil {
		r.latency.Record(ctx, durationMillis(elapsed), metric.WithAttributes(attrs...))
	}
}

func durationMillis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func payloadAttributes(include bool, input, output any) []attribute.KeyValue {
	if !include {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, 2)
	if in, ok := input.(string); ok {
		attrs = append(attrs, attribute.String("guardy.input", in))
	}
	if out, ok := output.(string); ok {
		attrs = append(attrs, attribute.String("guardy.output", out))
	}
	return attrs
}
