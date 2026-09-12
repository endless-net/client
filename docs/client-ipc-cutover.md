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
| Runtime | All specified v0 capabilities backed by real providers and durable state | pending |
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
- Production runtime, Dart local transport and consumer migration are still
  pending. These foundation tests do not close system acceptance scenarios.

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

The corrected descriptor and transport pipeline passed all jobs on
[client commit b3929b7](https://github.com/endless-net/client/actions/runs/34720790036).
That run includes Windows/Linux/macOS local transport, Buf baseline checks,
generated drift and Dart analysis; it predates the durable mutation foundation.
