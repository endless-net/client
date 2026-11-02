package clientapp

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

var ErrSignals = errors.New("client telemetry unavailable")

// Signals uses caller-owned providers and logger. It starts no exporters, sets
// no global providers and never accepts profile, credential or state values.
type Signals struct {
	logger          *slog.Logger
	tracer          trace.Tracer
	count           metric.Int64Counter
	duration        metric.Float64Histogram
	requests        metric.Int64Counter
	requestDuration metric.Float64Histogram
}

func (s *Signals) valid() bool {
	return s != nil && s.logger != nil && s.tracer != nil && s.count != nil && s.duration != nil && s.requests != nil && s.requestDuration != nil
}

func NewSignals(logger *slog.Logger, tracer trace.Tracer, meter metric.Meter) (*Signals, error) {
	if logger == nil || tracer == nil || meter == nil {
		return nil, ErrSignals
	}
	count, err := meter.Int64Counter("client.lifecycle.operations")
	if err != nil {
		return nil, ErrSignals
	}
	duration, err := meter.Float64Histogram("client.lifecycle.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, ErrSignals
	}
	requests, err := meter.Int64Counter("client.requests")
	if err != nil {
		return nil, ErrSignals
	}
	requestDuration, err := meter.Float64Histogram("client.request.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, ErrSignals
	}
	return &Signals{logger: logger.With("service", "client"), tracer: tracer, count: count, duration: duration, requests: requests, requestDuration: requestDuration}, nil
}

func (s *Signals) hook(component string, start, stop func(context.Context) error) fx.Hook {
	return fx.Hook{
		OnStart: func(ctx context.Context) error { return s.observe(ctx, component, "start", start) },
		OnStop:  func(ctx context.Context) error { return s.observe(ctx, component, "stop", stop) },
	}
}

func (s *Signals) observe(ctx context.Context, component, phase string, operation func(context.Context) error) error {
	// Every call site chooses a fixed value. Reject arbitrary strings before
	// they can become a metric label, span attribute or log field.
	if !s.valid() || operation == nil || (component != "state" && component != "ipc" && component != "workers" && component != "enrollment" && component != "connection" && component != "control_http") || (phase != "start" && phase != "stop" && phase != "run") {
		return ErrSignals
	}
	attributes := []attribute.KeyValue{attribute.String("component", component), attribute.String("phase", phase)}
	ctx, span := s.tracer.Start(ctx, "client.lifecycle", trace.WithAttributes(attributes...))
	defer span.End()
	started := time.Now()
	err := operation(ctx)
	outcome, level := "success", slog.LevelInfo
	if err != nil {
		outcome, level = "failure", slog.LevelError
		switch {
		case errors.Is(err, context.Canceled):
			outcome, level = "cancelled", slog.LevelInfo
		case errors.Is(err, context.DeadlineExceeded):
			outcome = "deadline"
		}
		if outcome != "cancelled" {
			span.SetStatus(codes.Error, "client.component.unavailable")
		}
	}
	attributes = append(attributes, attribute.String("outcome", outcome))
	span.SetAttributes(attribute.String("outcome", outcome))
	s.count.Add(ctx, 1, metric.WithAttributes(attributes...))
	s.duration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attributes...))
	fields := []any{"component", component, "phase", phase, "outcome", outcome}
	if err != nil && outcome != "cancelled" {
		fields = append(fields, "error_code", "client.component.unavailable")
	}
	if sc := span.SpanContext(); sc.IsValid() {
		fields = append(fields, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	}
	// Do not RecordError(err) or log err: transport/backend diagnostics can
	// contain plaintext credentials, usernames, paths or cryptographic input.
	s.logger.Log(ctx, level, "client.lifecycle", fields...)
	return err
}
