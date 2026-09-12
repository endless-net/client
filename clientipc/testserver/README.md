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

## Evidence and remaining layers

The tests exercise all unary dispatches over gRPC, ordered streaming followed by
a typed failure, immutable fixtures, request mismatch privacy, cancellation and
missing authorization. The existing Protobuf workflow executes this package on
GitHub Ubuntu, Windows and macOS runners as part of `clientipc` short tests.

This is the scripted transport fixture foundation. It does not yet provide a
standalone cross-process scenario host, reusable BA/SA scenario catalog, simulated
durable state engine, Dart integration, Android emulator or iOS simulator jobs.
Those layers are required before claiming planned UI coverage. Scripted replies
cannot prove runtime idempotency, policy enforcement, OS authorization, routing,
installer behavior or renewal continuity; those require separate real-provider
and platform acceptance against pinned artifacts.
