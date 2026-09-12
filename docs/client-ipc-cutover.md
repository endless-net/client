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
SelectProfile handler/lifecycle wiring and other runtime providers remain
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
domain invalidations, capability providers, global operation bounds/Disconnect
coalescing and production listener migration remain incomplete.

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

The native SelectProfile RPC/lifecycle worker, initial-profile adoption for
existing installations, complete policy restrictions, Disconnect race handling,
other profile-scoped invalidations and multi-platform real tunnel verification
still need implementation/validation in this repository before UF-16 acceptance.
The production HTTP v2 listener remains until the full hard cutover is ready;
the new implementation does not delegate to it or provide a fallback.
