module github.com/endless-net/client

go 1.27.1

require (
	connectrpc.com/connect v1.19.1
	github.com/Microsoft/go-winio v0.6.3
	github.com/caarlos0/env/v11 v11.4.1
	github.com/endless-net/billing/api v0.5.0
	github.com/endless-net/client/api v0.2.0
	github.com/endless-net/coordinator/api v0.4.0
	github.com/endless-net/identity/api v0.2.0
	github.com/endless-net/signing/api v0.1.0
	go.opentelemetry.io/otel v1.47.0
	go.opentelemetry.io/otel/metric v1.47.0
	go.opentelemetry.io/otel/sdk v1.47.0
	go.opentelemetry.io/otel/sdk/metric v1.47.0
	go.opentelemetry.io/otel/trace v1.47.0
	go.uber.org/fx v1.24.0
	golang.org/x/net v0.62.0
	golang.org/x/sys v0.49.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/endless-net/relay/api v0.3.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.uber.org/dig v1.19.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.26.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)
