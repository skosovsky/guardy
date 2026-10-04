package guardyotel

import (
	"context"
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

// WithIncludePayloads enables raw string payload capture in slow-path spans.
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

// NewMiddleware builds ValidatorMiddleware with fast-path metrics and slow-path tracing.
func NewMiddleware[T any](opts ...Option) guardy.ValidatorMiddleware[T] {
	cfg := Config{
		Tracer:            otel.Tracer("guardy/ext/guardyotel"),
		Meter:             otel.Meter("guardy/ext/guardyotel"),
		IncludePayloads:   false,
		AllowedValidators: nil,
		AllowedCodes:      nil,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	rec := newRecorder[T](cfg)
	return func(next guardy.Validator[T]) guardy.Validator[T] {
		return guardy.ValidatorFunc[T](func(ctx context.Context, input T) (T, *guardy.Report, error) {
			return rec.validate(ctx, next, input)
		})
	}
}

func newRecorder[T any](cfg Config) *recorder[T] {
	r := &recorder[T]{
		cfg:     cfg,
		calls:   nil,
		latency: nil,
	}
	if cfg.Meter != nil {
		if c, err := cfg.Meter.Int64Counter("guardy.validator.calls"); err == nil {
			r.calls = c
		}
		if h, err := cfg.Meter.Float64Histogram("guardy.validator.latency_ms"); err == nil {
			r.latency = h
		}
	}
	return r
}

func (r *recorder[T]) validate(ctx context.Context, next guardy.Validator[T], input T) (T, *guardy.Report, error) {
	phase, ok := guardy.ValidationPhaseFromContext(ctx)
	if !ok {
		phase = guardy.ValidationPhaseFast
	}

	start := time.Now()
	if phase == guardy.ValidationPhaseSlow && r.cfg.Tracer != nil {
		var span trace.Span
		ctx, span = r.cfg.Tracer.Start(ctx, "guardy.validator.slow")
		defer span.End()

		out, rep, err := next.Validate(ctx, input)
		attrs := r.reportAttrs(phase, rep, err)
		span.SetAttributes(attrs...)
		if err != nil {
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
	attrs := []attribute.KeyValue{
		attribute.String("guardy.phase", string(phase)),
		attribute.Bool("guardy.error", err != nil),
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
