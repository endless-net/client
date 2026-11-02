package clientapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/codes"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/fx"
)

// No-op providers are used only by lifecycle-focused fixtures; the dedicated
// telemetry fixture below uses actual SDK providers and collects both signals.
func testSignals(t *testing.T) *Signals {
	t.Helper()
	s, err := NewSignals(slog.New(slog.NewJSONHandler(io.Discard, nil)), tracenoop.NewTracerProvider().Tracer("fixture"), metricnoop.NewMeterProvider().Meter("fixture"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type blockedRunLog struct {
	entered, release chan struct{}
	once             sync.Once
}

func (*blockedRunLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *blockedRunLog) WithAttrs([]slog.Attr) slog.Handler     { return h }
func (h *blockedRunLog) WithGroup(string) slog.Handler          { return h }
func (h *blockedRunLog) Handle(_ context.Context, record slog.Record) error {
	block := false
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "phase" && attr.Value.String() == "run" {
			block = true
		}
		return true
	})
	if block {
		h.once.Do(func() { close(h.entered) })
		<-h.release
	}
	return nil
}

func TestWorkerFailurePrecedesBlockedTelemetryDelivery(t *testing.T) {
	handler := &blockedRunLog{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	releaseSink := func() { releaseOnce.Do(func() { close(handler.release) }) }
	defer releaseSink()
	signals, err := NewSignals(slog.New(handler), tracenoop.NewTracerProvider().Tracer("fixture"), metricnoop.NewMeterProvider().Meter("fixture"))
	if err != nil {
		t.Fatal(err)
	}
	var workers *Workers
	releaseWorker := make(chan struct{})
	app := fx.New(fx.NopLogger, fx.Invoke(func(lc fx.Lifecycle, shutdown fx.Shutdowner) error {
		var err error
		workers, err = NewWorkers(lc, shutdown, signals, func(context.Context) error { <-releaseWorker; return nil }, waitCancelled, waitCancelled)
		return err
	}))
	if err := app.Start(budget(t)); err != nil {
		t.Fatal(err)
	}
	shutdown := app.Wait()
	close(releaseWorker)
	select {
	case <-handler.entered:
	case <-budget(t).Done():
		t.Fatal("run signal did not reach sink")
	}
	if workers.Healthy() {
		t.Fatal("slow telemetry hid mandatory failure")
	}
	select {
	case signal := <-shutdown:
		if signal.ExitCode != 1 {
			t.Fatal(signal)
		}
	case <-budget(t).Done():
		t.Fatal("slow telemetry delayed shutdown")
	}
	// A blocked sink still cannot be claimed as a successful join. The caller's
	// Fx stop deadline bounds waiting, and releasing the sink permits cleanup.
	ctx, cancel := context.WithCancel(budget(t))
	cancel()
	if err := app.Stop(ctx); err == nil {
		t.Fatal("blocked sink reported clean stop")
	}
	releaseSink()
	select {
	case <-workers.done:
	case <-budget(t).Done():
		t.Fatal("worker join did not finish after sink release")
	}
}

func TestSignalsRejectMissingDependenciesAndUnboundedLabels(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	tracer := tracenoop.NewTracerProvider().Tracer("fixture")
	meter := metricnoop.NewMeterProvider().Meter("fixture")
	if _, err := NewSignals(nil, tracer, meter); err != ErrSignals {
		t.Fatal(err)
	}
	if _, err := NewSignals(logger, nil, meter); err != ErrSignals {
		t.Fatal(err)
	}
	if _, err := NewSignals(logger, tracer, nil); err != ErrSignals {
		t.Fatal(err)
	}
	s := testSignals(t)
	if err := s.observe(context.Background(), "profile-secret", "start", func(context.Context) error { t.Fatal("invalid observation executed"); return nil }); err != ErrSignals {
		t.Fatal(err)
	}
	if err := s.observe(context.Background(), "ipc", "request-secret", func(context.Context) error { t.Fatal("invalid observation executed"); return nil }); err != ErrSignals {
		t.Fatal(err)
	}
}

func TestSignalsCorrelateAndRedactRawFailures(t *testing.T) {
	var logs bytes.Buffer
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
		if err := tp.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	s, err := NewSignals(slog.New(slog.NewJSONHandler(&logs, nil)), tp.Tracer("client"), mp.Meter("client"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, parent := tp.Tracer("caller").Start(context.Background(), "fixture-parent")
	private := errors.New("synthetic-token private-key S-1-5-21-123 C:\\private-state")
	if err := s.observe(ctx, "state", "start", func(context.Context) error { return private }); err != private {
		t.Fatal("error changed", err)
	}
	if err := s.observe(ctx, "ipc", "stop", func(context.Context) error { return context.DeadlineExceeded }); err != context.DeadlineExceeded {
		t.Fatal(err)
	}
	if err := s.observe(ctx, "enrollment", "run", func(context.Context) error { return context.Canceled }); err != context.Canceled {
		t.Fatal(err)
	}
	parent.End()
	spans := recorder.Ended()
	if len(spans) != 4 {
		t.Fatal("missing spans", len(spans))
	}
	for i, span := range spans[:3] {
		if span.Name() != "client.lifecycle" || span.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Fatal("span correlation lost")
		}
		if i < 2 && span.Status().Code != codes.Error {
			t.Fatal("failure not marked")
		}
		if i == 2 && span.Status().Code != codes.Unset {
			t.Fatal("normal cancellation marked failure")
		}
		if len(span.Events()) != 0 {
			t.Fatal("raw errors recorded")
		}
	}
	if strings.Contains(logs.String(), "synthetic-token") || strings.Contains(logs.String(), "private-key") || strings.Contains(logs.String(), "S-1-5") || strings.Contains(logs.String(), "private-state") {
		t.Fatal("sensitive input disclosed")
	}
	decoder := json.NewDecoder(&logs)
	for range 3 {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event["service"] != "client" || event["msg"] != "client.lifecycle" || event["trace_id"] != parent.SpanContext().TraceID().String() {
			t.Fatal("missing structured correlation", event)
		}
		if _, err := trace.SpanIDFromHex(event["span_id"].(string)); err != nil {
			t.Fatal(err)
		}
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 2 {
		t.Fatal("missing counter or duration")
	}
	for _, instrument := range data.ScopeMetrics[0].Metrics {
		switch value := instrument.Data.(type) {
		case metricdata.Sum[int64]:
			if len(value.DataPoints) != 3 {
				t.Fatal("missing bounded outcomes")
			}
			for _, point := range value.DataPoints {
				if point.Value != 1 || point.Attributes.Len() != 3 {
					t.Fatal("unsafe or missing counter labels")
				}
			}
		case metricdata.Histogram[float64]:
			if len(value.DataPoints) != 3 {
				t.Fatal("missing latencies")
			}
			for _, point := range value.DataPoints {
				if point.Count != 1 || point.Attributes.Len() != 3 {
					t.Fatal("unsafe or missing histogram labels")
				}
			}
		default:
			t.Fatal("unexpected metric type")
		}
	}
}
