package clientapp

import (
	"context"
	"io"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

type TelemetryConfig struct {
	LogLevel     string  `env:"LOG_LEVEL,required"`
	LogFormat    string  `env:"LOG_FORMAT,required"`
	LogQueueSize int     `env:"LOG_QUEUE_SIZE,required"`
	MaxLogBytes  int     `env:"MAX_LOG_BYTES,required"`
	SampleRatio  float64 `env:"SAMPLE_RATIO,required"`
	Export       string  `env:"TELEMETRY_EXPORT,required"`
}

func (c TelemetryConfig) Validate() error {
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return ErrSignals
	}
	if c.LogFormat != "json" || c.LogQueueSize <= 0 || c.MaxLogBytes <= 0 || c.LogQueueSize > math.MaxInt/c.MaxLogBytes || math.IsNaN(c.SampleRatio) || math.IsInf(c.SampleRatio, 0) || c.SampleRatio < 0 || c.SampleRatio > 1 || c.Export != "disabled" {
		return ErrSignals
	}
	return nil
}

// Telemetry owns local SDK providers and a bounded asynchronous JSON sink.
// Export is explicitly disabled in this adapter: no exporter, collector URL,
// environment autodetection, global provider or network callback is installed.
type Telemetry struct {
	signals   *Signals
	traces    *sdktrace.TracerProvider
	metrics   *sdkmetric.MeterProvider
	reader    *sdkmetric.ManualReader
	logs      *logQueue
	mu        sync.Mutex
	failed    bool
	closeOnce sync.Once
	closed    chan struct{}
}

func NewTelemetry(lifecycle fx.Lifecycle, c TelemetryConfig, output io.Writer) (*Telemetry, error) {
	if lifecycle == nil || output == nil || c.Validate() != nil {
		return nil, ErrSignals
	}
	r := resource.NewSchemaless(attribute.String("service.name", "client"))
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(r), sdktrace.WithSampler(sdktrace.TraceIDRatioBased(c.SampleRatio)))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(r), sdkmetric.WithReader(reader))
	dropped, err := mp.Meter("endlessnet/client").Int64Counter("client.logs.dropped")
	if err != nil {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
		return nil, ErrSignals
	}
	q := &logQueue{output: output, queue: make(chan []byte, c.LogQueueSize), done: make(chan struct{}), max: c.MaxLogBytes, dropped: dropped}
	level := map[string]slog.Level{"debug": slog.LevelDebug, "info": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError}[c.LogLevel]
	logger := slog.New(slog.NewJSONHandler(q, &slog.HandlerOptions{Level: level}))
	signals, err := NewSignals(logger, tp.Tracer("endlessnet/client"), mp.Meter("endlessnet/client"))
	if err != nil {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
		return nil, ErrSignals
	}
	t := &Telemetry{signals: signals, traces: tp, metrics: mp, reader: reader, logs: q, closed: make(chan struct{})}
	// Register first: other components stop before their signal sink/providers.
	lifecycle.Append(fx.Hook{OnStart: t.start, OnStop: t.stop})
	return t, nil
}

func (t *Telemetry) Signals() *Signals   { return t.signals }
func (t *Telemetry) DroppedLogs() uint64 { return t.logs.total.Load() }

func (t *Telemetry) start(ctx context.Context) error {
	if err := t.logs.start(ctx); err != nil {
		_ = t.stop(ctx)
		return ErrSignals
	}
	return nil
}

func (t *Telemetry) stop(ctx context.Context) error {
	t.closeOnce.Do(func() {
		t.logs.closeAdmission()
		go func() {
			err := t.logs.stop(ctx)
			if e := t.traces.Shutdown(ctx); e != nil {
				err = ErrSignals
			}
			if e := t.metrics.Shutdown(ctx); e != nil {
				err = ErrSignals
			}
			t.mu.Lock()
			t.failed = t.failed || err != nil
			t.mu.Unlock()
			close(t.closed)
		}()
	})
	if _, bounded := ctx.Deadline(); !bounded {
		return t.failStop()
	}
	select {
	case <-ctx.Done():
		return t.failStop()
	case <-t.closed:
		if ctx.Err() != nil {
			return t.failStop()
		}
		t.mu.Lock()
		failed := t.failed
		t.mu.Unlock()
		if failed {
			return ErrSignals
		}
		return nil
	}
}

func (t *Telemetry) failStop() error { t.mu.Lock(); t.failed = true; t.mu.Unlock(); return ErrSignals }

type logQueue struct {
	mu              sync.Mutex
	output          io.Writer
	queue           chan []byte
	done            chan struct{}
	max             int
	started, closed bool
	dropped         metric.Int64Counter
	total           atomic.Uint64
}

func (q *logQueue) drop(reason string) {
	q.total.Add(1)
	q.dropped.Add(context.Background(), 1, metric.WithAttributes(attribute.String("reason", reason)))
}

func (q *logQueue) Write(data []byte) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.started || q.closed {
		q.drop("inactive")
		return len(data), nil
	}
	if len(data) > q.max {
		q.drop("too_large")
		return len(data), nil
	}
	select {
	case q.queue <- append([]byte(nil), data...):
	default:
		q.drop("queue_full")
	}
	return len(data), nil // Optional diagnostics never become a domain failure.
}

func (q *logQueue) start(ctx context.Context) error {
	if _, bounded := ctx.Deadline(); !bounded || ctx.Err() != nil {
		return ErrSignals
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.started || q.closed {
		return ErrSignals
	}
	q.started = true
	go func() {
		defer close(q.done)
		for data := range q.queue {
			if n, err := q.output.Write(data); err != nil || n != len(data) {
				q.drop("sink_failure")
			}
		}
	}()
	return nil
}

func (q *logQueue) stop(ctx context.Context) error {
	q.closeAdmission()
	q.mu.Lock()
	started := q.started
	q.mu.Unlock()
	if !started {
		return nil
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrSignals
	}
	select {
	case <-q.done:
		if ctx.Err() != nil {
			return ErrSignals
		}
		return nil
	case <-ctx.Done():
		return ErrSignals
	}
}

func (q *logQueue) closeAdmission() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.queue)
	}
}

// FXLogger deliberately ignores constructor names/arguments and raw event
// errors. Only fixed lifecycle outcomes enter the bounded application logger.
func (t *Telemetry) FXLogger() fxevent.Logger { return safeFXLogger{logger: t.signals.logger} }

type safeFXLogger struct{ logger *slog.Logger }

func (l safeFXLogger) LogEvent(event fxevent.Event) {
	phase, failed := "", false
	switch e := event.(type) {
	case *fxevent.Started:
		phase, failed = "start", e.Err != nil
	case *fxevent.Stopped:
		phase, failed = "stop", e.Err != nil
	case *fxevent.RollingBack:
		phase, failed = "rollback", true
	default:
		return
	}
	outcome, level := "success", slog.LevelInfo
	if failed {
		outcome, level = "failure", slog.LevelError
	}
	l.logger.Log(context.Background(), level, "client.fx", "component", "fx", "phase", phase, "outcome", outcome)
}
