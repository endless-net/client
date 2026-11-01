package clientapp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func ipcOperation(method string) string {
	const prefix = "/client.v0.ClientService/"
	if !strings.HasPrefix(method, prefix) {
		return "unsupported"
	}
	name := strings.TrimPrefix(method, prefix)
	if clientv0.File_client_v0_service_proto.Services().ByName("ClientService").Methods().ByName(protoreflect.Name(name)) == nil {
		return "unsupported"
	}
	return name // Exact finite catalog from the pinned producer descriptor.
}

func rpcTraceContext(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	carrier := propagation.MapCarrier{}
	for _, key := range []string{"traceparent", "tracestate"} {
		values := md.Get(key)
		if len(values) == 1 && len(values[0]) <= 512 {
			carrier[key] = values[0]
		}
	}
	// W3C validates the trace syntax. Baggage is deliberately not extracted;
	// neither a trace ID nor metadata is a caller/owner authorization source.
	return (propagation.TraceContext{}).Extract(ctx, carrier)
}

func rpcOutcome(err error) (string, string) {
	code := status.Code(err)
	if errors.Is(err, context.Canceled) {
		code = grpccodes.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = grpccodes.DeadlineExceeded
	}
	if code > grpccodes.Unauthenticated {
		code = grpccodes.Unknown
	}
	outcome := "failure"
	switch code {
	case grpccodes.OK:
		outcome = "success"
	case grpccodes.Canceled:
		outcome = "cancelled"
	case grpccodes.DeadlineExceeded:
		outcome = "deadline"
	case grpccodes.PermissionDenied, grpccodes.Unauthenticated:
		outcome = "denied"
	case grpccodes.ResourceExhausted:
		outcome = "limited"
	}
	return code.String(), outcome
}

func (s *Signals) Unary(ctx context.Context, request any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
	if !s.valid() || info == nil || next == nil {
		return nil, ErrSignals
	}
	ctx, finish := s.request(rpcTraceContext(ctx), "ipc", ipcOperation(info.FullMethod), trace.SpanKindServer)
	response, err := next(ctx, request)
	code, outcome := rpcOutcome(err)
	finish(code, outcome)
	return response, err
}

type requestStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *requestStream) Context() context.Context { return s.ctx }

func (s *Signals) Stream(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
	if !s.valid() || stream == nil || info == nil || next == nil {
		return ErrSignals
	}
	ctx, finish := s.request(rpcTraceContext(stream.Context()), "ipc", ipcOperation(info.FullMethod), trace.SpanKindServer)
	err := next(server, &requestStream{ServerStream: stream, ctx: ctx})
	code, outcome := rpcOutcome(err)
	finish(code, outcome)
	return err
}

// Call sites supply fixed transport/catalog values; raw method, URL, status
// messages, request/response bodies, metadata and credentials never enter here.
func (s *Signals) request(ctx context.Context, transport, operation string, kind trace.SpanKind) (context.Context, func(string, string)) {
	attributes := []attribute.KeyValue{attribute.String("transport", transport), attribute.String("operation", operation)}
	ctx, span := s.tracer.Start(ctx, "client.request", trace.WithSpanKind(kind), trace.WithAttributes(attributes...))
	started := time.Now()
	return ctx, func(code, outcome string) {
		defer span.End()
		labels := append(attributes, attribute.String("outcome", outcome), attribute.String("code", code))
		span.SetAttributes(attribute.String("outcome", outcome), attribute.String("code", code))
		level := slog.LevelInfo
		if outcome != "success" && outcome != "cancelled" {
			span.SetStatus(codes.Error, "client.request.failed")
			level = slog.LevelWarn
		}
		s.requests.Add(ctx, 1, metric.WithAttributes(labels...))
		s.requestDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(labels...))
		fields := []any{"transport", transport, "operation", operation, "outcome", outcome, "code", code}
		if sc := span.SpanContext(); sc.IsValid() {
			fields = append(fields, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
		}
		s.logger.Log(ctx, level, "client.request", fields...)
	}
}
