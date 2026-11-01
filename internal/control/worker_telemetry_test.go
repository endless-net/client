package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func workerTracer() trace.Tracer { return tracenoop.NewTracerProvider().Tracer("fixture") }
func workerMeter() metric.Meter  { return metricnoop.NewMeterProvider().Meter("fixture") }

func workerLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func TestWorkerJobsLinkWakeWithoutExtendingRequestOrPersistingTrace(t *testing.T) {
	for _, recovered := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit_wake", true: "restart"}[recovered], func(t *testing.T) {
			f := newRegistrationFixture(t)
			recorder := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithSpanProcessor(recorder))
			reader := sdkmetric.NewManualReader()
			mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() {
				if err := tp.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
				if err := mp.Shutdown(context.Background()); err != nil {
					t.Error(err)
				}
			})
			tracer, meter := tp.Tracer("client"), mp.Meter("client")
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			wake, parent := tracer.Start(context.Background(), "ipc-wake")
			parentID := parent.SpanContext()
			called := make(chan trace.SpanContext, 1)
			registration := f.executor(t, func(req *http.Request) (*http.Response, error) {
				called <- trace.SpanContextFromContext(req.Context())
				return f.response(req, t), nil
			})
			native, err := NewNative(&http.Client{}, staticAnnouncement{f.announcement}, 65536, "/api/v1/auth/native-login/", func() time.Time { return f.now })
			if err != nil {
				t.Fatal(err)
			}
			login, err := NewLoginExecutor(f.store, native, LoginConfig{MaxPending: 1, MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return f.now }})
			if err != nil {
				t.Fatal(err)
			}
			worker, err := NewEnrollmentWorker(login, registration, tracer, meter, logger)
			if err != nil {
				t.Fatal(err)
			}
			if !recovered {
				if err := worker.Wake(wake, f.p.ID, loginAttempt); err != nil {
					t.Fatal(err)
				}
			}
			parent.End() // The request is already over before its asynchronous job.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- worker.Run(ctx) }()
			var jobID trace.SpanContext
			select {
			case jobID = <-called:
			case <-time.After(time.Second):
				t.Fatal("job did not call remote")
			}
			deadline := time.Now().Add(time.Second)
			for len(recorder.Ended()) < 2 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("worker did not stop")
			}
			var job sdktrace.ReadOnlySpan
			for _, span := range recorder.Ended() {
				if span.Name() == "client.enrollment.job" {
					job = span
				}
			}
			if job == nil || job.Parent().IsValid() || job.SpanContext().TraceID() == parentID.TraceID() || job.SpanContext().TraceID() != jobID.TraceID() {
				t.Fatal("job incorrectly continued request or lost child context")
			}
			if recovered {
				if len(job.Links()) != 0 {
					t.Fatal("restart invented original request trace")
				}
			} else if len(job.Links()) != 1 || job.Links()[0].SpanContext.SpanID() != parentID.SpanID() {
				t.Fatal("explicit wake cause lost during startup scan")
			}
			snapshot, err := f.store.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte(jobID.TraceID().String())) || bytes.Contains(raw, []byte(parentID.TraceID().String())) {
				t.Fatal("telemetry entered protected enrollment state")
			}
			credential := snapshot.Profiles[f.p.ID].Secrets.NodeCredential
			if !strings.Contains(logs.String(), jobID.TraceID().String()) || strings.Contains(logs.String(), f.p.ID) || strings.Contains(logs.String(), loginAttempt) || (credential != "" && strings.Contains(logs.String(), credential)) {
				t.Fatal("job correlation missing or protected input logged")
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &data); err != nil {
				t.Fatal(err)
			}
			for _, scope := range data.ScopeMetrics {
				for _, instrument := range scope.Metrics {
					switch value := instrument.Data.(type) {
					case metricdata.Gauge[int64]:
						if len(value.DataPoints) != 1 || value.DataPoints[0].Value != 0 || value.DataPoints[0].Attributes.Len() != 0 {
							t.Fatal("queue depth not drained or unbounded")
						}
					case metricdata.Sum[int64]:
						for _, point := range value.DataPoints {
							if point.Attributes.Len() > 2 {
								t.Fatal("unbounded job labels")
							}
						}
					}
				}
			}
		})
	}
}
