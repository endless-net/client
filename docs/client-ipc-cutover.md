# Client IPC v0 hard cutover

- Status: in progress.
- Owner: client, with client-ui and system-tests.
- Authorized: 2026-09-13. Replace HTTP IPC v2 completely; no fallback, dual
  production protocol or preserved obsolete tests. Minor v0 schema corrections
  are allowed; version increases and infrastructure changes are not authorized.

## Completion gates

| Gate | Required result | Current state |
| --- | --- | --- |
| Transport | Authenticated local gRPC on Windows pipe and Unix sockets; Go/Dart interoperability | in progress |
| Runtime | All specified v0 capabilities backed by real providers and durable state | in progress |
| Consumers | CLI, helper, Flutter UI and emulator use generated v0 API | pending |
| Retirement | Delete old schema, IPC implementation, routes, DTOs and obsolete tests | pending |
| Distribution | New descriptor/digest and generated SDK pins in core/UI pairing | pending |
| Coverage | US-01–14, UI-AC-01–27 and relevant headless IT implemented with negative cases | pending |
| Acceptance | Pinned artifacts, supported platform/provider tests, explicit limits | pending |

Schema/SDK compilation is not runtime or product acceptance. Existing UI/OS
tests remain evidence of the currently served protocol until migrated; passing
them does not close these gates. No real deployment or signed release is implied.

The accepted BA and SA live in architecture and client-ui; do not duplicate the
requirements here. Update this ledger with actual test paths/results as the
cutover progresses. Do not use unavailable placeholders or fake success to
claim a specified capability is implemented.

## Transport foundation evidence (2026-09-13)

- `clientipc/local/local_test.go`: real local gRPC unary/streaming and bootstrap
  pairing rejection, OS peer identity required, remote/relative endpoint rejection.
- `clientipc/rpc/protocol_test.go`: all RPC access annotations enforced, unknown
  procedure rejection, absent/wrong/duplicate metadata, authorization before
  negotiation, typed failures across gRPC unary and streaming boundaries.
- Local Windows `go test -short ./...` and `go vet ./...` passed in `clientipc`.
  Linux/macOS execution is assigned to the three-platform contract CI matrix;
  adding the matrix is not evidence of a successful run.
- Go/Dart local transport subsequently passed on Windows/Linux/macOS in
  [client-ui 665beee](https://github.com/endless-net/client-ui/actions/runs/34721678102).
  Its pinned producer is dc560f8. This is bootstrap/unary/streaming/error and
  shutdown evidence using synthetic scripts, not full system acceptance.
  Production listener and complete consumer migration are still pending.

## Durable mutation foundation (2026-09-13)

`internal/client/service_rpc.go` uses the existing protected atomic ConfigStore
for v0 ownership claim, request deduplication and operation lifecycle. Preparation
stores intent together with acceptance; runtime providers must reconcile external
effects after that transaction. It is not yet wired into production handlers.
No state format/version increase or HTTP compatibility adapter was introduced.

`internal/client/service_rpc_test.go` covers concurrent claim with one winner,
rollback of rejected preparation, authenticated access and ownerless enrollment,
durable replay across a new runtime instance before CAS, payload/RPC conflicts,
keyed enrollment request digest, caller-bound lookup, operation transition rules,
terminal immutability, nonterminal retention and 24-hour completed retention.
These are storage/domain unit tests, not full runtime, provider or UI acceptance.

`internal/client/service_rpc_transport_test.go` additionally exercises the actual
local gRPC guard and ConfigStore acceptance: OS identity becomes the durable
owner, reconnect recovers a request by its original ID, an identical retry does
not prepare again, and a conflicting payload returns typed INVALID_ARGUMENT.
It now uses the native v0 ClientRPCService and real profile creation, not a
preparation fixture or the HTTP v2 handler.

The corrected descriptor and transport pipeline passed all jobs on
[client commit b3929b7](https://github.com/endless-net/client/actions/runs/34720790036).
That run includes Windows/Linux/macOS local transport, Buf baseline checks,
generated drift and Dart analysis; it predates the durable mutation foundation.

## Local profile handlers (2026-09-13)

`service_rpc_handlers.go` exposes native GetRuntimeInfo, GetOperation and
List/Create/Rename/RemoveProfile handlers with strict guard, message bounds and typed
error sanitization. The production listener has not switched to this service;
remaining methods are unimplemented and no complete capability is advertised.

Local-only profile changes and SUCCEEDED outcomes share one config transaction.
Create makes an empty inactive context with immutable canonical HTTPS origin;
rename cannot change origin/identity; remove rejects active, enrolled or busy
profiles and retains installation ownership. The producer bounds profile count
at 128 and display names at 128 UTF-8 bytes without control characters.
`service_rpc_profiles_test.go` verifies these invariants, durable terminal
records and idempotent replay. `service_rpc_pages_test.go` covers caller/query/
instance/revision/size-bound signed pagination, five-minute expiry, stable
ordering, stale snapshot rejection and no private config disclosure.
Production SelectProfile lifecycle wiring and other runtime providers remain
required work; this is not UF-16 acceptance yet. The handover foundation and
its remaining gates are recorded below.

## Native status/event publication (2026-09-13)

GetStatus and WatchEvents now share an atomic snapshot with durable mutation
publication under the runtime mutation lock. A new subscription queues its first
snapshot before registration completes. Local mutations publish refreshed
snapshots, caller-owned operation outcomes and profile invalidations in order.
Observers use an allowlisted projection, not a copy with selected secrets removed.
An absent provider observation remains unknown, never a fabricated connection.

`service_rpc_events_test.go` covers first/reconnect snapshots, ownership-claim
refresh, operation kind, observer redaction, clone isolation, unchanged status,
64-event/8-MiB overflow, cancellation and unsubscribe. The real local transport
test consumes native snapshots, terminal operations and profile invalidations.
Queued overflow returns LIMIT_EXCEEDED; a blocked transport write is aborted so
it cannot keep the subscriber alive indefinitely. A transport that can no longer
write trailers may expose reset/deadline instead of typed details; consumers must
discard stale state and reconnect in either case.

The agent still needs to publish its verified map, tunnel, connection phase,
session/credential and control-probe observations through PublishStatus. Other
domain invalidations, capability providers and production
listener migration remain incomplete. Global operation bounds are covered below.

## Agent observation projection (2026-09-13)

`cmd/endlessnet-client/service_rpc_status.go` builds native v0 status directly
from runtime config, verified cached maps and identity/revision-matched agent
snapshots. It does not convert HTTP v2 responses. Readiness uses a bounded,
credential-free probe without redirects; cached-map validity is not treated as
proof of a live control connection. Connection phase is an explicit coordinator
input, never inferred from registration or connected intent. Session and
credential deadlines stay absent until authoritative providers supply them.

`ObserveStatus` collects outside the mutation lock and verifies both the RPC
revision and the complete config fingerprint inside the durable write before
publication. Stale observations, including concurrent non-RPC config changes,
are rejected even when the resulting status would otherwise compare equal.
New CLI/provider tests cover unknown connectivity/deadlines, invalid cache,
foreign/future agent snapshots, safe readiness probes and native publication.
The agent loop still needs wiring to this provider with actual phase transitions;
that and the production listener cutover are not established by these tests.

## Durable profile handover foundation (2026-09-13)

`service_rpc_switch.go` persists the selection operation before side effects,
serializes handover using the same lock as automatic agent reconciliation, and
stops the old tunnel before committing the target context. Installation keys
and ownership remain installation-scoped. Old observations are discarded on
activation; profile-list invalidations accompany switch progress. A failed
target apply is stopped again and disables automatic reconnect. If cleanup
fails, continuity remains unknown; this does not claim routes were removed.

Short tests cover stop/apply ordering, pending replay, BUSY, selecting the same
profile without disruption, failure outcomes, and process-loss recovery before
and after durable activation. Recovery preserves recorded interruption and does
not infer uninterrupted service from a tunnel absent after restart. Agent-driver
tests verify the shared lock, missing provider/enrollment rejection and typed
failure sanitization. These are deterministic local tests, not OS route or
release acceptance evidence.

Production wiring of the native SelectProfile worker, initial-profile adoption for
existing installations, complete policy restrictions, Disconnect race handling,
other profile-scoped invalidations and multi-platform real tunnel verification
still need implementation/validation in this repository before UF-16 acceptance.
The production HTTP v2 listener remains until the full hard cutover is ready;
the new implementation does not delegate to it or provide a fallback.

`service_rpc_worker.go` now binds SelectProfile to a service-owned executor.
Startup scans the durable plan without requiring request replay; shutdown leaves
unfinished work resumable instead of turning lifecycle cancellation into a
business failure. The host must start the worker before serving, stop serving on
unexpected worker error, cancel its lifecycle context and await completion.
Without a live worker SelectProfile returns UNAVAILABLE. ListProfiles projects
worker availability and current BUSY restrictions from the native state.

The real local-transport short test exercises SelectProfile through the generated
client and observes its terminal event. Worker tests cover startup recovery,
duplicate-worker rejection and cancellation/resumption. These checks do not yet
prove production agent lifecycle integration, Disconnect preemption or actual
multi-platform route cleanup.

## Nonterminal operation bound (2026-09-13)

Durable acceptance enforces the installation-wide bound of 32 nonterminal
operations before domain preparation or ownership changes, reserving one slot
for Disconnect (31 for other commands). Retained terminal
records do not occupy an active slot. Idempotent replay is resolved before the
capacity check, preserving recovery at capacity; conflicting request payloads
still fail INVALID_ARGUMENT. Snapshot projection uses the same bound.

`service_rpc_capacity_test.go` fills the ordinary admission slots with 31 pending operations,
checks rejection without preparation/revision changes, exercises replay/conflict
at capacity and verifies a terminal transition releases a slot without deleting
its retained outcome. These are journal-level tests, not Disconnect acceptance.
The reserved Disconnect path is exercised separately as described below.

## Native Disconnect execution (2026-09-13)

Disconnect persists disconnected intent and a durable operation before Down,
retains registration, and uses the service executor's shared tunnel lock. Pending
profile selection also receives disconnected target intent so it cannot restore
the saved connected preference. The executor processes Disconnect before and
after a profile handover. A Down error produces a typed failure with unknown
continuity, never successful cleanup. Lifecycle cancellation leaves the journal
plan resumable; request cancellation does not cancel accepted execution.

The native handler serializes distinct Disconnect requests through terminal
completion to use one reserved slot without aliasing request IDs. Ordinary
commands have 31 active slots; Disconnect can use slot 32 and is exempt from the
normal retained-record admission cap. Terminal retention is unchanged.

`service_rpc_disconnect_test.go` covers acceptance at capacity, replay, retained
registration, Disconnect during blocked profile apply, Down failure and lifecycle
cancellation. The real local-transport test invokes Disconnect with the generated
client and checks its native terminal outcome. Production wiring, provider
timeouts/preemption, restart with real OS state and full multi-platform route
acceptance remain required; these local checks do not establish release readiness.

Disconnect recovery treats a currently absent tunnel as UNKNOWN continuity when
resuming RUNNING: the previous process may already have interrupted connectivity
before persisting its outcome. The cancellation/restart test explicitly returns
NOT_APPLICABLE from the replacement driver and verifies it cannot erase this
uncertainty.

At inspection on 2026-09-13, the [Test run for a4b5319](https://github.com/endless-net/client/actions/runs/34724571973)
was pending and its [Protobuf contract run](https://github.com/endless-net/client/actions/runs/34724571995)
was queued. Local green checks are not evidence that these cross-platform CI
gates passed; no runner or infrastructure changes were made.
