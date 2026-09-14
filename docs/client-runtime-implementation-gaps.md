# Client runtime implementation gaps

Status: incomplete implementation; initial source audit, 2026-09-13.
Scope: `client` and its producer contracts only. No changes to UI, backend
services or infrastructure are authorized by this implementation plan.

## Execution order

1. Map every applicable BA/SA requirement to runtime code, unit tests and external
   dependencies. The inventory below is a starting point, not the complete matrix.
2. Implement the required functionality with positive and negative unit tests:
   authorization, validation, durable idempotency, concurrency and recovery.
3. Audit functional completeness before moving to integration, system and
   platform acceptance. Missing providers do not count as implemented features.
4. Fix acceptance defects with unit regressions where applicable. SDK generation,
   transport conformance and green CI alone do not establish feature completeness.

## Confirmed source gaps

CI execution policy: branch pushes run only `go test -short ./...` in the Test
workflow, separately in the root and nested `clientipc` Go modules. The root
package pattern does not traverse nested modules. Full verification,
installation and contract matrices run on PR or
explicit manual dispatch. CodeQL and Protobuf contract checks no longer run on
push. Tag-triggered publication workflows are unchanged; publication still
requires exact-source full manual evidence, not a green short-test push. No PR
or manual integration run is created merely to bypass the implementation phase.

The generated handler interface is in
`clientipc/v0/clientipcconnect/service.connect.go`. `ClientRPCService` embeds its
unimplemented handler in `internal/client/service_rpc_handlers.go`. At this audit,
the five methods below have no runtime overrides; generated SDK methods and CLI
commands must not be counted as their runtime implementations.

| Requirement area | Missing runtime methods | Required implementation and unit evidence |
| --- | --- | --- |
| US-05 exit | `GetExitNode`, `SelectExitNode`, `ClearExitNode` | Family/LAN constraints; requested/effective distinction; durable selection; partial apply/clear and fail-closed path loss; list-only support does not establish selection |
| US-11 resources | `ListResources`, `SetResourceEnabled` | Verified resource identity and policy; search/pagination snapshot binding; local intent; hidden-resource denial, stale context and conflicts |

Additional partial implementations must not be mistaken for complete domains:

| Area | Source evidence | Remaining work |
| --- | --- | --- |
| Preferences/policy | `service_rpc_uiquit.go` accepts only a single `UI_QUIT` key for Set/Reset; `service_rpc_preferences_test.go` and `service_rpc_uiquit_test.go` exist | Implement the remaining specified settings and policy controls; unit-test presence versus false, locks/source, requested/effective state and atomic patch rejection |
| Diagnostics | `service_rpc_diagnostics.go` projects bounded OS route samples; missing samples remain explicitly unavailable and supplied samples remain incomplete | Qualify platform command execution later; extend route coverage beyond host-address sampling without substituting desired configuration for observed OS state |
| Updates | `service_rpc_update.go` reports `update_source_not_configured`; `service_rpc_update_test.go` exists | Bind an approved distribution source and verify its projection; unavailable is not up-to-date, and unavailable-path tests do not prove update discovery |

Existing test filenames above identify starting points for review, not assertions
that all listed scenarios are already covered.

## Client-owned scenario map

The separate [headless per-IT trace](client-headless-requirement-map.md) retains
all 33 SA integration specifications as client implementation/unit obligations
and explicit external observations. It is an open audit, not integration results.

This maps all fourteen system-scenario families to the client boundary. It is
not yet the per-requirement BA/SA completion matrix. All rows remain open until
each applicable requirement and negative outcome has been individually audited.
Paths in the table are under `internal/client/` unless qualified otherwise.

| Scenario | Runtime / contract implementation | Unit starting point and verified limit | Remaining client work / external dependency |
| --- | --- | --- | --- |
| US-01 bootstrap | `service_rpc_host.go`, `service_rpc_capabilities.go`; protected transport in `clientipc/local` | `service_rpc_host_capabilities_test.go`; separate `go test -short ./...` in `clientipc` passed locally on Windows on 2026-09-13 and is included in push CI | Audit startup/identity/capability failure variants; local Windows results do not qualify Unix transport or all bootstrap requirements |
| US-02 enrollment | `service_rpc_enrollment_worker.go`, `service_rpc_enrollment_executor.go` | `TestRPCEnrollmentWorkerRecoveryAndShutdown` | Trace approval, denial, expiry, cancellation and ownership outcomes individually; actual backend approval is external |
| US-03 connection | `service_rpc_connect.go`, `service_rpc_disconnect.go` | `TestRPCConnectDurabilityAndFailure`, `TestRPCDisconnectPreemptsApplyOnlyAfterAcceptance` | Audit remaining races and recovery boundaries; unit driver results are not OS traffic evidence |
| US-04 networks/peers | `service_rpc_networks.go`, `service_rpc_select_network.go`, `service_rpc_peers.go` | `TestRPCSelectCurrentNetworkIsDurableNoop`; peer pagination/event suites | Current-network no-op is not cross-network selection; audit real provider switching and stale catalogs |
| US-05 exit | `service_rpc_exit_catalog.go` lists live grants from the authenticated active-profile map | `TestRPCExitCatalogUsesSignedBoundGrants` checks signature/identity/expiry/privacy/page binding; selection remains unavailable | Durable selection, platform family/LAN support, effective status and fail-closed effects remain unimplemented |
| US-06 trust/recovery | `service_rpc_trust_worker.go`, `service_rpc_trust_recovery.go` | `TestRPCTrustWorkerRecoveryAndIndependentDisconnect` | Audit exact authority tuple, replay and privilege outcomes; helper/OS integration remains later evidence |
| US-07 diagnostics | `service_rpc_diagnostics.go`, bundle worker/store/read handlers, `route_observations.go` | `TestRPCDiagnosticsRejectsChangedContextAndInvalidProvider`, `TestRPCAdministratorBundleScopeAndRestart`, injected route collector tests | Route sampling implemented but platform execution unqualified; inspect redaction, bounds and archive lifecycle coverage separately |
| US-08 profiles/logout | Profile, logout and forget handlers | `TestRPCProfileRemovalGuards`, `TestRPCForgetCancelsQueuedEnrollmentAfterRestart`, `TestRPCProfilePaginationBindingsAndPrivacy` | Audit all removal/cleanup/replay cases; remote revocation depends on backend result |
| US-09 session/renewal | Session reads, durable renewal/poll executor, dedicated host worker, public `RenewSession`, bound snapshot identity and generated backend transport | Session read/clock/store suites; `service_rpc_session_worker_test.go`, `service_rpc_session_executor_test.go`; C `service_rpc_session_transport_test.go` | Full concurrency/cleanup audit, additional approved browser origins and platform/backend qualification remain open; no seamless-renewal acceptance claim |
| US-10 preferences/policy | `service_rpc_uiquit.go`, preference validators | `TestRPCUIQuitRejectsUnsupportedPatchAtomically` rejects mixed UI-quit/DNS patch without changing revision or override | Implement remaining settings; rejection is not DNS/routes/policy functionality |
| US-11 resources | Missing list/mutation overrides | No runtime implementation to qualify | Implement catalog, policy-aware enablement and actual runtime effects |
| US-12 lifecycle | `service_rpc_uiquit.go` | `TestRPCUIQuitPreferencesAndExecution` | UI-quit support is not logoff/suspend/resume adapter implementation; audit each specified event |
| US-13 distribution/help | `service_rpc_update.go`, `service_rpc_handlers.go`; packaging and producer manifest workflows | `TestRPCUpdateInfoDoesNotInferReleaseOrPairing` | Verified update source remains absent; installation/release evidence deferred until implementation phase completes |
| US-14 presentation/privacy | Typed status/operation/log/diagnostics projections | `TestRPCObserverSnapshotExcludesPrivateState`, diagnostics suites | Audit producer reasons/actions and secret redaction; UI rendering, locale selection and assistive technologies belong outside this task |

The historical [runtime gap audit](native-runtime-gap-audit.md) reported nine
missing methods at its pinned source. The current count is five: `GetUpdateInfo`
has an explicit unavailable-source implementation and `GetSession` now performs
a backend read, `RenewSession` now has a runtime worker and transport, and
`ListExitNodes` reads authenticated live exit grants from the cached map.
This does not complete update discovery or session renewal acceptance.

US-12 unit increment: `TestRPCUIQuitReplayDoesNotApplyChangedPreference` checks
that an acknowledged keep-intent notification replays the original operation
after changing the preference to disconnect, both before and after reopening
the durable store. Replay must not change revision or connected intent. A new
request ID must still schedule disconnect using the new preference. This tests
local durable admission, not OS suspend/logoff integration or completed Down.

US-10 projection increment: `TestRPCPreferencesRequireActiveProfileForMutation`
checks that an inactive profile's UI-quit setting reports a temporary mutation
restriction with the stable `preference_requires_active_profile` reason, while
the active profile reports availability. The effective default, absence of an
override and snapshot revision are preserved. The same test checks that Set and
Reset reject the inactive profile without state changes. Managed settings reuse
this preferences projection; the other preference keys remain unimplemented.

`TestRPCPreferenceReadAuthorizationAndValidation` covers owner/administrator
reads, anonymous callers (including a claimed administrator with no identity),
and non-owner reads against existing, missing and malformed profile references.
Authorization must precede profile lookup so those references do not reveal
profile existence to observers. Authorized malformed/missing references return
typed validation/not-found errors. Failed reads expose no response, and every
case leaves the durable configuration unchanged. These are handler-domain unit
assertions, not evidence for OS peer authentication or backend policy behavior.

US-07 route-target increment: `WireGuardRouteTargetsForPeers` now includes every
host address of each peer, rather than only the first address. The unit test
`TestWireGuardRouteTargetsRetainEveryPeerAddress` covers a dual-stack peer,
additional host addresses, equivalent textual duplicates, invalid values and
subnet exclusions. Agent/CLI route-target consumers receive the complete host
list; the separate OS sampling implementation and its limits are described below.

The diagnostics service now accepts a separate `OSRoutes` observation list and
projects it only with a verified profile map. Desired `Tunnel.Routes` are never
used as OS evidence. `TestRPCDiagnosticsSeparatesObservedRoutesFromDesiredRoutes`
checks this boundary, interface comparison, failed observations, redaction,
copy isolation, address validation and entry limits. Partial observations retain
the incomplete marker.

`ObserveOSRoutes` is now wired into agent diagnostics after map verification.
It samples up to 32 unique literal host addresses with a shared three-second
deadline and a 16 KiB per-command output bound. Linux uses `ip route get`, macOS
uses `route -n get`, and Windows uses `Find-NetRoute` with exactly one unique
interface alias and a hidden process window. Untrusted address text is never
interpolated into a command. Subnets, scoped addresses, unsupported platforms,
lookup failures and exhausted collection budgets do not become successful route
claims. Responses remain marked partial: sampling peer host addresses is not
full routing-table or subnet/exit acceptance.

`route_observations_test.go` covers all three command/parser branches with an
injected executor, duplicate/invalid addresses, cancellation, target/output
limits and failure redaction. The agent test verifies that an unverified map
does not invoke route collection. No real OS route command is executed by these
unit tests; platform execution qualification belongs to the later test phase.

Snapshot intent audit: `snapshotLocked` clears provider-supplied intent and the
user-disconnected flag before projecting the durable configuration, including
when that configuration has no intent. This prevents a cleared intent from
reappearing through a stale observation. The regression
`TestRPCSnapshotDoesNotRestoreClearedIntentFromObservation` checks both owner
and observer snapshots. This proves projection precedence, not actual tunnel
shutdown or backend session revocation.

`TestRPCRejectedObservationPreservesLastAcceptedStatus` exercises five config
changes during a probe without an RPC revision change: active profile, node,
network, owner and connection intent. The full-config fingerprint must reject
the late observation with `STALE_STATE`, retain the last accepted observation
and not increment the revision. This is a unit test of observation admission;
it does not exercise a real profile-switch worker or qualify cross-network
traffic isolation.

Event resource bounds: `subscribe` admits at most 16 subscriptions per runtime
and four per OS identity (using the same case-insensitive identity comparison
as authorization). Admission and removal share the mutation lock. Rejection
returns typed `LIMIT_EXCEEDED` before allocating a snapshot/queue; administrators
do not bypass the resource cap. Unsubscription releases the slot. Together with
the existing 8 MiB queue bound this caps accounted queued event payload at
128 MiB, not total process/transport memory. `TestRPCEventSubscriptionLimitsAndRelease`
checks both caps, identity casing, anonymous rejection before capacity errors,
slot reuse and snapshot-first sequencing. Real slow-consumer load qualification
remains deferred.

`TestRPCConcurrentEventSubscriptionAdmission` issues 32 concurrent subscription
attempts, first for one identity and then for distinct identities. It checks
exactly four or sixteen admissions respectively, typed capacity errors for all
remaining attempts, and zero retained registrations after unsubscription. No
subscription is released until every admission attempt has completed. This is
unit concurrency evidence, not a race-detector or transport-load run.

Session read increment (2026-09-14): `GetSession` authorizes the local caller,
selects the profile's user bearer, invokes the bounded TLS backend adapter and
validates the response with the producer's `ValidateSessionResponse`. Only state
and optional session deadlines are projected; backend IDs and renewal bearer
are never forwarded. Expiry/warning transitions use the session clock, not node
credentials. Missing bearer produces unauthenticated session state without a
backend request. Changed configuration, revoked owner and cancelled requests
cannot publish a successful response. The read now persists validated backend
session/renewal authority in the protected ConfigStore, bound to the user bearer
digest and control origin. Changed observations advance revision and invalidate
the session domain; identical protobuf responses do not rewrite state. Renewal
is explicitly unavailable until its durable execution workflow is implemented.
`TestRPCSessionReadProjectionAndContext` covers these
read-domain cases with an injected backend provider; actual backend deployment,
transport integration and platform/backend qualification remain open.

`rpcProjectSession` is shared by GetSession and snapshot/event projections.
Owner/admin snapshots only use stored authority matching the active profile
origin and current bearer binding; stale provider Session fields cannot override
it. Missing observations remain unknown, missing bearer is not-authenticated,
and observer snapshots exclude session data. `TestRPCSessionSnapshotUsesBoundAuthorityAndOwnClock`
checks those boundaries plus expiry independent of a valid node credential.
Fresh snapshots evaluate the session clock. The host now owns a one-second
session-clock worker for open subscriptions; it advances revision and publishes
status plus session invalidation only when a subscriber's projected state changes.
Observers retain the public status projection without session data/invalidation.
`TestRPCSessionClockPublishesOnlyTransitions` checks warning/expiry using controlled
time, no duplicate emissions, and unchanged tunnel intent. The cancellation test
checks worker termination. Host failures cancel/join this worker alongside other
workers. This does not renew a session or establish real idle-stream timing on
every supported OS.

`TestRPCSessionAuthorityPersistenceAndCleanup` checks private authority retention
across disk reopen, returned revision, identical-read stability, no bearer in IPC,
copy isolation, and durable removal on logout or bearer replacement/removal.
Config normalization also clears mismatched authority in inactive profiles;
inactive-profile execution coverage remains to be completed. The general
diagnostic redactor recognizes bearer, renewal-authorization and stored-session
keys (`TestDiagnosticsSessionAuthorityKeysAreSensitive`). Existing state
protection is reused without a format/version increase. This is not proof of
renewal success, fresh platform ACL qualification or backend deployment.

Renewal admission foundation: internal `renewSessionAs` writes an immutable
backend `RenewSessionRequest`, its renewal authorization, user/profile/origin
binding and local operation ID into protected RPC state before any network
effect. It uses producer request validation and permits an expired access
session only while its separate renewal grant remains valid. Public
At this earlier increment, `RenewSession` remained unimplemented until the execution/recovery worker existed;
no public caller can enqueue this unfinished workflow. The unit test
`TestRPCSessionRenewalAdmissionAndDurableReplay` checks owner denial without
mutation, pending-operation privacy, exact disk-backed replay and rejection of
a second operation. Backend dispatch, polling, response authority validation,
full token-rotation coverage, cancellation/cleanup and their negative coverage
remain required before exposing the RPC or claiming renewal functionality.
The retained renewal grant is now separate from the latest session observation:
the backend forbids issuing renewal authority in an expired/revoked response.
An expired observation preserves an earlier grant only when its user/session,
origin and bearer binding match and its own deadline is still valid. Revocation,
changed identity, grant expiry or an active response disabling renewal drops it.
Admission validates both the current observation and retained grant. The test
`TestRPCSessionGrantRetentionAfterExpiryObservation` covers all six cases through
read, disk reopen and renewal admission. This does not make the public renewal
RPC or execution worker complete. No legacy grant fallback is introduced.

Internal `completeSessionRenewal` now validates a successful backend result with
the producer validator and saved request/user/owner/profile/bearer bindings,
rejects expired replay/results, and atomically rotates the access bearer and
protected session while committing the public terminal operation and removing
the execution plan. It requires the operation to be RUNNING; no public RPC
dispatch is enabled yet. `TestRPCSessionRenewalResultAtomicRotationAndBinding`
checks successful rotation/restart/replay and six rejected result/context cases,
including unchanged durable state on rejection and preserved disconnected intent
and node registration. Network dispatch, polling operation-ID binding, browser
approval, ambiguous-failure retry and cleanup still require implementation.

Internal browser checkpointing now persists backend operation ID, private polling
authorization, replay deadline and next-poll time atomically with WAITING_FOR_USER.
The producer validator checks request binding and the configured control origin;
untrusted origins, elapsed actions, malformed polling authority and URLs carrying
known bearer material (including percent-encoded material) are rejected. Later
checkpoints and successful completion cannot switch a pinned backend operation
ID/replay deadline. `TestRPCSessionBrowserCheckpointPrivacyAndBinding` covers
checkpoint restart/privacy and these negative cases. The network polling worker,
additional approved authentication origins and public RPC admission remain open;
this internal checkpoint is not browser-flow acceptance.

`ReconcileSessionRenewal` now executes a single durable renewal/poll step using
producer DTOs and separate renewal/poll authorities. RUNNING and retry scheduling
are committed before dispatch; an ambiguous transport failure retains the same
request ID across restart. Server polling delays and replay deadlines are enforced.
Rejected, expired, malformed or stale-context outcomes clear private execution
authority with a durable typed failure; success rotates the token atomically.
`TestRPCSessionRenewalExecutorRestartPollingAndFailure` covers these paths,
cancellation before dispatch and token replacement during polling. The background
host worker, actual backend transport and public RPC/capability wiring remain open;
these unit checks do not establish end-to-end session renewal acceptance.

The session worker and public `RenewSession` are now wired into the native host;
the agent uses the generated producer client with separate renewal and polling
Authorization headers, HTTPS-only origins and bounded messages/timeouts. Capability
readiness follows worker lifetime. Session reads/snapshots expose a bound active
renewal operation ID and availability based on the current grant and worker.
`TestRPCSessionWorkerAdmissionReadinessAndShutdown` checks owner admission,
observer rejection, replay, duplicate-worker exclusion and cancellation/join.
`TestAgentSessionRenewalTransportUsesDedicatedAuthority` exercises generated
serialization against an in-memory handler (no external backend or socket).
Full cleanup/concurrency coverage, approved cross-origin browser approval and
real provider/platform acceptance remain open; seamless renewal is not claimed.

Confirmed local forget now durably requests cancellation of a same-profile
renewal, cancels an in-flight provider only after admission commits, and drains
the session executor before local cleanup. Cancellation removes private renewal
and polling authority and records a replayable CANCELLED operation without a
browser action. `TestRPCForgetCancelsSessionRenewalAndDrainsBeforeCleanup` covers
a late successful provider response, rejected unconfirmed forget and queued or
browser-waiting cancellation after restart. Logout/profile-switch concurrency
and the rest of the renewal cleanup matrix remain under audit.

`TestRPCSessionRenewalConflictsAndIndependentDisconnect` now verifies both
admission orders for renewal versus logout/profile switch: conflicts return BUSY
without changing durable state. A blocked renewal provider does not block
Disconnect, and later successful rotation preserves disconnected intent and node
authority. Session-clock invalidation now compares the entire public session
projection, not only its state enum. `TestRPCSessionClockPublishesRenewalGrantExpiry`
verifies that renewal grant expiry withdraws availability while access remains
ACTIVE, emitting one transition rather than silently leaving stale availability.
These are unit observations, not real traffic or platform qualification.

Exit catalog uses the pinned producer's signed `ClientPolicy.ExitNodes`, not
advertised/default-route inference or a nonexistent backend Exit RPC. The entire
cached map is validated and authenticated, with local node/network/revision
binding and live map/grant deadlines. Page tokens bind the signed payload and
visible catalog so grant expiry cannot continue a stale page. The list exposes
no selectable modes and reports `exit_executor_unavailable` until actual
platform-aware selection/application is implemented. No exit capability is
advertised by this read-only increment; Get/Select/Clear and traffic acceptance
remain open.

## External dependencies and approvals

- `clientapi` owns backend DTOs, policy validation and session transport. On
  2026-09-14 the user explicitly approved consuming producer revision
  `e4fb0a95d2af577cda7425abec064a4ed49a3eae`. `client` now pins its resolved
  Go pseudo-version `v1.12.1-0.20260913120316-e4fb0a95d2af` in `go.mod`.
  This removes the dependency-update approval blocker for session and policy
  contract consumption; the missing runtime handlers listed above remain open.
  No other dependency version or producer repository is changed. Further
  version increases still require explicit approval. Do not copy backend DTOs,
  use a local replacement or rewrite a published version.
- Backend owners must implement the corresponding producer behavior. The
  existence of a producer contract is not evidence of deployed behavior.
- Distribution source selection and external platform/provider capabilities
  require their owners' input. Record each concrete dependency as requirements
  are audited; do not invent product links, sources or acceptance evidence.

These dependencies do not prevent work on independent client-owned functionality
and unit tests. They do prevent claiming the affected requirements complete.
