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

The [local v0 requirement matrix](client-local-requirement-map.md) complements
the headless BR/AC/RULE/IT inventory with all UF/UBR/UR/UI-AC IDs from the pinned
BA and their US implementation/unit and external-owner joins. Inventory coverage
does not close the remaining assertion audit or implementation gaps below.

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
the four methods below have no runtime overrides; generated SDK methods and CLI
commands must not be counted as their runtime implementations.

| Requirement area | Missing runtime methods | Required implementation and unit evidence |
| --- | --- | --- |
| US-05 exit | `GetExitNode`, `SelectExitNode`, `ClearExitNode` | Family/LAN constraints; requested/effective distinction; durable selection; partial apply/clear and fail-closed path loss; list-only support does not establish selection |
| US-11 resources | `SetResourceEnabled` | Policy-aware local intent and actual effects; hidden-resource denial, stale context and conflicts; the catalog alone does not implement enablement |

Additional partial implementations must not be mistaken for complete domains:

| Area | Source evidence | Remaining work |
| --- | --- | --- |
| Preferences/policy | Public Set/Reset supports inbound/DNS/routes through the durable worker, optionally together with UI_QUIT; UI_QUIT-only operations are immediate. Signed policy resolution and pending/committed reads are implemented | Implement remaining lifecycle keys; complete per-method transport, concurrency/recovery and OS effect evidence |
| Diagnostics | `service_rpc_diagnostics.go` projects bounded OS route samples; missing samples remain explicitly unavailable and supplied samples remain incomplete | Qualify platform command execution later; extend route coverage beyond host-address sampling without substituting desired configuration for observed OS state |
| Updates | `service_rpc_update.go` reports `update_source_not_configured`; `service_rpc_update_test.go` exists | Bind an approved distribution source and verify its projection; unavailable is not up-to-date, and unavailable-path tests do not prove update discovery |

UI-quit managed-policy increment: `service_rpc_lifecycle_policy.go` resolves
KEEP_INTENT/DISCONNECT from the authenticated profile-recipient map. Reads,
Set/Reset and UI_QUIT execution share that resolution. An unlocked managed
value supplies the reset baseline; a locked value constrains an existing user
override and rejects a conflicting new override. Provenance and the responsible
administrator are projected. Invalid/expired/context-mismatched maps do not
silently supply a default; accepted request replay precedes current-policy
validation. Managed CONNECT still requires lifecycle connect execution and is
explicitly unsupported. Other lifecycle keys, network preferences and full OS
lifecycle implementation remain open. This increment does not close US-10/12.

Active map/policy catalog invalidation now runs both on accepted runtime status
observations and independently in the RPC host's one-second catalog clock.
Source changes and map/exit/application expiry invalidate owner-visible peers,
networks, exit, preferences, managed settings and resources even when Status is
unchanged. This does not implement resource effects or OS lifecycle events;
inactive-profile and complete cross-domain invalidation audit remains open.

Network acceptance increment: `network_preferences.go` persists optional DNS and
route intent in the profile Config and resolves signed managed baselines/locks.
The shared userspace router builder consumes this resolution: disabled DNS has
no proxy, resolver, search or split-domain configuration; disabled learned routes
retain ordinary host routes but omit subnets, explicitly managed single-IP
subnets and application routes. Signed sources are recipient-bound and checked
before preference resolution. This is used by the existing OS router adapters,
but the unit evidence is derived router configuration, not observed OS effects.
OS effect qualification remains missing; inbound public admission/projection is described below.
The worker increment below provides candidate apply and durable containment.
Checked static export now receives the full Config and uses
the same authenticated DNS/routes resolver; it cannot reintroduce a disabled
resource route or DNS setting. Static output is still not a live policy/expiry
enforcer. No additional
preference capability is advertised by this increment.

Durable admission preparation: `service_rpc_network_preferences.go` records an
atomic DNS/routes patch (optionally including UI-quit) separately from active
configuration, bound to owner, profile, node, network and signed map payload.
`TestNetworkPreferenceAdmissionIsDurableAndDoesNotApply` covers restart and
same-request replay ahead of CAS/conflict checks;
`TestNetworkPreferenceAdmissionRejectsWholePatch` covers authorization, signed
source tampering, revision mismatch, managed lock and unsupported/empty patches;
`TestNetworkPreferenceResetPreservesUnselectedOverrides` covers selective reset.
Public Set/Reset now routes patches containing DNS/routes through the ready
profile worker; UI-quit-only operations retain their immediate local semantics.
`service_rpc_network_preferences_worker.go` now runs in the profile worker's
startup scan under the shared agent driver lock. It authenticates the candidate
before apply and again at commit, rejects changed profile/owner/map/intent,
and commits network and UI-quit overrides together. Failure enters a durable
containment stage with disconnected intent before Down; a Down error is
resumable after restart and never retries the failed candidate. This rollback
retains prior preference values and disconnects; it does not restore a live
connection. `TestNetworkPreferenceWorkerApplyAndContainment` covers successful
candidate delivery, offline application, failure, concurrent Disconnect/map
tampering and shutdown. `TestNetworkPreferenceContainmentRecoversAfterDownFailure`
covers the persistent recovery stage. Driver callback success is unit evidence,
not OS observation. Containment now persists the original bounded failure
code/reason before Down: concurrent Disconnect yields CANCELLED and retains its
accepted intent reason; context drift yields STALE_STATE, invalid signed source
yields UNAVAILABLE, and driver errors yield APPLY_FAILED. Restart tests assert
that a later Down failure does not replace the original apply cause.
`service_rpc_network_preferences_public.go` projects authenticated committed
policy resolution separately from pending requested overrides, including reset
absence and managed provenance/locks. GetPreferences and ListManagedSettings
use that projection, and preference operations invalidate resources as well.
`TestNetworkPreferencePublicAdmissionWakesWorkerAndSeparatesPending` exercises
the readiness/acceptance bridge, worker wakeup and pending-to-committed reads;
`TestNetworkPreferenceProjectionManagedLockAndReset` covers lock precedence
and reset baseline restoration. This is not authenticated transport acceptance
or observed OS effects, both of which still need evidence.

Preference concurrency/readiness correction: UI-quit-only Set/Reset cannot
mutate a pending network patch; the generic journal still resolves previously
accepted retries before this conflict check. Pending UI-quit projection disables
mutation, and unlocked DNS/routes mutation availability follows the serving
profile worker, including cancellation. `TestUIQuitMutationCannotBypassPendingNetworkPatch`
checks rejection without persistent changes and accepted replay;
`TestNetworkPreferenceMutationProjectionTracksWorkerReadiness` checks missing,
live and cancelled worker states without altering preference values.

Inbound filter preparation: `inbound_filter.go` implements a bounded volatile
outbound-flow table for restricting new inbound traffic while preserving matched
TCP/UDP/ICMP replies. `TestInboundFilterTCPHandshakeExpiryAndPolicyChange` checks
unsolicited TCP denial, handshake ordering, expiry and policy-change clearing;
`TestInboundFilterUDPBindingBoundsAndReplyLifetime` checks port binding, fixed
reply windows, capacity/expiry recovery and malformed packets. Full IPv6
transport coverage and runtime/public integration still need evidence; allow_inbound
remains incomplete. The optional filter is now the final conjunction in TUN
Read/Write, so traffic denied by earlier ACL/application/sharing filters cannot
seed its flow table. `TestInboundTUNFilteringPreservesBatchesAndCannotSeedDeniedFlows`
checks the actual wrapper with batch offsets, unsolicited reply denial, matched
replies and an enabled preference that cannot bypass peer ACL.
`TestInboundFilterEchoFamiliesAndExtensionRejection` checks IPv4/IPv6 echo
identity/sequence, reply expiry and fragment rejection. Engine construction now
installs this filter on the real userspace TUN. The shared producer-policy
resolver includes optional AllowInbound, with locks taking precedence over local
overrides. Configure tightens before apply and only relaxes after success;
failure retains restriction. Identity changes and Down clear volatile flows.
Static export rejects a resolved inbound restriction rather than dropping it.
`TestInboundPolicyResolutionAndExportRestriction` covers signed baseline/reset,
locks and checked-export rejection. `TestInboundEngineAppliesPolicyAndClearsIdentityFlows`
exercises engine Configure with a test TUN/router, restriction and relaxation,
and filter identity reset. Public Set/Reset now includes AllowInbound in the
same durable atomic plan as DNS/routes and UI_QUIT, and GetPreferences plus
ListManagedSettings expose its signed baseline, override and lock. The existing
CLI `allow-inbound` key routes through these native methods.
`TestInboundPreferenceAtomicPatchRestartAndSelectiveReset` checks pending versus
committed values, disk restart/replay, atomic mixed commit and inbound-only reset.
`TestInboundPolicyLockRejectsWholeMixedPreferencePatch` checks all-or-nothing
lock rejection and device-policy provenance. Authenticated transport mutation
coverage, complete IPv6 coverage and OS qualification remain open; these units
are not system acceptance.

Existing test filenames above identify starting points for review, not assertions
that all listed scenarios are already covered.

Resource mutation identity preparation: `resource_identity.go` shares canonical
ID encoding with ListResources and resolves IDs against the authenticated current
recipient map. It rejects noncanonical encoding/ports, undisclosed targets,
default routes, ordinary single-IP host addresses masquerading as subnets and
applications for which the recipient is only a connector. Explicit managed
single-IP subnets remain valid. `TestResourceIdentityMatchesCatalogAndRejectsForgedTargets`
round-trips catalog IDs and tests forgery/tampering;
`TestResourceIdentityManagedSingleIPAndApplicationSource` covers subnet identity
and application role. This is preparatory validation: SetResourceEnabled,
durable operation intent/effects and overlap handling remain unimplemented.
`resource_preferences.go` now resolves a per-profile optional local choice over
the authenticated producer baseline/lock without claiming reachability. Service
ports share the producer service policy while local overrides remain per catalog
ID. `TestResourcePreferenceServicePolicyCoversPortsAndPreservesLocalChoice`
checks both ports, locked/unlocked precedence, disk persistence, absence/reset
and cloned reads. `TestResourcePreferenceFalsePresenceAndAuthenticatedSource`
checks explicit false across disk reopen and signature rejection. Public
SetResourceEnabled and complete effect qualification are still required.

Resource packet enforcement preparation: `resource_filter.go` is composed into
the optional TUN filter chain before inbound response tracking. It applies
prefix/port denials in both directions with deny precedence for overlaps,
retains old denials while tightening, and relaxes only on commit. Map expiry and
withdrawal close traffic. `TestResourceFilterDeniesOverlapsAndTransitionsInBothDirections`
checks port/protocol separation, overlap and staged changes, expiry and withdraw;
`TestResourceFilterRuleValidationAndOwnership` checks rule shape,
defensive copying and malformed packets. Public dispatch, the durable worker
and engine integration are described below; complete effects remain unqualified.
`resource_rules.go` now authenticates once and compiles local/managed choices
into bounded, deduplicated prefix/port denials for host, subnet, service and
source-role application resources. Service host matching includes the current
public key; application port targets remain port-specific. Default routes are
excluded. Stale local choices cause a compilation error and still need a
reconciliation policy. `TestResourceCompilerSubnetOverlapAndApplicationPort`
checks overlap deny precedence, application port scope and stale choice failure;
`TestResourceCompilerManagedServiceAndSignature` checks locked service ports and
tampered-map rejection. Engine integration now compiles from the supplied signed
map, stages denials before Configure, commits after success, and withdraws on
apply failure/Down. The real TUN retains the same filter object across updates;
resource-only changes are reported even without route changes. Static export
rejects compiled resource restrictions. `TestResourceEngineAppliesChangesAndRejectsStaticBypass`
exercises default/disable/enable, Changed reporting, map expiry, shutdown and
checked-export rejection with a test TUN/router. Public SetResourceEnabled,
durable resource operations and OS effect qualification remain open.

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
| US-10 preferences/policy | `service_rpc_uiquit.go`, network preference admission/worker/projection, inbound filter and router | `TestInboundPreferenceAtomicPatchRestartAndSelectiveReset`, `TestInboundPolicyLockRejectsWholeMixedPreferencePatch` and worker/filter tests described above | Remaining lifecycle keys, authenticated transport mutation and OS effects need evidence |
| US-11 resources | `service_rpc_resources.go` reads the authenticated active-profile map with bounded search and typed targets | `TestRPCResourcesAuthenticateFilterAndBindPages` covers authentication, privacy, filters, page binding and immutable source | Effective enablement, policy controls, overlap/conflict projection, runtime availability and mutation remain open |
| US-12 lifecycle | `service_rpc_uiquit.go` | `TestRPCUIQuitPreferencesAndExecution` | UI-quit support is not logoff/suspend/resume adapter implementation; audit each specified event |
| US-13 distribution/help | `service_rpc_update.go`, `service_rpc_handlers.go`; packaging and producer manifest workflows | `TestRPCUpdateInfoDoesNotInferReleaseOrPairing` | Verified update source remains absent; installation/release evidence deferred until implementation phase completes |
| US-14 presentation/privacy | Typed status/operation/log/diagnostics projections | `TestRPCObserverSnapshotExcludesPrivateState`, diagnostics suites | Audit producer reasons/actions and secret redaction; UI rendering, locale selection and assistive technologies belong outside this task |

The historical [runtime gap audit](native-runtime-gap-audit.md) reported nine
missing methods at its pinned source. The current count is four: `GetUpdateInfo`
has an explicit unavailable-source implementation and `GetSession` now performs
a backend read, `RenewSession` now has a runtime worker and transport, and
`ListExitNodes` reads authenticated live exit grants from the cached map, and
`ListResources` reads disclosed hosts/subnets/services and source-authorized
applications from that map.
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

Exit routing foundation: `exit_routes.go` derives peer routes for an explicit
recipient/peer-key-bound local selection. It authenticates the selected grant,
checks exact family/LAN authorization and deadlines, and retains default routes
only for the selected peer and address family. No selection removes defaults
from the derived input; invalid selection returns an error, never a direct-route
fallback. `TestExitRouteProjectionRequiresExactLiveSelection` covers IPv4,
IPv6, dual stack, absent intent, expired/tampered grants, changed recipient/key,
denied mode/LAN and unchanged signed source/ordinary routes. This foundation is
not yet called by the live engine/router: packet enforcement, fail-closed
platform rules, durable admission and effective-status wiring remain required.

`exit_filter.go` adds an isolated TUN packet-enforcement layer using authenticated
recipient-bound maps and exact exit selection. Every packet checks map and grant
deadlines; changes permit only the old/new intersection before commit. Invalid
updates close the filter, and withdrawal cannot be undone by a stale commit.
`TestExitPacketFilterExpiryTransitionAndWithdrawal` exercises request/reply
expiry, IPv4/dual-stack expansion and restriction, clear, tampering, malformed
packets and withdrawal. This is an additional gate, not a replacement for ACL,
application or sharing checks. Engine/TUN wiring and OS protection against traffic
bypassing the TUN remain unimplemented; these units do not prove fail-closed exit.

The `applicationTUN` wrapper now accepts an exit filter and intersects it with
existing application/sharing checks in both directions and outbound peer ACLs.
`TestExitTUNEnforcesExpiryInBothDirectionsAndRetainsACL` uses an in-memory batch
device to verify live traffic, deadline withdrawal, ordinary-route preservation,
offset/batch compaction and independent ACL denial. The live engine does not yet
construct or activate this filter: engine lifecycle, durable selection, route
application and OS fail-closed protections still require implementation.

The live engine's shared UAPI renderer and OS-route builder now apply the
no-selection route projection: neither IPv4 nor IPv6 default routes activate
solely because they appear in a map. The shared UAPI path includes initial
configuration, endpoint refresh and rollback. `TestEngineDoesNotActivateImplicitExitRoutes`
checks both endpoint modes and configured states, ordinary-route preservation
and unchanged signed source. This intentionally removes implicit exit behavior;
it does not enable explicit Select/Clear, live exit-filter activation or OS
fail-closed protection, which remain open.

Internal exit admission now journals a recipient/peer-key-bound requested
selection (or clear), previous selection, owner, profile and control origin before
any routing effect. Exact supported family/LAN pairs are intersected with signed
grant authorization and current map revision. `TestRPCExitSelectionAdmissionBindingAndRestart`
covers invalid authority/policy/support, stale maps, privacy, pending conflicts,
offline clear and replay across restart without reevaluating previously accepted
support. `TestNodeCleanupRemovesBoundExitSelection` checks node/logout cleanup.
These internal methods are not public RPC overrides: no worker or ready exit
capability exists yet, and active routing is not changed by admission alone.

Exit completion now requires an exact per-family APPLIED observation, matching
requested/effective exit and LAN settings, and fail-closed proof for enabled
families. Disabled/cleared families must have absent selection IDs. The operation,
profile/owner/origin/node tuple, previous selection and current signed grant are
rechecked before atomic completion. `TestRPCExitResultRejectsPartialOrStaleApplication`
covers valid select/offline-clear completion and restart replay, partial/missing
family evidence, lost protection, wrong LAN, unexpected IPv6, expired grants and
changed node/selection. These injected observations are not real OS evidence.
The executor, partial-failure recovery, live effective-status provider and public
Select/Clear wiring remain open; this helper alone does not implement exit apply.

Static WireGuard export also strips implicit IPv4/IPv6 default routes before
rendering, preserving ordinary peer/subnet routes and the original signed map.
`TestStaticExportDoesNotActivateImplicitExit` covers both legacy LAN-block option
values and the internal sharing-enforcement flag: neither enables exit routes or
exit LAN hooks. Static export cannot enforce grant expiry or platform fail-closed
transitions and does not implement explicit exit selection.

`service_rpc_exit_executor.go` now serializes one internal apply attempt and
persists RUNNING/retry time before dispatch. It rechecks owner, profile, recipient,
previous selection, signed grant, map revision and exact platform family/LAN pair.
Invalid queued intent fails without OS dispatch; ambiguous, cancelled or partial
dispatched effects retain the durable guard and retry the same operation ID.
`TestRPCExitExecutorDurabilityAndRevalidation` covers these cases and disk restart;
`TestRPCExitExecutorSerializesConcurrentAttempts` checks duplicate dispatch.
The adapter is injected in unit tests only. Actual OS protection/application,
containment after context changes during an attempted apply, public Select/Clear,
GetExitNode observations and background worker lifecycle remain unimplemented.
A retained operation guard is concurrency protection, not an OS fail-closed rule.

Resource catalog increment: stable opaque IDs distinguish kinds, peer/subnet
tuples and individual service ports. Default routes are excluded from ordinary
subnets; applications require the local signed source identity. Search intersects
kind filters; page tokens bind caller/profile, signed payload, canonical query
and result digest. Expired, tampered, foreign-recipient and revision-mismatched
maps are unavailable. Enabled now reports authenticated committed policy
resolution, with pending Requested kept separate. Source, policy ID and locks
come from the producer policy; local choices retain USER provenance unless
locked. Worker absence and a pending configuration change disable mutation.
The catalog and admission share the same conflict check for every nonterminal
operation, including Disconnect without a preference plan. The check runs once
per catalog read. `TestResourceMutationControlMatchesDisconnectConflict` checks
that the catalog restriction matches BUSY admission, and disappears after Down
completes while worker absence remains a separate restriction.
`TestResourceCatalogCommittedPendingAndPolicy` checks these distinctions.
Availability still explicitly reports missing runtime observations; a policy
value does not prove reachability. Catalog overlap reporting now uses the same
packet scopes as denial compilation: host/subnet addresses, service transport
and port, and application target routes. Symmetric overlap IDs are computed
before filtering and pagination; defaults never enter those scopes. Work is
bounded by 8192 scopes, one million comparisons and 32768 emitted links, with
LIMIT_EXCEEDED instead of an incomplete overlap claim. Tests
`TestResourceOverlapProtocolPortAndFamily` and
`TestResourceCatalogOverlapSurvivesKindFilter` cover transport/family separation
and a service's overlap with a filtered-out host. Deny precedence remains in
the packet filter; these links do not establish actual reachability or OS
acceptance. No resources capability is advertised by this step.

## External dependencies and approvals

Resource enforcement observation: `WireGuardEngine.TryResourceEnforcement`
authenticates the requested configuration and confirms a configured device,
exact signed payload, current committed denial rules and unexpired authority.
It returns no confirmation during Configure/Down, a staged or withdrawn filter,
changed choices, or a different map with the same revisions. The existing
`TestResourceEngineAppliesChangesAndRejectsStaticBypass` checks map and choice
bindings, engine contention, closure and expiry with a test TUN/router.
The agent now wires this observation into ListResources. A confirmed denial
projects `resource_packet_restriction_applied` for each intersecting resource;
partial intersections remain restrictions, not claims that all its traffic is
blocked. `TestResourceAppliedDenialProjectionRequiresObservation` checks a host
and overlapping service, loss of confirmation, and rejection before provider
invocation for an unauthenticated map. No confirmation is cached across reads.
The host-owned one-second resource clock now invalidates RESOURCES when the
bound enforcement observation changes, even without a Status change. It
authenticates before observation, binds map/profile and local choices, checks
the current source again before committing a revision, and joins on shutdown.
`TestResourceObservationClockInvalidatesTransitions` checks confirmation/loss,
revision and profile binding, unchanged-state suppression and invalid-source
rejection; `TestResourceObservationClockStopsWithHost` checks cancellation.
Positive availability still needs route/path/application evidence. Neither an
applied filter nor a successful operation proves reachability.

Resource mutation worker increment: private `setResourceEnabledAs` admits a
durable resource choice into the same transaction worker as network preferences.
It authenticates the active map, resolves the canonical disclosed resource,
honors managed locks and rejects conflicting pending operations. Admission does
not publish the choice. The worker applies a candidate, rechecks map, profile,
intent and previous resource choices, then commits. Failure retains the previous
choice and enters durable disconnection containment; Disconnect supersedes apply.
`TestResourceChangeAdmissionAndRestart`, `TestResourceChangeRejectsAtomically`
and `TestResourceWorkerAppliesOrContains` cover admission/replay/restart, locks,
invalid sources, apply failure and concurrent changes with injected drivers.
Public `SetResourceEnabled` now requires a live profile worker, admits the
authenticated local caller and wakes reconciliation. Resource operations emit
resource-domain invalidation on admission and state transitions.
`TestResourcePublicTransportApplyReplayAndEvents` uses the actual local native
transport and an injected driver to check authentication, missing-worker denial,
pending-to-committed false choice, terminal replay and revision-bound resource
events. Runtime reachability observations, overlap reporting and OS effect
qualification remain open; resources capability is not yet advertised.

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
