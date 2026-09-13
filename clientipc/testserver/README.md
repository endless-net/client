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
`ResponseRelease` optionally supplies one gate for each streaming response;
nil gates send immediately, while other gates wait for release or cancellation.
This supports an initial snapshot followed by a controlled later status event
without blocking unrelated unary calls. The gate slice is copied on enqueue.
Unary methods and mismatched gate counts are rejected.

Call `Verify` after the scenario and after all RPCs finish. `WaitIdle(ctx)` can
synchronize handler completion after consumer channels close; it does not
cancel handlers or replace Verify. The standalone host waits at most five
seconds after the explicit `verify` command before failing on unfinished calls.
Unexpected calls,
request mismatches, unconsumed steps and in-flight calls fail verification.
An admitted streaming call counts as consumed even when the consumer cancels
before all responses are sent. This permits intentional cancellation scenarios;
`Verify` alone is not evidence that every event was delivered or applied.
Success-path tests must independently assert each required consumer observation.
The per-response gate test separately checks that cancellation reports CANCELED
and never delivers the unreleased response; the UI Network scenario separately
awaits the new Network/revision after release before verifying the script.
Errors never print request contents. Fixtures should contain synthetic data
only. Error values supplied by a test remain the test author's responsibility.

The fixture deliberately permits semantically malformed response fields so tests
can exercise defensive consumers. Request/response message types are checked
against the producer descriptor. Undeclared calls return typed UNSUPPORTED;
there is no default successful mutation.

### Event and access-transition scenarios

For a valid subscription, script exactly one opening `SnapshotEvent` at sequence
1, then `status_changed`, `operation_changed`, or domain invalidation events with
increasing sequence numbers and nondecreasing metadata revisions. A repeated
snapshot belongs in a negative consumer test, not a successful update scenario.

The actual runtime terminates an observer subscription after an ownership claim
with typed `STALE_STATE`; owner revocation terminates it with `OWNER_REQUIRED`.
Model these as the first WatchEvents step's terminal error, then enqueue a second
WatchEvents step beginning at sequence 1 with the new role's snapshot. The consumer
must explicitly establish a fresh subscription; do not model a role change by
injecting a second snapshot into the old stream. Assert that revoked private data
is cleared and that the fresh snapshot is applied before verifying the script.

These scripts test consumer handling of simulated authorization transitions.
Actual OS identity, runtime ownership enforcement and installed-platform behavior
remain separate acceptance evidence; the fixture does not implement that policy.

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
steps. Streaming responses are emitted in array order. The optional
`response_gates` array has exactly one string per streaming response: an empty
string sends immediately; a unique name waits for a parent signal. Names contain
1–64 lowercase ASCII letters, digits, underscores or hyphens; a script may define
at most 4096 gates. Unary gates and duplicate names are rejected before serving.
For example, `"response_gates":["","network-changed"]` sends the first event
immediately and holds the second until stdin receives `release network-changed`.
The parent waits for the JSON `released` acknowledgement, then awaits/asserts the
consumer's observed event. Acknowledgement proves release, not consumer delivery.
Unknown or repeated releases fail the run without echoing the supplied name.
The whole-step in-process `Release` channel remains unavailable in JSON.

For WatchEvents, `"hold_open": true` sends the declared responses and then waits
for consumer cancellation. Other RPCs can run while that subscription remains
active. This supports a live snapshot → mutation scenario without artificial
EOF or sleeps. It cannot be combined with a terminal scripted RPC failure or
used for unary methods. Close/cancel the stream before asking the host to Verify;
an active held stream must fail verification.

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
