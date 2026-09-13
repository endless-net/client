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
