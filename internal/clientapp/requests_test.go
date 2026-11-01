package clientapp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/endless-net/client/internal/localipc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func requestSignals(t *testing.T) (*Signals, *tracetest.SpanRecorder, *bytes.Buffer, *sdkmetric.ManualReader) {
	t.Helper()
	logs := &bytes.Buffer{}
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
	s, err := NewSignals(slog.New(slog.NewJSONHandler(logs, nil)), tp.Tracer("client"), mp.Meter("client"))
	if err != nil {
		t.Fatal(err)
	}
	return s, recorder, logs, reader
}

func TestHTTPPropagationWaitsForBodyAndPreservesOriginalRequest(t *testing.T) {
	s, recorder, logs, reader := requestSignals(t)
	ctx, parent := s.tracer.Start(context.Background(), "parent")
	transport, err := s.HTTPTransport(roundTrip(func(r *http.Request) (*http.Response, error) {
		remote := (propagation.TraceContext{}).Extract(context.Background(), propagation.HeaderCarrier(r.Header))
		sc := trace.SpanContextFromContext(remote)
		if !sc.IsRemote() || sc.TraceID() != parent.SpanContext().TraceID() {
			t.Fatal("HTTP trace not propagated")
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-secret" || r.Header.Get("Baggage") != "" {
			t.Fatal("credential altered or baggage propagated")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != "private-payload" {
			t.Fatal("request bytes changed", err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("private-response"))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://control.invalid/nodes/register?secret-query=private", strings.NewReader("private-payload"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer synthetic-secret")
	request.Header.Set("Traceparent", "forged-parent")
	request.Header.Set("Baggage", "private-baggage")
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.Ended()) != 0 {
		t.Fatal("span ended before body read")
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if request.Header.Get("Traceparent") != "forged-parent" || request.Header.Get("Baggage") != "private-baggage" {
		t.Fatal("original headers mutated")
	}
	parent.End()
	spans := recorder.Ended()
	if len(spans) != 2 || spans[0].Parent().SpanID() != parent.SpanContext().SpanID() || spans[0].SpanKind() != trace.SpanKindClient {
		t.Fatal("HTTP span/count correlation lost")
	}
	for _, secret := range []string{"synthetic-secret", "secret-query", "private-payload", "private-response", "private-baggage", "control.invalid"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("HTTP signal disclosed", secret)
		}
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	for _, metric := range data.ScopeMetrics[0].Metrics {
		if sum, ok := metric.Data.(metricdata.Sum[int64]); ok {
			if len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 || sum.DataPoints[0].Attributes.Len() != 4 {
				t.Fatal("response counted more than once or unsafe labels")
			}
		}
	}
}

type failedBody struct{}

func (failedBody) Read([]byte) (int, error) { return 0, errors.New("private-body-failure") }
func (failedBody) Close() error             { return errors.New("private-close-failure") }

func TestHTTPBodyFailureAndUnknownURLUseBoundedSignals(t *testing.T) {
	s, recorder, logs, _ := requestSignals(t)
	transport, err := s.HTTPTransport(roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: failedBody{}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://control.invalid/private-unknown-path", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if len(recorder.Ended()) != 1 {
		t.Fatal("body failure double counted")
	}
	if strings.Contains(logs.String(), "private-") || !strings.Contains(logs.String(), "unsupported") || !strings.Contains(logs.String(), "network_error") {
		t.Fatal("unbounded or missing body failure signals")
	}
	unsafe := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	if _, err := s.HTTPTransport(unsafe); err != ErrSignals {
		t.Fatal("unsafe TLS transport hidden by instrumentation")
	}
	certificate := &http.Transport{TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{{}}}}
	if _, err := s.HTTPTransport(certificate); err != ErrSignals {
		t.Fatal("workload certificate hidden by instrumentation")
	}
}

func TestIPCObservationRecordsAuthorizationRefusalWithoutTrustingMetadata(t *testing.T) {
	s, recorder, logs, _ := requestSignals(t)
	ctx, parent := s.tracer.Start(context.Background(), "caller")
	carrier := propagation.MapCarrier{}
	(propagation.TraceContext{}).Inject(ctx, carrier)
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("traceparent", carrier["traceparent"], "x-caller-sid", "S-1-5-18", "baggage", "private-baggage"))
	info := &grpc.UnaryServerInfo{FullMethod: "/client.v0.ClientService/GetRuntimeInfo"}
	_, err := s.Unary(ctx, "private-request", info, func(ctx context.Context, request any) (any, error) {
		return localipc.Unary(func(context.Context) (string, error) { return "", nil })(ctx, request, info, func(context.Context, any) (any, error) { t.Fatal("forged caller reached handler"); return nil, nil })
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	parent.End()
	spans := recorder.Ended()
	if len(spans) != 2 || spans[0].Parent().SpanID() != parent.SpanContext().SpanID() || spans[0].SpanKind() != trace.SpanKindServer {
		t.Fatal("IPC correlation lost")
	}
	for _, secret := range []string{"private-request", "S-1-5-18", "private-baggage"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("IPC signal disclosed", secret)
		}
	}
	if !strings.Contains(logs.String(), "denied") || !strings.Contains(logs.String(), "Unauthenticated") {
		t.Fatal("authorization refusal not observed")
	}
	if got := ipcOperation("/private-unknown-method"); got != "unsupported" {
		t.Fatal(got)
	}
}

type fixtureStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fixtureStream) Context() context.Context { return s.ctx }

func TestIPCStreamObservationKeepsAuthorizationAndRejectsMalformedTraceParent(t *testing.T) {
	s, recorder, logs, _ := requestSignals(t)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("traceparent", "private-invalid-trace", "x-caller-sid", "S-1-5-18"))
	stream := &fixtureStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/client.v0.ClientService/WatchEvents", IsServerStream: true}
	err := s.Stream(nil, stream, info, func(server any, stream grpc.ServerStream) error {
		return localipc.Stream(func(context.Context) (string, error) { return "", nil })(server, stream, info, func(any, grpc.ServerStream) error { t.Fatal("stream metadata forged caller"); return nil })
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Parent().IsValid() {
		t.Fatal("malformed traceparent trusted")
	}
	if strings.Contains(logs.String(), "private-invalid-trace") || strings.Contains(logs.String(), "S-1-5-18") || !strings.Contains(logs.String(), "WatchEvents") {
		t.Fatal("stream signals unbounded or missing")
	}
}
