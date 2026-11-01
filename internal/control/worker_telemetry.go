package control

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/endless-net/client/internal/state"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type workerTelemetry struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	events   metric.Int64Counter
	depth    metric.Int64Gauge
	jobs     metric.Int64Counter
	duration metric.Float64Histogram
}

func newWorkerTelemetry(tracer trace.Tracer, meter metric.Meter, logger *slog.Logger) (*workerTelemetry, error) {
	if tracer == nil || meter == nil || logger == nil {
		return nil, ErrNativeProtocol
	}
	events, err := meter.Int64Counter("client.enrollment.queue.events")
	if err != nil {
		return nil, ErrNativeProtocol
	}
	depth, err := meter.Int64Gauge("client.enrollment.queue.depth")
	if err != nil {
		return nil, ErrNativeProtocol
	}
	jobs, err := meter.Int64Counter("client.enrollment.jobs")
	if err != nil {
		return nil, ErrNativeProtocol
	}
	duration, err := meter.Float64Histogram("client.enrollment.job.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, ErrNativeProtocol
	}
	return &workerTelemetry{tracer: tracer, logger: logger, events: events, depth: depth, jobs: jobs, duration: duration}, nil
}

func (t *workerTelemetry) queued(ctx context.Context, outcome string, depth int) {
	t.events.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	t.depth.Record(ctx, int64(depth))
}

func jobCause(ctx context.Context) trace.SpanContext {
	cause := trace.SpanContextFromContext(ctx)
	// Keep only valid identifiers/flags. No untrusted tracestate, baggage,
	// identity, profile, token or durable enrollment input is stored here.
	if !cause.IsValid() {
		return trace.SpanContext{}
	}
	return trace.NewSpanContext(trace.SpanContextConfig{TraceID: cause.TraceID(), SpanID: cause.SpanID(), TraceFlags: cause.TraceFlags()})
}

func (t *workerTelemetry) start(ctx context.Context, job loginJob) (context.Context, func(time.Time, error)) {
	options := []trace.SpanStartOption{trace.WithNewRoot(), trace.WithAttributes(attribute.Bool("recovered", job.recovered))}
	if job.cause.IsValid() {
		options = append(options, trace.WithLinks(trace.Link{SpanContext: job.cause}))
	}
	_, span := t.tracer.Start(ctx, "client.enrollment.job", options...)
	// Preserve the application's cancellation/deadline and all non-telemetry
	// context values even if a supplied tracer returns a different context.
	ctx = trace.ContextWithSpan(ctx, span)
	started := time.Now()
	return ctx, func(due time.Time, err error) {
		defer span.End()
		outcome := "completed"
		if !due.IsZero() {
			outcome = "rescheduled"
		}
		if err != nil {
			switch {
			case errors.Is(err, state.ErrStale), errors.Is(err, state.ErrUncertain):
				outcome = "retry"
			case errors.Is(err, context.Canceled):
				outcome = "cancelled"
			case errors.Is(err, context.DeadlineExceeded):
				outcome = "deadline"
			default:
				outcome = "failure"
			}
		}
		if outcome == "failure" || outcome == "deadline" {
			span.SetStatus(codes.Error, "client.enrollment.job.failed")
		}
		span.SetAttributes(attribute.String("outcome", outcome))
		labels := metric.WithAttributes(attribute.String("outcome", outcome), attribute.Bool("recovered", job.recovered))
		t.jobs.Add(ctx, 1, labels)
		t.duration.Record(ctx, time.Since(started).Seconds(), labels)
		level := slog.LevelInfo
		if outcome == "failure" || outcome == "deadline" {
			level = slog.LevelWarn
		}
		fields := []any{"component", "enrollment", "outcome", outcome, "recovered", job.recovered}
		if sc := span.SpanContext(); sc.IsValid() {
			fields = append(fields, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
		}
		t.logger.Log(ctx, level, "client.enrollment.job", fields...)
	}
}
