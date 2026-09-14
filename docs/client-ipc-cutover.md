# Client IPC v0 hard cutover

- Status: in progress.
- Owner of this implementation task: client. Other repositories are external dependencies.
- Authorized: 2026-09-13. Replace HTTP IPC v2 completely; no fallback, dual
  production protocol or preserved obsolete tests. Minor v0 schema corrections
  are allowed; version increases and infrastructure changes are not authorized.

Release exception authorized on 2026-09-14: the user explicitly requested
`v0.6.0`, GitHub Release and publication to `endless-net/apt`. This authorizes
the release pipeline and its exact-source pre-release checks; it does not close
the incomplete BA/SA cutover gates below or authorize other version increases.

The first v0.6.0 pre-release run on `247fbae` passed Linux/Windows/macOS
verification and the separate Protobuf workflow, but failed installation reset
and ephemeral-container scenarios that still expected enrollment to connect
implicitly. Those scenarios now require initial disconnected intent followed by
an explicit native Connect. Installed mutation submission also uses the existing
bounded CAS-admission retry and safe typed failure reporting. The failed
[Test run](https://github.com/endless-net/client/actions/runs/34879714349)
is not release acceptance; the corrected source requires a new complete gate.

The next [Test run](https://github.com/endless-net/client/actions/runs/34880659203)
on `236cfbe` passed the completed installation jobs (Windows 2025 and Linux
amd64/arm64), container lifecycle, all three OS verification jobs and the separate
Protobuf workflow. Its control-plane peer-traffic scenario exposed another
missing explicit Connect. A source review found the same setup omission in the
initial DNS, MTU, resource, service, relay, advertisement, subnet/exit, diagnostics
and common control fixtures. These fixtures now submit Connect explicitly;
their traffic/recovery assertions remain. Accountless SelectNetwork expects
NEEDS_LOGIN and still requires no accepted operation or additional registration.
The cancelled run is partial evidence only, not a successful release gate.

The [third Test run](https://github.com/endless-net/client/actions/runs/34881749817)
on `d656324` also failed platform contracts. Completed Linux reports identify
cached-authority restart intent, browser enrollment/session setup, exit routing
and event-stream failures. Windows additionally reports repeated STALE_STATE
diagnostics and consequent traffic-observation failures. Browser enrollment and
session fixtures now explicitly Connect before testing connected recovery.
The other failures remain unresolved: do not publish this source or treat the
separate successful Protobuf run as a substitute for the release gate. Exit
tests must not restore implicit default routes to conceal the missing native
exit implementation.

## Completion gates

The current task is limited to this repository and its producer contracts.
Implement the required runtime behavior and unit tests first, and audit the
requirement-to-implementation-to-unit-test matrix before beginning integration,
system or platform acceptance. Existing CI evidence below is historical evidence,
not permission to skip this implementation phase. External dependencies remain
open requirements, not accepted unsupported behavior. See the
[runtime implementation gaps](client-runtime-implementation-gaps.md) for the
initial source audit; that audit is not yet a complete BA/SA matrix.

| Gate | Required result | Current state |
| --- | --- | --- |
| Transport | Authenticated local gRPC on Windows pipe and Unix sockets; Go/Dart interoperability | in progress |
| Runtime | All specified v0 capabilities backed by real providers and durable state | in progress |
| Consumers | Client-owned CLI, helper and test consumers use generated v0 API | source migration observed; complete behavior audit pending |
| Retirement | Delete old schema, IPC implementation, routes, DTOs and obsolete tests | inspected agent/CLI/helper use v0; exhaustive retirement audit pending |
| Distribution | Client-owned descriptor/digest, SDKs and core compatibility metadata | pending; consuming UI distribution is an external dependency |
| Coverage | Client producer obligations from US-01–14 and relevant headless BA/SA, including negative cases | incomplete; UI rendering/interaction acceptance is external |
| Acceptance | Pinned artifacts, supported platform/provider tests, explicit limits | pending |

Schema/SDK compilation is not runtime or product acceptance. Existing UI/OS
tests remain evidence of the currently served protocol until migrated; passing
them does not close these gates. No real deployment or signed release is implied.

The accepted BA and SA live in architecture and client-ui; do not duplicate the
requirements here. Update this ledger with actual test paths/results as the
cutover progresses. Do not use unavailable placeholders or fake success to
claim a specified capability is implemented.

## Current source-boundary audit (2026-09-13)

Inspected client source at `4fd4bb1`:

- `cmd/endlessnet-client/service_rpc_host.go`: `startAgentRPC` binds the native
  local listener and `ClientRPCService`; listener errors are returned, not used
  to start an older protocol. Explicitly disabled IPC opens no listener.
- `cmd/endlessnet-client/service_rpc_cli.go`, `service_rpc_cli_mutation.go` and
  `service_rpc_cli_bundle.go`: consumers use `local.NewClient` and bootstrap the
  v0 pairing before their operations.
- `cmd/endlessnet-client-recovery-helper/main.go`: recovery uses the same local
  client/bootstrap boundary, not HTTP v2 DTOs.
- `clientipc/local/local.go`: generated gRPC client over a custom OS-local dialer;
  the `http://endlessnet.local` base URL is not a TCP endpoint or legacy HTTP IPC
  fallback. HTTP/2 here is the gRPC transport, not the retired JSON protocol.
- `clientipc/local/local_windows.go` and `local_unix.go`: local named pipes or
  absolute Unix sockets. Remote Windows pipes and non-absolute Unix endpoints
  are rejected. This inspection is not new platform execution evidence.

Targeted searches in `cmd`, `internal` and `clientipc` found no old IPC v2 route
or listener among the inspected runtime paths. Matches for backend failover,
relay/direct path fallback, and negative tests are not legacy IPC support and
must not be removed merely because they contain the word `fallback`.

This is a bounded source audit, not proof that all obsolete artifacts/tests have
been removed or that all specified methods are implemented. The runtime gap
matrix remains authoritative for missing functionality. Flutter UI migration,
consumer SDK adoption and UI-specific acceptance belong to `client-ui`; they
remain external follow-up work and do not authorize changes there. Earlier
sections below retain their historical state and are not current completion
claims.

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

Production wiring of the native SelectProfile worker, complete policy restrictions, Disconnect preemption,
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

`service_rpc_disconnect_journal_test.go` exercises the normal 4096-record cap
using synthetic terminal records in the real ConfigStore. It checks ordinary
rejection without preparation, Disconnect admission and idempotent lookup above
the cap, BUSY for a distinct request while its predecessor remains active, and
separate durable outcomes after serialized completion. Reopening the store and
advancing an injected clock checks each outcome's independent 24-hour retention
boundary without evicting records to admit Disconnect. The Stop driver is a
test callback: this is component journal evidence, not actual tunnel shutdown,
process-crash durability, local-transport handler scheduling or platform acceptance.

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

## Initial profile state adoption (2026-09-13)

Before starting its native executor, the service assigns existing configured
installation state to one active v0 profile. Registration, credentials, keys,
ownership and connection intent remain unchanged at the config root; adoption
does not reconfigure the tunnel or duplicate installation secrets into profiles.
Repeated startup preserves the same profile ID and revision. Ownerless existing
enrollment stays ownerless and cannot be claimed by the first connecting UI.

Adoption requires a single unambiguous canonical HTTPS control origin. Different
origins, missing origin for enrollment, unsafe URLs, corrupt active-profile
references or conflicting unfinished work fail without changing persisted state.
Equivalent spellings of the same origin are accepted without rewriting the
existing URL configuration. Resolving an ambiguous existing configuration remains
an explicit operator task, not automatic credential reassignment.

`service_rpc_adoption_test.go` verifies preserved config, idempotence, ownerless
access protection and rejection without mutation. This is initial state cutover,
not an HTTP v2 fallback; production listener migration remains outstanding.

## Native Connect execution (2026-09-13)

Connect now persists its operation and connected intent for the active registered
profile before invoking the native driver. Conflicting nonterminal operations
are rejected; replay remains idempotent. The service executor resumes a persisted
Connect at startup and retains unfinished work on lifecycle cancellation.
Failed apply attempts invoke cleanup and disable automatic reconnect while
retaining registration. Failures expose typed diagnostics, not raw driver text.

Disconnect can supersede Connect before or during apply. The terminal transaction
rechecks durable intent and marks the superseded Connect CANCELLED without ever
restoring connected intent. Disconnect's own operation still waits for Down.
Successful configuration apply is not treated as evidence of live connectivity:
phase remains unknown until an authoritative provider observation arrives.

`service_rpc_connect_test.go` covers durable acceptance/replay, success/failure,
partial-apply cleanup, both Disconnect orderings and disk-backed restart recovery.
The real local-transport test exercises Connect rejection without enrollment and
its terminal event with a substituted driver. Its unsigned fixture is not real
map-verification or tunnel evidence. Full policy/phase provider integration,
bounded preemption, production lifecycle and multi-platform system acceptance
remain required before claiming UF-05 release acceptance.

Connect/profile application now registers a cancellable provider context under
the same lock as durable mutation acceptance. Accepted Disconnect cancels that
context only after its intent/journal commit; rejected and replayed commands do
not interrupt current work. This closes the check-intent/start race without
claiming that context cancellation itself removed routes. Cleanup still runs
through Down and its own result handling. `TestRPCDisconnectPreemptsApplyOnlyAfterAcceptance`
checks rejected CAS, cancellation of an active apply, cancelled Connect outcome
and subsequent Disconnect completion. Providers must honor cancellation; real
driver latency and OS cleanup still require system evidence.

## Observer-safe support metadata (2026-09-13)

Native GetSupportInfo returns product/build identity without enrollment or a
control-plane request. The service fills absent platform/architecture from its
actual Go runtime (not a Windows assumption), preserves supplied build version,
commit and date, and clones inputs/outputs to avoid mutable metadata leakage.
The local-transport test calls it before ownership claim; the support test checks
offline operation, build identity and response isolation.

Documentation/support/privacy/license destinations and the offline-help resource
key remain absent until approved product metadata and shipped UI content supply
them. Their absence is not a complete UF-23 implementation: client-ui still owns
localized offline help and presentation, and the client/distribution integration
must provide build metadata and approved destinations before release acceptance.

## Preference validation foundation (2026-09-13)

`service_rpc_preferences.go` validates the complete typed patch before returning
its ordered key set. Explicit false remains present; empty patches, unknown wire
fields, unspecified/unknown lifecycle enums and malformed reset key lists are
rejected without a partial result. Reset requires nonempty unique known keys.
Tests cover all eight preference keys and input immutability.

Structural validation alone does not persist or apply settings. The UI_QUIT
binding is described below. Other per-profile overrides, effective policy projection, provider
support checks, atomic application/rollback and lifecycle adapters remain needed.
In particular, a structurally known PLATFORM_MANAGED enum does not authorize a
user override; the applicable provider's allowed values must still be enforced.
UF-18/UF-21 acceptance is not established by these structural tests.

## Graceful UI quit preferences (2026-09-13)

Get/Set/ResetPreferences now support the per-profile UI_QUIT setting with
KEEP_INTENT (default) and DISCONNECT values. Reset removes the user override;
requested presence and DEFAULT/USER source are distinct. A patch containing other
unsupported keys is rejected in full, without changing UI_QUIT. Other preference
projections remain absent rather than fabricated effective values.

NotifyLifecycle accepts only explicit UI_QUIT for the active profile. KEEP_INTENT
finishes as an atomic no-change operation. DISCONNECT persists intent and a
NOTIFY_LIFECYCLE operation, preempts in-flight apply and completes through the
same Down executor as Disconnect. It keeps its original operation kind throughout.
Neither dropping a connection nor crashing the UI sends this command automatically.
Preference changes publish profile-scoped preference invalidations.

Tests cover default keep, persisted override/replay, disk reload, reset,
mixed-patch rejection, invalid lifecycle events and executed Down. The real local
transport exercises preference reads/writes/reset and NotifyLifecycle. Wiring
graceful UI exit in client-ui, managed setting constraints, the remaining seven
preferences, OS lifecycle adapters and production/system acceptance remain open;
this is not complete UF-18/UF-21 acceptance.

ListManagedSettings now shares the same native UI_QUIT effective projection as
GetPreferences, including DEFAULT/USER source and snapshot revision. Set/reset
invalidates both profile-scoped PREFERENCES and MANAGED_SETTINGS so consumers
refetch both views. Local transport tests check matching values/revisions,
reset-to-default source and observer denial; event tests check both invalidations
at the committed revision. Missing providers still mean the other managed
settings are absent, not proven unlocked or policy-free. Full device/account
policy projection and UF-20 acceptance remain outstanding.

## Profile device-binding correction (2026-09-13)

Inspection of the existing registration flow showed that DeviceFingerprint binds
the installation/public key to its control URL; it is not a profile-independent
installation value. Profile handover now saves/restores each profile's fingerprint
instead of carrying the previous server's fingerprint into the target context.
Installation private keys remain installation-scoped and are not duplicated.

Saved control URLs are retained subject to the existing config normalization and
must resolve to the profile's immutable canonical origin. They are not replaced
with a differently spelled canonical origin that could change a historical device
binding. The round-trip two-origin test verifies both fingerprints, URL bindings
and key scope without reading real installation state. Actual cross-server
registration and OS tunnel acceptance remain required system evidence.

## Typed registration workflow extraction (2026-09-13)

The `up` CLI now parses flags/configuration and calls `enrollConfiguredClient`
with typed options. Registration, identity proof, response binding/signature and
credential verification, durable pending request handling and restricted approval
state remain in that shared workflow; it consumes neither HTTP v2 DTOs nor CLI
argument arrays. CLI reporting is an optional callback at its completion points.

Direct workflow tests exercise verified pending enrollment and reject substituted
node identity before saving enrollment authority. Existing CLI tests remain and
verify the refactored path. Native Enroll is not yet bound: durable RPC workflow
acceptance, context cancellation, browser action projection, per-profile commit
coordination and operation outcomes still need integration. This extraction does
not establish UF-04 acceptance or production cutover.

The typed workflow now requires a caller context. A transport wrapper combines
that lifecycle with each request's original deadline and retains it until the
response body is consumed/closed, not merely until headers arrive. Browser polling
uses cancellable timers and does not translate lifecycle cancellation into a
pending-approval result. Tests cover header/body lifetime, in-flight cancellation,
deadline preservation and cancellation before any local config write. Native RPC
acceptance/recovery integration and browser action projection remain unfinished.

Browser approval notices now leave the shared workflow through an optional typed
callback carrying only request ID and approval URL; console rendering belongs to
the CLI adapter. Newly created requests are persisted before notification, and
callback failure preserves the pending request for recovery. A zero-wait retry
can also report the saved approval action when the status endpoint is unavailable.
Short tests verify durable state before notification and preservation on callback
failure. This does not yet bind the callback to native Enroll operation events;
the accepted v0 schema and generated SDKs are unchanged by this internal extraction.

The shared registration workflow and browser request persistence now require an
explicit config-save callback instead of accepting a config path and overwriting
the file themselves. The CLI supplies file persistence; a native runtime adapter
must supply profile/operation-guarded transactions. Rejected saves stop the
workflow: tests cover installation-key config, pending request (before registration
is sent), and verified result (before success reporting). Installation identity
binding still uses the installation-state subsystem; this change does not claim
that all enrollment effects are already part of one RPC transaction. Native
profile guards, operation recovery and browser event wiring remain to implement.

The native mutation coordinator now provides an enrollment config-save adapter
for a RUNNING ENROLL operation. Each checkpoint commits enrollment fields and
operation revision together. It rejects a changed active profile, control origin,
owner, account, installation identity or registration state, a pending profile
switch, and writes after a terminal outcome. An explicit field allowlist preserves
concurrent Disconnect intent, preferences and the current journal. Tests exercise
successive checkpoints, concurrent intent/settings, stale contexts and late
responses. This adapter is not yet wired to a native Enroll handler/worker;
acceptance, secret-safe restart recovery and browser action events remain open.

Native Enroll admission now validates mode, hostname, explicit token/browser
authentication and the selected empty profile's immutable origin. It rejects
conflicting operations and existing registration/session state. Acceptance stores
a protected runtime plan together with the PENDING operation; only the keyed
request digest and safe operation envelope are observable. Terminal reconciliation
removes the plan atomically. Tests cover disk persistence, exact replay, changed
input rejection, invalid-input rollback and token absence from the operation.
The plan's authorization is protected config data, not a log or public journal
field. Enroll is not exposed by the native service until its executor can consume
and recover this plan; mode-specific runtime policy and end-to-end enrollment
remain outstanding. No proto or state format version is increased.

Config secret classification now includes any RPC state and pending direct
registration, ensuring Unix private-file permission checks also apply before
root enrollment and to credentials stored in inactive profiles/runtime plans.
Windows continues to use the existing protected-state loader; persistence tests
read through that loader rather than treating protected bytes as plaintext JSON.

The enrollment executor now consumes the accepted plan through a typed provider:
RUNNING checkpoints, WAITING_FOR_USER approval actions and successful
EnrollmentResult use the same journal. Lifecycle cancellation/checkpoint failure
retain recoverable work; terminal failures remove the plan and do not expose raw
provider diagnostics. Browser actions require HTTPS without embedded credentials.
The agent provider calls the shared registration workflow directly, supplies the
operation ID as registration idempotency key and projects restricted approval as
WAIT_FOR_APPROVAL rather than a connected/successful registration claim. Tests
cover waiting/resume through a new coordinator, lifecycle cancellation, sanitized
failure, unsafe URLs and verified pending enrollment through the agent adapter.
Native service worker scheduling, production listener wiring, real restart and
same-artifact system acceptance remain outstanding; this is not full UF-03/04.

The native service now exposes Enroll when StartEnrollmentWorker is running.
Admission and wakeup are serialized with worker shutdown; unavailable workers
reject new calls without acceptance. The worker reconciles once on startup and
periodically revisits approval waits. Registration has a separate worker from
tunnel application so network/approval waits do not hold the Disconnect executor.
Tests verify unavailable admission, single-worker enforcement, cancellation with
retained work and startup completion without caller replay. The production agent
still needs to start this worker with agentRPCEnroll, supervise its completion and
wire the native listener; full local-transport and real OS/system acceptance are
not established by the coordinator/provider tests.

The native local-transport acceptance test now obtains its registered node through
Enroll rather than directly editing config. It covers unavailable-worker rejection,
OS-authenticated acceptance, caller-context cancellation after acceptance, approval
events without the poll token, a new local client recovering by request ID, worker
restart and the terminal EnrollmentResult before Connect. The control provider is
synthetic; this tests the actual local RPC/worker/journal composition, not a real
control-plane approval or production process restart. The test is shared by the
Windows/Linux/macOS build targets; this local run proves only the current host.

GetStatus and initial/refreshed snapshots now derive enrollment actions from the
current caller's durable waiting operation, not a potentially stale provider
observation. The request ID comes from current config; terminal operations cannot
resurrect old browser links. Other callers (including another administrator) do
not receive the initiating caller's browser action. Unit tests cover ownership
and stale observations; local-transport tests compare GetStatus with recovered
GetOperation during approval and check action removal after completion.

Temporary enrollment transport failures now retain the RUNNING operation, protected
plan and original registration idempotency key for the worker's next attempt.
The agent classifies network errors/timeouts and temporary HTTP statuses at the
transport boundary because SDK failover may flatten them into diagnostic strings.
No retry decision parses error text. Authorization/input/signature failures remain
terminal; cancellation of the runtime remains recoverable shutdown. Tests cover
HTTP 429/5xx versus 4xx, certificate/identity errors, and successful reconciliation
after an ambiguous response using the same saved request and operation. Actual
network-loss/approval recovery against deployed control plane remains system work.

Native ForgetLocalEnrollment now requires OS administrator access and explicit
confirmation for the active profile. It records disconnected intent, waits for
the profile worker's actual Down result and only then applies the local cleanup
matrix. A failed Down retains credentials/session; successful cleanup retains
installation keys, fingerprint, ownership and trust, reports REMOTE_UNCONFIRMED
and carries an existing recovery correlation ID. Tests check authorization,
confirmation, ordering, stop failure and installation retention. Enrollment and
forget mutations invalidate the profile catalog. Active conflicting operations
still return BUSY: cancellation/draining of an in-flight Enroll, inactive-profile
cleanup, remote Logout correlation integration and production/system acceptance
remain required before complete UF-12 acceptance.

Inactive-profile local forget is now an immediate, administrator-confirmed
transaction and does not require the tunnel worker. The transaction rechecks
inactivity, rejects profiles participating in a switch/nonterminal operation,
clears only that profile's registration/session and preserves its preferences,
origin and device binding. Tests verify the active profile, credentials and
connected intent remain unchanged, no Down plan is created, and exact retries
return the original result. Coordinated cancellation of active work and the
remote Logout path remain unfinished.

Active local forget now supersedes the earlier BUSY limitation for a matching
Enroll operation. Acceptance durably marks enrollment cancellation before
signalling its provider context. Enrollment checkpoints reject late writes;
the Down executor drains the registration provider and persists CANCELLED before
performing cleanup. A saved cancellation is also reconciled after coordinator
restart without resending registration. Tests cover rejected/unconfirmed commands
not cancelling work, accepted cancellation, a late provider save, drain-before-Down
ordering and queued enrollment recovery. Other conflicting provider operations
still return BUSY; real OS/process/network interruption acceptance remains open.

Remote logout confirmation has been extracted from CLI parsing/file cleanup into
revokeConfiguredClient(ctx, cfg). Both node revocation and session logout are
bound to the runtime context; this function performs no local config/file cleanup.
Tests verify revocation-before-session ordering, retention of local state on both
remote failures and confirmation, typed revocation correlation and cancellation
of an in-flight session request. The CLI uses the shared function; native Logout
admission, durable remote-confirmation progress and post-confirmation Down/cleanup
are still to be connected. This does not establish complete UF-12 acceptance.

Native Logout admission now persists a profile-bound plan alongside its operation.
Remote confirmation progress has separate monotonic node/session checkpoints,
guarded by the current registration context; credentials and intent are not
removed or changed by these checkpoints. The shared remote workflow accepts saved
progress and a checkpoint callback, skips confirmed steps and stops on checkpoint
failure. Tests cover exact acceptance replay, disk persistence, a new coordinator,
stale credentials, no progress rollback and session-only continuation. The CLI
still runs without durable operation progress until its native RPC migration;
the native Logout executor, terminal outcomes and service exposure remain open.

The native Logout executor now performs remote confirmation, durable progress,
Down and local cleanup in that order. Remote failure reports
REMOTE_CLEANUP_REQUIRED with correlation and retains registration; Down failure
also retains local data. Per-profile confirmation checkpoints are bound to the
original authority with a keyed digest, survive a failed operation and are reused
only for matching credentials on explicit retry. Connect rejects an already
revoked authority. Successful cleanup removes both active and saved-profile
credential copies and clears confirmation state. Down-start progress avoids
claiming uninterrupted continuity after an ambiguous restart. Tests cover remote
and Down failures, explicit retry, retained steps and removal of credential copies.
The agent adapter calls the typed remote workflow, but worker/RPC exposure and
production host integration still remain to wire; this is not full UF-12 evidence.

Logout is now exposed by the native RPC service when its profile worker has a
remote-logout provider. Startup rejects an unfinished logout if that provider is
missing; otherwise it resumes saved progress before processing later work. The
agent profile driver supplies the typed remote adapter. Tests cover shutdown after
node confirmation and resumed session cleanup, plus Logout acceptance/caller
cancellation/terminal cleanup over the actual local transport. Logout also
invalidates the profile catalog. Providers in transport/worker tests are synthetic;
production agent listener cutover and deployed remote-cleanup acceptance remain
separate unfinished gates.

Failed remote Logout now retains its correlation ID with the profile's
authority-bound confirmation state, independently of the terminal journal's
retention window. Active and inactive ForgetLocalEnrollment copy that ID into
CleanupResult before deleting registration. A changed session/authority cannot
inherit an old cleanup correlation. Tests prune the terminal logout record,
exercise actual profile handover, and verify both preservation and rejection of
stale correlation. This is local contract evidence, not deployed revocation proof.

Accepted Disconnect now preempts an in-flight Logout attempt through a separate
provider context. It retains the logout operation and confirmed remote progress,
releases the worker for Down and allows later logout reconciliation to continue.
Rejected CAS/input never cancels the remote attempt. The worker test blocks a
logout provider, accepts Disconnect, verifies durable successful disconnection and
checks that the session and logout progress remain recoverable. Actual network
provider cancellation latency and OS Down latency still require system evidence.

SetPreferences and ResetPreferences now enforce the active-profile requirement
before changing UI_QUIT overrides. Reading inactive-profile preferences remains
allowed. Regression coverage verifies STALE_STATE, unchanged revision and saved
preferences after both rejected mutations, and isolation of an accepted active
profile update. Local vet, lint and short tests pass; other preference providers
and production IPC activation remain unfinished.

Logout no longer projects DISCONNECTING merely because remote revocation is
running. The last observed tunnel phase survives that stage and a remote-only
failure; the persisted Down-start checkpoint triggers DISCONNECTING instead.
Down failure still invalidates the observation, and confirmed local cleanup
reports DISCONNECTED. Executor regression tests distinguish remote and Down
failures. This fixes native status projection, not production runtime activation.

The native service now owns a coordinated host lifecycle through Serve: profile
and enrollment workers start before serving, listener or worker failure cancels
the sibling and closes admission, and return waits for both workers and the
server. Partial startup also closes the supplied local listener and drains any
started worker. Unit tests cover listener failure, missing enrollment provider
and a corrupt enrollment plan failing while the listener is blocked in Accept.
The production agent still calls the old IPC host; replacing that call, wiring
status observation and migrating consumers remain required cutover work.

The production agent entry point now opens the native v0 local host with the
typed profile/logout and enrollment providers. The old agent HTTP IPC startup
functions were removed; there is no transport fallback. Host failure cancels the
agent loop and is returned after workers drain, before engine close. A real local
transport test covers native bootstrap, repeated stop and rejection after stop;
mixed/platform-inappropriate endpoints are rejected. No-endpoint headless mode
remains available. This is a source cutover, not deployed acceptance: CLI/UI and
helper consumers still need migration, old handler/test code remains to remove,
native runtime status observation and the remaining providers are unfinished.

CLI `service status`, `service runtime-info` and `service support-info` now use
native v0 with mandatory Bootstrap contract/digest verification and protobuf JSON
output (not the retired HTTP response envelope). Windows, Linux and macOS use
their own default local endpoint; mixed or wrong-platform flags fail before I/O.
The former v2 status case was replaced with real local-host CLI dispatch tests
which decode replies against the accepted generated messages. Invalid arguments
and timeouts are covered too. Other CLI mutations/events, helper and UI migration
and live runtime status observation remain separate unfinished work.

CLI `service events` now uses v0 WatchEvents and emits protobuf NDJSON after a
verified Bootstrap. It requires the full opening snapshot, contiguous sequence,
the same runtime instance and nondecreasing nonzero revisions. Unexpected EOF or
invalid events fail explicitly; only an elapsed/cancelled bounded subscription
that already emitted a snapshot completes normally. The old hello/HTTP event
command and its tests were removed. Replacement tests cover native local CLI
subscription, timeout before/after snapshot, EOF, sequence/metadata corruption
and output failure. Automatic reconnect is not implied; callers must resubscribe
for a fresh snapshot after a reported stream failure.

CLI `service operation --request-id UUID` or `--operation-id UUID` now reads the
durable v0 operation through a new authenticated connection, after Bootstrap.
Exactly one lookup is required. It emits the generated GetOperationResponse as
protobuf JSON and never retries a mutation. Real local-host tests create an
operation, recover the identical record by both identifiers through CLI dispatch,
and verify NOT_FOUND with no output for an unknown request. This is the recovery
primitive for upcoming native CLI mutations, not evidence that all mutation
commands have already migrated.

CLI `service connect`, `disconnect` and `logout` now send typed v0 mutations.
Each requires explicit `--request-id`, `--expected-instance-id`,
`--expected-revision` and `--profile-id`, allowing callers to retain the command
before transmission. Success output is the protobuf response/operation, not a
claim that every accepted asynchronous operation has finished. Errors after
dispatch/output include request-ID lookup guidance and never trigger automatic
replay or CAS refresh. The retired HTTP command cases were removed. Real local
transport CLI tests cover stale disconnect rejection, confirmed synthetic Down,
identical retry results, and unregistered Connect/Logout rejection. The engine
fixture is synthetic; successful live Connect/Logout and OS acceptance remain
separate gates, as do the other CLI/helper/UI consumers.

Native CLI profile management is available through `profiles`, `create-profile`,
`select-profile`, `rename-profile` and `remove-profile`. Mutations retain the same
explicit request-ID/CAS contract; create takes `--display-name` and
`--control-origin` without a target profile, rename takes a target and display
name. `profiles` accepts `--page-size` and opaque `--page-token` and returns the
page metadata needed for subsequent CAS. Local-host tests now select through CLI
instead of directly changing active-profile state, create/rename/remove an empty
inactive profile and traverse both catalog pages. Real platform handover,
enrollment CLI and remaining providers/consumers still require migration or
system evidence.

CLI `service local-forget` now invokes v0 ForgetLocalEnrollment with explicit
request identity, CAS, target profile and `--confirm-local-forget`. Missing or
false confirmation is rejected before connecting. The old HTTP cleanup command
was removed. Local-host CLI coverage follows the authenticated OS role: a
non-administrator must receive PERMISSION_DENIED; an administrator must recover
terminal cleanup reporting REMOTE_UNCONFIRMED, never confirmed remote revocation.
Only the host's actual role branch runs locally; privileged cross-platform and
enrolled-device cleanup acceptance remain separate system-test obligations.

`service enroll` now uses native Enroll with explicit request-ID/CAS/profile,
`--mode`, optional `--hostname`, and exactly one of `--browser-login` or
`--enrollment-token-file` (including `-` for stdin). Origin belongs to the profile;
retired `--server`, `--idempotency-key`, inline token and HTTP-output flags are not
accepted by this command. Responses expose the operation, not token-bearing
request data, and no automatic mutation retry is performed. Tests cover all four
mode encodings, auth oneof round trips, ambiguous auth before file I/O, and
inactive-profile rejection through native CLI dispatch. Successful real-control
CLI enrollment remains a system gate. The separate managed enrollment entry point
still uses its old helper and must be migrated; this change does not remove that
remaining consumer or claim full enrollment cutover.

`service operation --wait` now polls only GetOperation by the accepted operation
ID, emits changed operation responses as protobuf NDJSON, and completes only on
SUCCEEDED. FAILED, invalid identity/state, read/output errors and timeout return
errors; WAITING_FOR_USER remains nonterminal and exposes its action in the output.
Tests cover waiting/deduplicated progress/success, terminal failure, changed
identity, cancellation and output/read failures, plus completed-operation waits
through the local CLI host. This reader is available for replacing managed-up's
old repeated-Enroll loop; that consumer has not yet been switched.

Managed `up` now uses native v0 too. It requires an existing active `--profile-id`,
snapshot CAS and `--connect-request-id`; optional `--enroll-request-id` enables an
Enroll-then-Connect workflow with explicit mode and browser/token-file auth.
Registration is dispatched once, approval is observed through GetOperation, and
Connect uses the successful enrollment operation's revision. Separate UUIDs allow
either stage to be recovered; dispatch/read failures never auto-replay mutations.
Output is changed operation responses, not an inferred claim of tunnel health.
The old managed HTTP loop, retry helpers and their obsolete tests were removed.
Replacement workflow tests cover waiting approval, one enrollment/connection,
rejected approval, output failure, connect-only mode and preserved caller CAS.
These providers are synthetic; deployed approval, live tunnel health and CLI
system acceptance remain required. Old `up --server/--join-token/--approval-timeout`
arguments are not retained as compatibility behavior.

The agent synchronization loop now publishes native status observations through
the same mutation journal/event service. CONNECTED phase requires both successful
application and successful live WireGuard inspection in a nonfailed iteration;
missing/failed observations remain UNSPECIFIED. The disconnected-intent branch
rechecks intent under the runtime operation lock and publishes DISCONNECTED only
after confirmed Down. This also prevents a stale outer-loop intent check from
tearing down a newly selected connected state. Existing observation CAS/fingerprint
guards reject concurrent stale snapshots. Tests cover the apply/inspection/error
matrix and delivery of an observation through the native local host. System
validation must still establish actual routing/connectivity and observation timing;
control probes/inspection are not peer reachability proof.

Agent diagnostic snapshots now carry the source profile ID on both successful
iterations and failure writes. Native status requires that ID to match the active
profile in addition to node/network/revision checks. An unbound old snapshot or
a different profile with coincident node/network IDs is not attached; a prior map
revision of the same profile remains explicitly PREVIOUS. Tests cover rejection
of foreign/unbound snapshots and persistence of the failure snapshot binding.
This local runtime artifact change does not change the v0 protobuf schema/version.

Server identity inspection now has a typed context-bound workflow independent of
v2 DTOs. It validates both trust bundles, never adopts the announcement and uses
a credential-free public-key request. Cancellation reaches an in-flight provider
request and returns the caller's cancellation. Tests cover changed/invalid trust,
preserved local authority, absent Authorization and in-flight cancellation. The
old response adapter currently delegates to it; native GetServerIdentity exposure,
announcement binding and TrustServerIdentity execution still remain to implement.

GetServerIdentity is now served natively and `service server-identity --profile-id`
uses it instead of HTTP v2. The provider receives only profile origin/public trust;
authorization precedes provider I/O and is rechecked afterward. Changed runtime
state rejects a stale result. Announcement identity binds profile/origin and both
validated bundles without accepting the remote key. Tests cover unauthorized
access, provider input isolation, stable/changed announcements, stale reads,
sanitized provider failure and empty-profile CLI rejection. TrustServerIdentity
confirmation/execution and release acceptance remain unfinished; this read does
not assert that identity recovery is available end to end.

Native trust admission now has a durable private plan holding the confirmed
origin/key/announcement ID and a keyed binding to the current registration.
Administrator authorization, active profile, exact origin, complete confirmation,
valid existing trust and exclusive operation checks precede admission. Acceptance
does not replace trust or erase credentials; executor-side refetch and exact
announcement validation are still mandatory. Tests cover invalid/admin rejection,
unchanged authority, persistence and exact retry/changed-payload conflict. This
admission is not yet exposed as TrustServerIdentity: the recovery executor and
worker integration must be completed before the RPC can accept real requests.

The trust executor's announcement stage now re-fetches public authority, checks
the exact confirmation hash/key and rechecks the local authority binding before
checkpointing the validated bundle. It leaves trust, credentials and tunnel
unchanged. Mismatch/provider failure terminates the operation and clears its plan;
context cancellation retains RUNNING for restart recovery. Tests cover matching
and replaced announcements, provider failure, changed local authority, cancellation
and restart with protected checkpoint persistence. Down, trust adoption and
credential recovery are still subsequent unfinished stages; TrustServerIdentity
is intentionally not exposed yet.

The native trust adoption stage now serializes Down with profile/agent tunnel
work, checkpoints DownStarted before the driver call, and rechecks the exact
confirmation and local authority both before and after Down. It atomically saves
the new trust in the root and active profile together with a stable recovery
operation/idempotency ID, preserving credentials for typed renewal. Enrolled
operations remain RUNNING; adoption alone is not recovery success. Unenrolled
trust completes with a ChangeResult; an identical bundle completes unchanged
without Down. Down failure leaves trust and credentials intact. Restart after an
uncertain Down repeats the idempotent stop without claiming preserved continuity.
The automatic agent loop skips native trust plans rather than applying a map or
invoking the old recovery path outside the native journal.

Local short tests cover adoption, paired profile persistence, no-op, Down failure,
stale trust/authority, authority replacement during Down, missing verification,
cancellation/restart and repeat reconciliation. Validation: go vet, golangci-lint
and go test -short. Network recovery, worker/RPC integration and release acceptance
remain unfinished; TrustServerIdentity is still intentionally not exposed.

Credential recovery now has a shared private network/verification workflow that
returns a verified map or typed terminal/retryable progress without accessing
the config store or mutating its input. It checks the recovery plan's confirmed
origin and active trust key before sending credentials, binds all HTTP work to
the operation context, and verifies map identity/signature, node credential and
revision before returning success. Cancellation returns no committable outcome.
The remaining store-based recovery caller now consumes this workflow instead of
duplicating network verification. Direct tests cover success, terminal and
retryable errors, mismatched origin/key, cancellation and unchanged durable
registration; existing recovery retention/idempotency tests also pass.

This prepares the native executor integration but does not complete it: the v0
executor still needs authority-checked atomic result application, retry/terminal
operation transitions and worker/RPC wiring. Validation is local go vet,
golangci-lint and go test -short, not release or cross-platform acceptance.

The native trust recovery reconciler now rechecks the adopted trust, active
profile, registration authority, recovery identity and map state before and after
the provider call. Successful verified results atomically update an explicit
allowlist of registration/map fields in both root and saved profile and complete
the operation. Retryable failures retain the original operation/idempotency plan;
terminal re-enrollment responses clear node-bound state in both copies and report
NEEDS_ENROLLMENT with correlation. Other final failures preserve registration.
Canceled, stale or malformed results cannot overwrite it. Runtime-private provider
errors are sanitized, and the command adapter maps control-plane error categories
to native failure codes without embedding raw errors in IPC.

Tests cover success, retry across a new mutations instance, terminal cleanup,
policy blocking, cancellation, stale authority, malformed results, provider errors
and preservation of session/installation keys/trust/intent. Adapter tests cover
revocation, temporary unavailability, authentication, policy and identity-binding
failures. Local validation: go vet, golangci-lint and go test -short. Worker/RPC
wiring, explicit tunnel resumption semantics and end-to-end acceptance remain
unfinished; TrustServerIdentity is not yet exposed to callers.

TrustServerIdentity is now wired into the native runtime through a dedicated
durable worker and the administrator-authorized admission path. The production
host supplies the typed identity/recovery providers. Startup resumes saved plans;
announcement, Down/adoption and recovery run in order, with bounded five-second
retry polling. HTTP request cancellation does not cancel accepted work. Host
cancellation joins the trust worker alongside profile/enrollment workers, and a
worker failure stops serving. Partial trust-provider configuration fails startup
and drains workers already started.

Trust network calls do not hold the profile/tunnel locks. A synthetic worker test
holds recovery pending while Disconnect completes, cancels the worker, starts a
new mutations instance and verifies recovery completion without refetching the
adopted trust or losing disconnected intent. Host tests cover trust-worker drain
and partial-startup failure. The automatic agent loop resumes its normal work
only after the native plan clears and remains subject to the durable connection
intent; successful trust recovery alone does not claim a connected tunnel.
Local go vet, golangci-lint and go test -short pass. CLI/helper/UI consumers of
trust still require migration; real tunnel recovery and release/platform
acceptance have not been established by these tests.

`service trust-server` now calls native TrustServerIdentity and prints its
Protobuf JSON operation response. The old CLI HTTP trust dispatch and `--yes`
flag are removed. Required inputs are `--profile-id`, `--request-id`,
`--expected-instance-id`, `--expected-revision`, `--confirmed-control-origin`,
`--confirmed-key-id` and `--confirmed-announcement-id`. Inspect identity first
with `service server-identity --profile-id <id>` and retain the exact confirmation
and request UUID before sending. This command reports acceptance, not completed
recovery or connectivity; use `service operation --operation-id <id> --wait` to
follow the operation. Ambiguous dispatch/output errors retain request-ID lookup
guidance and never trigger an automatic replay or refreshed CAS.

CLI tests reject incomplete/malformed confirmation and the retired boolean flag;
the real local-transport host test verifies administrator/precondition rejection
through the native command path. Local go vet, golangci-lint and go test -short
pass. Privileged helper and UI trust consumers still need their own migration;
this CLI change is not evidence of full UF-10/release acceptance.

The privileged recovery helper no longer imports HTTP IPC v2. It uses the fixed
native platform endpoint, validates the runtime contract through Bootstrap, then
dispatches exactly one typed trust or local-forget request with caller-retained
profile/request/CAS and complete confirmation. Stdout is native Protobuf JSON:
Operation on acceptance or Failure on request/bootstrap error, distinguished by
exit status. Argument/output errors can leave no result; no implicit replay or
CAS refresh is attempted. Closed arguments still reject paths, alternate IPC
endpoints, credentials and arbitrary commands. The helper checks returned
operation kind/request/profile identity and sanitizes untyped transport errors.

Rewritten tests cover both native operations, bootstrap failure preventing
dispatch, typed administrator errors, invalid/retired arguments, malformed
announcements, missing responses and output failures without replay. Local
go vet, golangci-lint and go test -short pass. UI launch/result handling, request
correlation after OS elevation (including differing root/user identities), actual
platform elevation and release acceptance remain separate unfinished work.

## Native update discovery without a configured source (2026-09-13)

GetUpdateInfo now provides an installation-level, owner/administrator-authorized
read even without enrollment or a profile. It returns the runtime build and a
copy of the optional caller-reported UI identity. Since no verified distribution
source is configured, state is SOURCE_UNAVAILABLE, discovery is UNSUPPORTED and
installed-pair compatibility is UNKNOWN. Matching version strings do not attest
the UI binary or establish compatibility. No available release, accepted digest,
update URL or installation action is invented. Read errors remain typed; the
operation journal, configuration and connection intent are unchanged.

Short tests cover absent/matching/different UI claims, immutable response copies,
authorization before validation, administrator access without a profile and the
generated consumer over the native local transport. This does not enable the
update-discovery capability or complete UF-22/US-13. This repository still owns
verified release-source integration; client-ui owns discovery rendering and the
platform updater handoff. Real signed artifacts and installed-pair/platform
acceptance remain separate evidence.

## Connection capability readiness (2026-09-13)

A successfully started profile worker with validated Lock/Stop/Start callbacks
now advertises CAPABILITY_CONNECTION through both GetRuntimeInfo and the opening
WatchEvents snapshot. This enables the native UI's existing Connect gate on a
runtime with connection execution wired. Capability means runtime applicability,
not approval, enrollment, a connected tunnel or platform/release acceptance.
Caller authorization, current status and durable mutation admission still apply.
Other incomplete capability families are not enabled by this change.

Readiness is volatile and disappears on cancellation or worker exit. Starting,
stopping or replacing a worker closes existing streams with STALE_STATE so the
consumer explicitly obtains a new opening context; no second snapshot is sent.
A late callback from a previous worker cannot withdraw a replacement's readiness.
Tests cover startup refusal, immutable projections, cancellation, replacement,
fresh-runtime absence and native Bootstrap/CLI/opening-snapshot consistency.
Actual UI/core pairing and supported-platform connection acceptance remain open.

The same readiness registry now includes PROFILES with the profile worker,
LOGOUT only when that worker has a logout provider, and ENROLLMENT only while
the independently configured enrollment worker is alive. Deterministic ordering
and per-worker ownership prevent one worker's stop callback from withdrawing
another's capabilities. Stopping enrollment leaves Connection/Profiles/Logout
available; stopping their worker does not claim completion of accepted work.
The initial wiring did not enable preference/resource/exit/session/update
capabilities. Preference/resource readiness is now also owned by the profile
worker, which executes their shared durable apply/containment plan through the
native driver. Exit and update are not enabled by this profile-worker change;
session readiness belongs to its independent executor.

Native transport coverage reboots its subscription after worker transitions,
then observes approval and completion of the original enrollment operation
without replaying it. Separate tests cover absent enrollment/logout providers,
independent shutdown and readiness without profile/enrollment side effects.
These checks make implemented commands reachable by native consumers; they are
not full BA/SA, installed platform or released-artifact acceptance evidence.

LOCAL_FORGET now follows the profile worker that owns tunnel stop and local
cleanup; administrator authorization and explicit confirmation remain mandatory.
DIAGNOSTICS requires the bundle worker/storage plus both diagnostics and recent
logs providers. PEERS is advertised only when its provider is configured, for
the serving host's lifetime. SUPPORT_INFO covers the available local metadata
read; missing links/offline topic keys remain absent. None of these readiness
claims removes truncation, missing data or caller/profile checks from responses.

Provider-combination tests observe capabilities at listener startup and verify
their removal after listener failure. Missing diagnostics/logs/storage never
advertise the complete diagnostic family; discovery performs no enrollment,
logout, peer lookup or diagnostics probe. The native host test compares all
the configured capabilities through Bootstrap, CLI and opening snapshot.
These are local/component checks; complete diagnostics content, approved support
resources and installed artifact/platform acceptance still require evidence.

### Diagnostics archive administrator scope

The durable diagnostics plan retains the authenticated requester's identity and
admitted administrator role separately from the installation-owner snapshot.
An administrator can collect on an unowned installation or one owned by another
principal without claiming ownership. Archive bytes remain bound to the original
requester: another administrator cannot read them merely by knowing the handle.
Reads recheck current authorization. Installation ownership changes invalidate
the archive before physical purging, including after process restart. Pending
plans retain the existing profile, cleanup-journal and snapshot-consistency checks.

`TestRPCAdministratorBundleScopeAndRestart` covers both installation states,
durable plan execution, restored archive reads, foreign/demoted callers and
ownership revocation. These component checks do not replace the platform CI
diagnostics export scenario or full BA/SA acceptance.
