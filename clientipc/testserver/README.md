# Client v0 testserver

Producer-owned Go fixture implementing all 38 generated ClientService methods.
Use it to drive consumers through deterministic protobuf requests, snapshots,
operation updates, domain invalidations and typed failures. It has no runtime
state-file access and no control-plane credentials.

## Scripted RPC fixture

Create a `Server` with `New`, enqueue `Step` values with `Expect`, and serve
`Handler(authorize)` through the local transport package or an isolated test
HTTP/2 server. A desktop consumer fixture should use `clientipc/local.Listen`
and `local.NewServer` with a unique test endpoint. The authorization callback
must derive the caller from `local.PeerFromContext`; never accept a claimed
identity in request metadata. The fixture exposes no RPC for changing scripts.

Each step matches an exact protobuf request and consumes one per-method FIFO
entry. Unary steps provide one typed response or an error. WatchEvents steps
provide an ordered list of typed events followed by an optional error. Enqueue
another WatchEvents step to model reconnection with a fresh snapshot. A Release
channel delays a step without wall-clock sleeps, allowing deterministic late
responses and cancellation tests. Scripts clone all messages when enqueued.

Call `Verify` after the scenario and after all RPCs finish. Unexpected calls,
request mismatches, unconsumed steps and in-flight calls fail verification.
Errors never print request contents. Fixtures should contain synthetic data
only. Error values supplied by a test remain the test author's responsibility.

The fixture deliberately permits semantically malformed response fields so tests
can exercise defensive consumers. Request/response message types are checked
against the producer descriptor. Undeclared calls return typed UNSUPPORTED;
there is no default successful mutation.

## Standalone scenario host

Build `./cmd/client-testserver` from the `clientipc` module. Start it with
`--script scenario.json --endpoint <unique-test-pipe-or-socket>` and optionally
`--access observer|owner|administrator` (default: observer). Production default
endpoints are rejected. The host uses the producer's authenticated local
transport; access is a fixture-configured simulated role, not proof of runtime
ownership enforcement. Never pass real credentials to fixtures.

Example synthetic scenario:

```json
{"steps":[
  {"method":"GetStatus","request":{},"responses":[
    {"status":{"connectionPhase":"CONNECTION_PHASE_CONNECTING"}}
  ]},
  {"method":"GetStatus","request":{},"failure":{
    "rpc_code":"unavailable",
    "detail":{"code":"ERROR_CODE_UNAVAILABLE","retryable":true}
  }}
]}
```

Message bodies use protobuf JSON. Unknown fields and invalid message types are
rejected without echoing script contents. Scripts are limited to 8 MiB and 4096
steps. Streaming responses are emitted in array order. JSON scripts do not
support the in-process `Release` synchronization primitive.

The parent keeps stdin open and waits for the stdout JSON `ready` event, which
includes `contract_sha256`, before connecting. After completing its assertions
and closing consumer channels, it writes `verify` followed by a newline to stdin.
Only a `verified` event **and** exit code zero indicate successful script
consumption. Unconsumed steps, unexpected requests, active calls, EOF or parent
loss fail the run. The parent should impose a scenario timeout and terminate the
process on test failure. There is no network scenario-control endpoint.

## Evidence and remaining layers

The tests exercise all unary dispatches over gRPC, ordered streaming followed by
a typed failure, immutable fixtures, request mismatch privacy, cancellation and
missing authorization. The existing Protobuf workflow executes this package on
GitHub Ubuntu, Windows and macOS runners as part of `clientipc` short tests.

The standalone host is tested through the real local pipe/socket with readiness,
role rejection and explicit verification. This is the scripted transport fixture
foundation. It does not yet provide a reusable BA/SA scenario catalog, simulated
durable state engine, Dart integration, Android emulator or iOS simulator jobs.
Those layers are required before claiming planned UI coverage. Scripted replies
cannot prove runtime idempotency, policy enforcement, OS authorization, routing,
installer behavior or renewal continuity; those require separate real-provider
and platform acceptance against pinned artifacts.
