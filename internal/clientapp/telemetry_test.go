package clientapp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

func telemetryConfig() TelemetryConfig {
	return TelemetryConfig{LogLevel: "info", LogFormat: "json", LogQueueSize: 16, MaxLogBytes: 2048, SampleRatio: 1, Export: "disabled"}
}

func TestTelemetryValidatesWithoutStartingOutput(t *testing.T) {
	for _, mutate := range []func(*TelemetryConfig){
		func(c *TelemetryConfig) { c.Export = "otlp" }, func(c *TelemetryConfig) { c.LogFormat = "text" }, func(c *TelemetryConfig) { c.LogLevel = "unknown" },
		func(c *TelemetryConfig) { c.LogQueueSize = 0 }, func(c *TelemetryConfig) { c.MaxLogBytes = 0 }, func(c *TelemetryConfig) { c.LogQueueSize = math.MaxInt },
		func(c *TelemetryConfig) { c.SampleRatio = math.NaN() }, func(c *TelemetryConfig) { c.SampleRatio = math.Inf(1) }, func(c *TelemetryConfig) { c.SampleRatio = 1.1 },
	} {
		c := telemetryConfig()
		mutate(&c)
		if _, err := NewTelemetry(&hooks{}, c, io.Discard); err != ErrSignals {
			t.Fatal(err)
		}
	}
	if _, err := NewTelemetry(nil, telemetryConfig(), io.Discard); err != ErrSignals {
		t.Fatal(err)
	}
	if _, err := NewTelemetry(&hooks{}, telemetryConfig(), nil); err != ErrSignals {
		t.Fatal(err)
	}
	var output bytes.Buffer
	h := &hooks{}
	tel, err := NewTelemetry(h, telemetryConfig(), &output)
	if err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatal("constructor wrote to sink")
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	if err := tel.Signals().observe(context.Background(), "ipc", "start", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStop(budget(t)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"service":"client"`) || !strings.Contains(output.String(), `"msg":"client.lifecycle"`) {
		t.Fatal("local JSON not drained")
	}
}

type blockedWriter struct {
	entered, release chan struct{}
	once             sync.Once
}

func (w *blockedWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(data), nil
}

func TestTelemetryQueueOverflowNeverBlocksOperationsAndStopFailureIsRetained(t *testing.T) {
	writer := &blockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(writer.release) }) }
	defer release()
	c := telemetryConfig()
	c.LogQueueSize = 1
	h := &hooks{}
	tel, err := NewTelemetry(h, c, writer)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	if err := tel.Signals().observe(context.Background(), "ipc", "start", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writer.entered:
	case <-budget(t).Done():
		t.Fatal("sink not entered")
	}
	finished := make(chan struct{})
	go func() {
		for range 20 {
			_ = tel.Signals().observe(context.Background(), "ipc", "start", func(context.Context) error { return nil })
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-budget(t).Done():
		t.Fatal("blocked output delayed request completion")
	}
	if tel.DroppedLogs() == 0 {
		t.Fatal("queue overflow not observable")
	}
	var metrics metricdata.ResourceMetrics
	if err := tel.reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == "client.logs.dropped" {
				found = true
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok || len(sum.DataPoints) != 1 || sum.DataPoints[0].Attributes.Len() != 1 {
					t.Fatal("unbounded drop reasons")
				}
			}
		}
	}
	if !found {
		t.Fatal("drop metric missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := h.hook.OnStop(ctx); err != ErrSignals {
		t.Fatal("blocked sink claimed clean shutdown", err)
	}
	release()
	select {
	case <-tel.logs.done:
	case <-budget(t).Done():
		t.Fatal("sink did not join after release")
	}
	if err := h.hook.OnStop(budget(t)); err != ErrSignals {
		t.Fatal("shutdown failure forgotten", err)
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("synthetic-private-sink-error") }

func TestTelemetrySinkFailureDoesNotChangeDomainResult(t *testing.T) {
	h := &hooks{}
	tel, err := NewTelemetry(h, telemetryConfig(), failedWriter{})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	domain := errors.New("synthetic-private-domain-token")
	if err := tel.Signals().observe(context.Background(), "state", "start", func(context.Context) error { return domain }); err != domain {
		t.Fatal("diagnostic failure changed outcome", err)
	}
	if err := h.hook.OnStop(budget(t)); err != nil {
		t.Fatal(err)
	}
	if tel.DroppedLogs() != 1 {
		t.Fatal("sink failure not counted")
	}
}

func TestTelemetryActualFxRollbackSanitizesEvents(t *testing.T) {
	var output bytes.Buffer
	var tel *Telemetry
	app := fx.New(fx.Provide(func(lc fx.Lifecycle) (*Telemetry, error) {
		var err error
		tel, err = NewTelemetry(lc, telemetryConfig(), &output)
		return tel, err
	}),
		fx.WithLogger(func(t *Telemetry) fxevent.Logger { return t.FXLogger() }),
		fx.Invoke(func(lc fx.Lifecycle, _ *Telemetry) {
			lc.Append(fx.Hook{OnStart: func(context.Context) error { return errors.New("synthetic-private-fx-secret") }})
		}))
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	if err := app.Start(budget(t)); err == nil {
		t.Fatal("startup error lost")
	}
	if strings.Contains(output.String(), "synthetic-private") || !strings.Contains(output.String(), `"phase":"rollback"`) {
		t.Fatal("raw Fx error leaked or rollback not observed")
	}
	select {
	case <-tel.logs.done:
	case <-budget(t).Done():
		t.Fatal("rollback did not join sink")
	}
}

func TestConcurrentTelemetryStopsJoinOneCleanup(t *testing.T) {
	h := &hooks{}
	tel, err := NewTelemetry(h, telemetryConfig(), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.hook.OnStart(budget(t)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 16)
	for range 16 {
		go func() { done <- h.hook.OnStop(budget(t)) }()
	}
	for range 16 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-tel.closed:
	default:
		t.Fatal("stop returned before cleanup completed")
	}
}
