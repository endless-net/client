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

Local verification observation (2026-09-14): the resume-policy short run initially
hit Windows TCP bind access-denied errors in
`TestDNSProxyServesTCPOnTheUDPAddress` and
`TestDNSProxyPairRecoversFromTransportSpecificExclusion`. The final full short
rerun passed unchanged DNS code. Source inspection then found that a failed
ephemeral selector bind terminated before trying the other transport. Selection
now continues within the existing 16-attempt bound; explicit ports still get
one attempt. Deterministic socket units cover either selector failing,
transport-specific exclusion, full exhaustion, fixed-port failure and ownership
of all retained reservations. The separate loopback TCP DNS response/shutdown
test remains. A later source-refresh short run also exhausted this bound in
`TestServiceDiscoveryThroughRunningDNSListener`. Native `:0` selection now draws
independent candidates from the dynamic/private port range instead of relying
on sequential OS allocations through excluded ranges. Both transports must bind
the same candidate; explicit ports, the attempt bound and socket cleanup remain.
`TestDNSPairCandidateEscapesContiguousTransportExclusion` covers the range jump;
`TestDNSPairCandidatePreservesBindingAndEntropyFailures` covers explicit/IPv6
binding and incomplete entropy. These units do not prove every Windows exclusion layout has an
available pair, and host exclusion qualification remains open. No native/system
acceptance was run.

The user also accepted KEEP_INTENT defaults for user_logoff, suspend and resume
on 2026-09-14. Per-profile local preferences now persist those choices, resolve
signed account/device baselines and locks, expose matching preference/managed
catalogs, and reset selectively to policy or the accepted default. Missing
authenticated authority for an enrolled profile exposes unknown effective
behavior; rejected source or locked choices cannot partially commit a patch.
The shared network/resource worker captures all three settings across restart,
commits them only with the complete effect, and retains old settings on failure.
`service_rpc_desktop_preferences_test.go`, the worker apply/containment matrix
and native catalog checks cover these preference semantics. They do not prove
OS event execution: trusted logoff/suspend/resume adapters and CONNECT execution
for those events remain unimplemented, and platform qualification remains open.

The engine now provides a mutex-serialized suspend gate: teardown blocks a later
Configure, failed cleanup keeps the gate closed, and Resume never reapplies the
old map. Failed route removal retains the router for retry, including when the
device is already gone; Configure cannot replace that unresolved cleanup.
Windows router teardown now propagates enumeration/removal errors and retains
its pending state instead of returning success on a repeated Down. Its native
script enumerates then filters the owned interface so absent entries can be
retried without suppressing command failures. Engine and Windows command-runner
units cover these paths, not actual platform cleanup or SCM event delivery.
Windows SCM power events now feed the gate through the agent consumer described
below; other native event adapters remain open.
The internal `ApplyRuntimeLifecycleIntent` handler now resolves signed
logoff/suspend/resume policy and commits the current intent decision before
cancelling an in-flight apply. It validates the logoff owner, rejects unknown
policy without mutation, preserves newer Disconnect under KEEP_INTENT, clears
a startup-recovery checkpoint under managed DISCONNECT, and propagates that
disconnect to a pending profile-switch target. Runtime lifecycle tests cover
these decisions, restart and duplicate delivery; no public RPC operation or
completed OS teardown is fabricated. Native event delivery, worker ordering,
source-failure recovery and CONNECT behavior remain to be integrated.
The new `RuntimeLifecycleExecutor` joins those intent decisions to the engine
Suspend/Resume/Down interface and the same operation lock used by ordinary
workers. It retains that lock across suspension and failed resume; an
unconfirmed Down or unavailable policy cannot release the gate. Resume wakes
ordinary reconciliation without applying a saved map. Foreign logoff is rejected
before engine access, and Close requires cancelled worker lifetime so shutdown
cannot release live workers into an accidental reconnect. Executor units cover
failed teardown, failed resume/source, Disconnect during suspend and shutdown.
The Windows service now accepts SCM power notifications and queues copied
PBT_APMSUSPEND / PBT_APMRESUMEAUTOMATIC values to the agent executor. It ignores
the subsequent user-interaction resume notification to avoid a second lifecycle
decision. Microsoft documents the automatic wake event as occurring on each
[resume](https://learn.microsoft.com/en-us/windows/win32/power/pbt-apmresumeautomatic)
and the limited processing time for
[suspend](https://learn.microsoft.com/en-us/windows/win32/power/pbt-apmsuspend).
The bounded queue cancels the runtime on overflow instead of silently dropping
an event. The agent serializes delivery, retries an unsuccessful latest transition,
and releases the held worker lock on cancelled lifetime before worker shutdown.
Headless service mode uses the same durable mutations without requiring an IPC
listener. `windows_service_lifecycle_windows_test.go` covers scalar delivery,
ignored events, stop and overflow; `agent_runtime_lifecycle_test.go` covers
durable default intent, held gate, retry, resume wake, source loss and shutdown.
These are synthetic source/engine units. Resume now confirms teardown under
the effect lock before refreshing an expired/missing signed policy snapshot,
including when current intent is disconnected. The common snapshot fetch keeps
startup admission separate; offline mode validates local authority without
contacting control. `RefreshRuntimeLifecyclePolicy` uses a fresh full-context CAS,
validates recipient/signature/expiry/revisions/hash and commits only authority,
with domain invalidations on a changed revision. It never adopts fetched intent,
credentials or profiles. Resume resolves current intent after refresh, so a
newer user Disconnect is preserved. Fetch/validation/CAS failures retain the gate
and the consumer retries. Primitive units cover rejected authority, cancellation,
owner/intent races and idempotence; source units cover a real signed HTTP snapshot
while disconnected; executor units cover source failure and subsequent recovery
inside the closed gate. Native delivery, callback latency, effect observation,
logoff, non-Windows subscriptions and the full suspension/recovery race audit
remain open. The b4165b8 short run passed on all three OS runners.
Lifecycle transitions now publish a bounded status observation under the shared
effect lock. Confirmed teardown projects DISCONNECTED while preserving a
connected KEEP_INTENT; failed teardown projects DISCONNECTING. Resume never
projects CONNECTED before a new ordinary apply. A fresh profile/map-bound agent
snapshot clears previous path/relay/apply successes, with fixed failure keys
instead of raw source bodies and no control-plane probe. Observation failure
retains resume serialization for retry. Executor units cover confirmation and
publication ordering/failure; agent units cover stale-path replacement, failure
privacy, no probes and separation of intent from dataplane state. Full native
event/subscriber delivery, restart semantics and resources/exit effective-state
observations still require the remaining audit and platform qualification.
`runtime_lifecycle_events_test.go` now exercises the executor with the actual
mutation observation, snapshot and subscriber queues: failed teardown reports
DISCONNECTING, retry reports DISCONNECTED with connected KEEP_INTENT, and resume
cannot invent CONNECTED or an RPC operation. Owner/observer projections share
the committed revision and maintain per-stream sequence, while observer output
omits private identity/failure details. Reattachment receives a fresh stopped
snapshot; reopening durable storage in a new mutation runtime preserves intent
without replaying a previous runtime's observation as evidence of a tunnel.
Native transport/SCM timing, interruption during durable writes and complete
cross-domain invalidation coverage remain separate open evidence.
The pinned x/sys SCM host forwards SessionChange EventData after its callback
returns; the adapter must copy session data during a live native callback rather
than dereference that forwarded pointer later.
The session-owner component now binds a WTS session ID to the same SID string
used by the named-pipe peer. A newer logon invalidates the old binding before
lookup; a delayed lookup cannot restore it after replacement or logoff. Initial
enumeration uses temporary logoff tombstones, so stale enumeration cannot
resurrect a departed session. Bindings/tombstones are bounded, duplicate/unknown
logoff has no owner, and post-enumeration logoff frees its slot. Unit races cover
replacement, failed lookup, departure, late seeding, duplicate events and limits.
Windows source functions enumerate copied session IDs and retrieve SID from
[WTSQueryUserToken](https://learn.microsoft.com/en-us/windows/win32/api/wtsapi32/nf-wtsapi32-wtsqueryusertoken),
closing token handles and freeing enumeration buffers. They require the
documented LocalSystem/SE_TCB_NAME service context; that OS execution is not
locally qualified. The owner component is now attached to SCM. Safe copying
of [session notification](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-wtssession_notification)
data is implemented by the dispatcher below.
The service now uses a local SCM dispatcher instead of forwarding native data
through x/sys's asynchronous EventData field. Its HandlerEx callback validates
and copies the WTS session ID while native data is live; the queue contains no
pointer. Malformed session data or a full bounded control queue cancels the
runtime. The status pump drains shutdown after reporting failure, joins runtime
cleanup, and reports STOPPED exactly once as required by
[SetServiceStatus](https://learn.microsoft.com/en-us/windows/win32/api/winsvc/nf-winsvc-setservicestatus).
Synthetic callback/status-pump units cover copy lifetime, invalid data, overflow,
reporting failure and terminal ordering. Power and SessionChange use this
dispatcher. Startup enumerates sessions, seeds SID bindings and fails before
runtime launch if enumeration fails. Logon refreshes the binding; logoff consumes
it and sends an owner-bearing internal notification to the agent. Unknown,
duplicate and failed-lookup logoffs never infer the current profile's owner.
The consumer rechecks the current local owner before execution; mutation
admission rechecks it under its lock. Logoff retries cannot replace pending power
retries, and a foreign logoff leaves a failed suspend retry intact. Source units
cover seeded/reused sessions, unknown owners and enumeration failure; agent units
cover owner-bound DISCONNECT and foreign-logoff/power-retry interleaving.
Before resume releases the stopped gate, refreshed policy also resolves an
earlier failed owner logoff; otherwise resume remains pending. Unrelated events
do not postpone an already armed retry timer. Agent tests in
`agent_runtime_lifecycle_pending_test.go` hold teardown at a barrier and verify
that an unavailable logoff decision is committed before engine resume once
local policy becomes available; a replaced owner retains its newer intent.
An unsuccessful offline policy refresh neither resumes the engine nor wakes
reconciliation, and a later successful attempt resolves the pending logoff.
These are consumer-ordering units, not signed-map acquisition or native OS
execution evidence. A committed logoff decision is now distinguished from a
failed teardown/observation: effect failures yield to ordinary reconciliation
or resume's gated teardown and do not enqueue the original policy decision
again. `agent_runtime_lifecycle_reconnect_test.go` verifies that a newer Connect
intent survives resume after logoff's first Down failed. This does not yet
establish ordering for every unresolved-policy/user-mutation interleaving.
Power transitions also retain whether their intent decision committed: a
same-transition retry after teardown or observation failure does not overwrite
a newer intent. Opposite power events start a new decision; unresolved policy
is still retried, and resume still refreshes authority before opening the gate.
`runtime_lifecycle_power_retry_test.go` covers failed suspend teardown,
failed suspend/resume observation, newer Connect preservation and the next
power cycle applying policy again. These process-local markers do not replace
the remaining crash-recovery and native event-ordering audit.
The Windows service also retries
one unresolved WTS owner per second, using round-robin selection within the
bounded registry. Resolved or departed sessions are not queried; an in-flight
lookup is not duplicated. Every completion checks the original binding before
storing SID, so a retry cannot overwrite a newer logon or resurrect logoff.
`windows_session_retry_test.go` covers recovery, persistent-failure fairness,
bounded calls, duplicate lookup suppression and replacement/departure races.
Source-health projection, recovery of an already missed unknown-owner logoff,
native lookup latency and complete interleaving evidence,
native callback ABI, SCM execution and
non-Windows lifecycle sources remain open for further implementation/audit and
the agreed platform qualification stage.

Linux/Darwin routers now retain a cleanup plan containing only failed or
unattempted steps. Down propagates failures; Configure cannot overwrite pending
cleanup; failed initial setup also retains its cleanup obligation. Successful
steps are not repeated. A failed interface-bound removal can complete only when
interface enumeration independently confirms absence; cancellation or failed
enumeration cannot establish that postcondition. Linux route removal uses
exact prefix, device and table selectors with
[ip route flush](https://man7.org/linux/man-pages/man8/ip-route.8.html), allowing
already absent selected routes without broadening removal to other prefixes.
`router_cleanup_test.go` covers partial completion, retry, cancellation and
absence/observation failure on every host. The OS-specific cleanup suites exercise
DNS, route and interface failures and blocked reconfiguration in Linux/macOS
short CI; Windows-local checks do not execute those OS-tagged tests. Actual
platform cleanup, Linux policy-rule absence and external deletion/replacement
of macOS routes still need qualification and reconciliation evidence.

Linux policy-rule cleanup now checks the family/table/mark-filtered JSON dump
after deletion, including when the delete command failed. Only confirmed absence
finishes the step; remaining duplicates, malformed/oversized dumps, failed
observation and cancellation retain it. The parser uses iproute2's
`suppress_prefixlen` JSON attribute for the main-table suppression rule, as
defined by the upstream [rule implementation](https://github.com/iproute2/iproute2/blob/main/ip/iprule.c).
The shared runner/parser tests cover both families and selectors locally.
Native policy-rule ownership and platform effect qualification remain open.
The a980455 push short run passed on Linux; macOS verification was skipped by
the old push gate. Push unit CI now runs one short pass on Linux, Windows and
macOS, including the nested IPC module, with no installer/system jobs enabled.
The 65bf9f5 push short run completed successfully on all three OS runners,
including the OS-tagged cleanup tests. This is unit evidence, not native
installer/networking or lifecycle acceptance.

The user accepted the runtime-start default on 2026-09-14: KEEP_INTENT,
with DISCONNECT when no explicit intent is saved. The agent now initializes
that durable baseline under its lifetime lock before engine creation, RPC workers
or network sync. Existing connected/disconnected intents keep their reason and
timestamp; malformed saved intents cannot imply Connect. The short test
`TestRuntimeStartPreservesExplicitIntentAndDefaultsDisconnected` checks initial
state and restart. Runtime-start now supports local KEEP_INTENT/CONNECT/DISCONNECT,
signed account/device baselines and locks, requested/effective/source projection,
selective reset and atomic patches with UI_QUIT or network preferences. Preference
mutation changes future startup behavior, not the current connection intent.
The real agent startup resolves the committed setting before network work;
unverifiable policy holds networking down while leaving local recovery available.
CONNECT cannot supersede a nonterminal operation's saved recovery intent.
`service_rpc_runtime_start_test.go` checks policy/lock/reset/replay, source failures,
owner denial and pending Disconnect; the network worker matrix includes runtime-start
commit/rollback. Logoff/suspend/resume inputs and platform qualification remain open;
startup refresh when the authenticated cached policy is absent/expired also needs
a control-plane recovery audit. These tests do not close UF-21 or UI-AC-21.
Startup additionally rejects a missing/mismatched active profile and holds an
enrolled identity with no cached map down instead of executing local CONNECT or
restoring a connected intent. The context regression checks persisted blocking
and restart without rewriting recovery records. Automatic signed-source refresh
now has a bounded real agent preflight: when reconnect is intended and the cached
authority is unusable, it fetches a full map over the producer stream, verifies
trust/signature and adopts only map fields if identity, intent, profile/recovery
state and prior authority are unchanged. It runs before engine creation and never
applies routes. A local HTTP fixture covers signed success, tampering, cancellation
and Disconnect during fetch. A private checkpoint in the blocked intent now keeps
the original KEEP_INTENT across an offline failure and restart. Recovery binds
profile, owner, identity, credentials and local startup choice, then applies the
fresh signed policy to the original intent. Tests cover restoration, explicit
Disconnect, identity/credential changes and a new managed DISCONNECT. The running
agent now retries source recovery from the confirmed-Down branch under its shared
effect lock and existing retry/backoff loop. Map authority and resumed intent
commit together after CAS and pending-operation checks, with one revision and
catalog/preference invalidations. The retry unit matrix covers success, signature
tampering, expiry, concurrent Disconnect, pending exit work, retained enrollment
recovery and cancellation;
connected intent does not imply applied network state. Full platform and
control-plane outage/recovery qualification remain open.
Missing-map reads now preserve requested intent but report unknown
effective value/source and temporary unavailability; set/reset reject the whole
patch without modifying durable settings. The context regression checks this
projection/admission parity. Unregistered profiles retain the accepted default.
Local vet/lint and the final short run passed. A preceding short run exhausted
the DNS TCP/UDP ephemeral-port pairing attempts on Windows (bind access denied);
the unchanged listener passed on repetition. This is not evidence of platform
binding reliability or lifecycle acceptance.

Exit selection admission now persists the authenticated map payload hash.
Preflight, completion and retry reject replacement maps even when recipient,
revisions and the selected grant still match. Queued work fails without dispatch;
dispatched work retains its guard until containment succeeds, including restart.
`TestExitSelectionBindsSignedMapAcrossRestartAndApply` covers both paths using
valid signed replacement maps and an injected executor. Clear remains possible
without a map. This does not supply the missing OS exit adapter or qualify
fail-closed behavior on a platform.

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
the two methods below have no runtime overrides; generated SDK methods and CLI
commands must not be counted as their runtime implementations.

| Requirement area | Missing runtime methods | Required implementation and unit evidence |
| --- | --- | --- |
| US-05 exit | `SelectExitNode`, `ClearExitNode` | Family/LAN constraints; durable selection; partial apply/clear and fail-closed path loss; read support does not establish selection |

Additional partial implementations must not be mistaken for complete domains:

| Area | Source evidence | Remaining work |
| --- | --- | --- |
| Resources | Public `SetResourceEnabled` uses the durable worker; catalog projects policy, overlap and confirmed TUN denials, with observation events | Complete positive route/path/application observations, stale-choice reconciliation, failure/restart audit and OS effect qualification |
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
Resources/preferences now share the network-selection Stop confirmation rule:
nil error alone is insufficient when continuity is PRESERVED, UNSPECIFIED or
an unknown enum. Such results cannot commit offline preferences or finish
containment. UNKNOWN remains an accepted historical-continuity result from a
driver that confirms successful Down. `TestPreferenceAndResourceCleanupRequiresConfirmedStop`
covers both operation families and connected/disconnected candidates, durable
RUNNING containment, unchanged committed settings, disk recovery and exactly
one confirmed cleanup without repeating Apply. Persistent native cleanup errors
and automatic retry availability still need their own runtime qualification.
The profile worker now retains service availability for typed UNAVAILABLE,
DEADLINE_EXCEEDED or LIMIT_EXCEEDED returned during durable resource/preference
containment. It retries after five seconds or a wake, beginning again with
Disconnect reconciliation. It never retries an uncheckpointed candidate on this
path; unknown/permanent errors still terminate the worker for host recovery.
`TestPreferenceWorkerRetriesTemporaryCleanupWithoutReplay` exercises the actual
worker timer through a failed Apply, temporary Down and confirmed cleanup with
one Start, two Stops and the original APPLY_FAILED outcome. Classification units
exclude non-containment, raw, cancellation, permission and stale-state errors.
This is worker/unit evidence; native error mapping and platform qualification
remain open.
`service_rpc_network_preferences_public.go` projects authenticated committed
policy resolution separately from pending requested overrides, including reset
absence and managed provenance/locks. GetPreferences and ListManagedSettings
use that projection, and preference operations invalidate resources as well.
Network preference/reset and resource admission now perform authorization,
mutation validation and durable replay before rejecting an absent/stopped worker.
Only new work requires executor readiness. `TestPreferenceResourceReplayWithoutWorker`
checks pending and terminal replay after disk reopen for all three methods,
absent and cancelled workers, unauthenticated/foreign callers, changed payload
under the same request ID and new-work rejection without persistent mutation.
The rejecting admission callback cannot execute a candidate or create a plan.
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
| US-02 enrollment | `service_rpc_enrollment_worker.go`, `service_rpc_enrollment_executor.go`, credential trust included in `copyEnrollmentFields` | `TestRPCEnrollmentWorkerRecoveryAndShutdown`, `TestEnrollmentCheckpointPersistsCredentialVerificationAuthority`, `TestEnrollmentCheckpointRejectsConcurrentCredentialTrustChange`, `TestNativeEnrollmentThroughRPCHostRetainsCredentialAuthority` | Credential trust survives RPC checkpoint/disk reopen and participates in checkpoint CAS. The native host unit now exercises public create/select/enroll, the real provider against a certificate-verified synthetic HTTPS server, worker completion, credential status and request replay without repeated registration or tunnel configuration. Direct provider tests alone had bypassed this callback. Remaining approval/denial/expiry/cancellation cases and deployed backend/platform evidence remain open. |
| US-03 connection | `service_rpc_connect.go`, `service_rpc_disconnect.go` | `TestRPCConnectDurabilityAndFailure`, `TestRPCDisconnectPreemptsApplyOnlyAfterAcceptance` | Audit remaining races and recovery boundaries; unit driver results are not OS traffic evidence |
| US-04 networks/peers | Network/peer handlers; isolated target preparation and checkpoints; native registration/approval refresh; `ReconcileNetworkSelectionRegistration` validates registration readiness without activating target | Preparation/CAS/checkpoint and native-provider units; `TestNetworkRegistrationCoordinatorResumesApprovalWithoutActivation`, `TestNetworkRegistrationCoordinatorRejectsFalseSuccess`, `TestNetworkSelectionReadinessRejectsMismatchedAuthority`; peer suites | Public SelectNetwork now admits different-network operations through the live native worker, validates installation keys and control origin, and retains durable same-current no-op/replay. Worker readiness drives capability and catalog restrictions; accepted replay remains readable after worker shutdown. TestNetworkSelectionAdmissionRejectsInvalidInstallationAndOrigin verifies rejection without journal/source changes for absent or malformed keys, missing fingerprint and missing/foreign control origin. Internal coordinator resumes pending approval from disk, reuses valid ready checkpoints and rejects expired credential/map, no-save success, tampered map and concurrent source change. Readiness binds account/network/node, signatures, revisions/hash and installation keys. Internal activation now journals DownStarted before driver.Stop, excludes ordinary agent apply, revalidates source and signed target after confirmed teardown, then atomically replaces active identity/map and profile configuration. TestNetworkActivationStopsSourceBeforeAtomicTargetAdoption, TestNetworkActivationRetainsBarrierOnUncertainOrStaleStop and TestNetworkActivationResumesUncertainStopFromDisk cover ordering, context/expiry races and interrupted teardown. Activation remains RUNNING; internal ReconcileNetworkSelectionApply now journals apply/cleanup, rechecks target authority before terminal success, resumes uncertain apply through Stop and retains the barrier on cleanup failure. TestNetworkApplyCompletesOnlyCurrentTargetAndReplays, TestNetworkApplyFailureResumesCleanupWithoutReapply, TestNetworkApplyContainsContextChangeDespiteProviderSuccess and TestNetworkApplyRecoversUncertainAttemptByStoppingFirst cover injected driver outcomes, including accepted RPC Disconnect cancelling in-flight apply even when the driver returns success. Native agentRPCCleanupNetworkTarget resolves a saved uncertain registration and revokes only its node without session logout; TestNetworkTargetCleanupRevokesOnlyItsNode covers target binding, exact signed request replay, response-checkpoint failure and remote revoke failure. Backend idempotent replay/revocation qualification remains external evidence. ReconcileNetworkSelectionAbort now persists cancellation/failure, permits isolated recovery checkpoints after source changes, rejects source/active/profile node revocation, and checkpoints TargetRevoked before local teardown. TestNetworkAbortBeforePreparationPreservesSource, TestNetworkAbortCheckpointsRecoveredTargetAfterSourceChange, TestNetworkAbortPersistsRevocationBeforeRetryingLocalStop and TestNetworkAbortRejectsActiveNodeAndRetainsUncertainCleanup cover terminal/retained plans and restart ordering. Native host now starts and joins StartNetworkSelectionWorker with real catalog/registration/cleanup providers; durable dispatch advances preparation, registration, activation, apply and abort. TestNetworkWorkerResumesAllPhasesFromDisk, TestNetworkHostJoinsRegistrationOnShutdown and TestNetworkRemoteCompensationDoesNotBlockDisconnect cover orchestration and lifetime boundaries. Remote compensation no longer holds the tunnel lock. Source CAS now permits verified monotonic endpoint/peer observations while preserving network/node policy and all non-map config bindings; TestNetworkSourceRefreshDoesNotCancelIsolatedRegistration and TestNetworkSourceRefreshRejectsAuthorityChangesAndInvalidMaps cover refresh during registration and restart handover plus policy/identity/trust/intent/hash/signature/rollback rejection. TestNetworkSelectionThroughNativeTransport covers authenticated local transport admission, missing-key rejection, pending/terminal replay, payload conflict, BUSY, successful target adoption, unauthorized catalog rejection and capability removal on shutdown, using injected domain providers and tunnel driver. Dispatcher retry classification now covers every durable phase, including resumed remote compensation and local apply cleanup. TestNetworkDispatchRetriesCompensationAcrossRestart and TestNetworkDispatchRetriesApplyCleanupWithoutReapplying cover UNAVAILABLE/DEADLINE_EXCEEDED/LIMIT_EXCEEDED, retained barriers, original failure preservation and no repeated registration/apply across disk restart. Native provider units now retry a validated temporary revoke and temporary map refresh against synthetic HTTP without registering again or logging out the shared session; malformed/rejected revoke remains unconfirmed. Validated registration authorization_denied now reaches native network RPC as PERMISSION_REQUIRED. TestNetworkRegistrationDenialPreservesUncertainAttempt covers complete producer rejection versus malformed/miscorrelated responses and confirms compensation retains/replays the exact uncertain request without unbound node cleanup or shared-session logout. A rejected replay does not prove a prior uncertain registration created no node; permanent compensation recovery remains open. Complete permanent-error classification, native backend workflow qualification and full recovery/platform acceptance remain required for UF-07/UBR-10/UI-AC-14. Injected Stop units do not qualify native OS route removal or selection acceptance. |
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
uses `route -n get`, and Windows calls the pinned x/sys `GetBestInterfaceEx`
binding followed by interface lookup by the returned index. Windows no longer
starts a PowerShell process for each target. Untrusted address text is never
interpolated into a command. Subnets, scoped addresses, unsupported platforms,
lookup failures and exhausted collection budgets do not become successful route
claims. Responses remain marked partial: sampling peer host addresses is not
full routing-table or subnet/exit acceptance.

`route_observations_test.go` covers both Unix command/parser branches with an
injected executor, duplicate/invalid addresses, cancellation, target/output
limits and failure redaction. `TestWindowsNativeRouteObservation` covers the
native IPv4/IPv6 sockaddr binding, zero/error indexes, disappeared or mismatched
interfaces, invalid aliases and cancellation between native calls. The agent
test verifies that an unverified map does not invoke route collection. These
unit tests inject OS boundaries; platform execution qualification remains open.
The native lookup removes per-target process startup from the strict diagnostic
snapshot-consistency interval; it does not weaken the rejection of changed
configuration or prove all STALE_STATE failures in run 34881749817 resolved.

Snapshot intent audit: `snapshotLocked` clears provider-supplied intent and the
user-disconnected flag before projecting the durable configuration, including
when that configuration has no intent. This prevents a cleared intent from
reappearing through a stale observation. The regression
`TestRPCSnapshotDoesNotRestoreClearedIntentFromObservation` checks both owner
and observer snapshots. This proves projection precedence, not actual tunnel
shutdown or backend session revocation.

Startup policy recovery now projects the original requested intent while its
context-bound checkpoint remains valid. An unavailable/expired policy still
holds the runtime disconnected; this operational gate is not a user Disconnect.
Both native agent status and RPC snapshots use `RequestedConnectionIntent`.
No recovery hash or private checkpoint is serialized, and connection phase
remains independent. `TestRPCStartupRecoveryProjectsRequestedIntentWithoutStarting`
checks reopened state, owner/observer snapshots, opening events, immutable reads
and explicit Disconnect supersession. `TestRequestedIntentRejectsUnboundRecovery`
checks owner/profile/node/network/credential/session-token/preference changes and
invalid checkpoint reasons. Existing startup-recovery tests still require fresh
authenticated policy before restoring runtime admission. These units address
the misleading intent seen in cached-map/sharing restart CI failures; actual
traffic recovery and platform acceptance still need evidence.

Network activation resolves that same context-bound requested intent against
the still-current source, after confirmed source teardown and renewed target
authority validation. It no longer copies the source's temporary disconnected
gate and discards the saved Connect. The adopted target gets a detached intent
with no source startup checkpoint. `TestNetworkSelectionPreservesBoundRequestedIntentAcrossActivationRestart`
checks valid versus invalidated recovery and explicit Disconnect, disk restart
between activation/apply, verified target-only Start, and no repeated effects
after completion. This does not substitute requested intent for target policy
authorization or qualify the native OS/backend selection path.

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

The native HC-052/HC-053 scenario previously attempted eight subscriptions from
one identity and discarded terminal RPC errors. Run 34881749817 consequently
reported an unexplained early stream end at its opening-snapshot assertion.
`TestControlPlaneIPCEvents` now fills the four-slot identity budget, requires
typed LIMIT_EXCEEDED for a fifth stream with no snapshot, and verifies both
Disconnect and Connect reach every admitted stream after that rejection. It
then retains the cancellation, slot reuse, independent delivery and host-restart
sequence checks. Unexpected termination reports only allowlisted transport and
domain codes. The updated native scenario still requires platform CI evidence;
the service limits and authorization rules have not been relaxed.

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

Exit events now invalidate EXIT_NODE on select/clear operation admission and
state transitions. GetExitNode projects the durable containment cause while
keeping the aggregate pending and per-family enforcement unknown. The test
`TestExitEventsAndContainmentReadShareCommittedRevision` checks profile/revision
binding and the distinction between a recovery reason and observed protection.
This adds operation events, not live OS path-loss observation or enforcement.

Exit context-loss containment now has a durable phase and a separate executor
callback. Dispatched operations that lose context enter containment before the
callback; current matching identity receives disconnected intent without
overwriting an accepted Disconnect or another identity's intent. Restart resumes
containment without Apply. Terminal failure/cancellation requires bound proof
that exit routes were removed and both families remain blocked. Callback error,
partial evidence or foreign operation proof retains the guard. The test
`TestExitContainmentRequiresBoundProofAndRecovers` checks checkpoint ordering,
store reopen, failed/partial/foreign proof and original Disconnect outcome.
This is executor orchestration, not an OS containment implementation. Real
adapters, shared OS serialization and privileged-platform qualification remain
required before public Select/Clear can be enabled.

Exit intent race: admission now snapshots ConnectionIntent, and preflight and
completion both require that snapshot to match. An accepted Disconnect during
Apply prevents stale success and preserves the dispatched operation's recovery
guard. `TestExitApplyCannotCommitAfterAcceptedDisconnect` checks late Disconnect,
nonterminal outcome, preserved user reason and rejection of redispatch after
store reopen. This closes the missing intent binding; OS containment still must
be implemented before that retained guard can safely become terminal.

Exit request read: public GetExitNode now exposes the profile's durable request
and pending select/clear intent, including per-family requested IDs and LAN
choice. With no OS observation provider, effective IDs remain absent, actual
fail-closed remains unconfirmed and observation-unavailable failures are
explicit. Saved requests survive grant withdrawal as intent, never as current
authorization. `TestExitReadSeparatesDurableRequestAndUnknownEnforcement`
checks absent/saved/pending/clear states, family separation and owner access.
This does not implement public Select/Clear, OS application or positive status.

Resource restart evidence: `TestResourceWorkerRestartsApplyOrContainment`
reopens the durable store after worker cancellation or failed Down. A cancelled
worker resumes its pending apply; persisted containment performs Down without
starting the failed candidate. Apply failure retains the previous explicit
choice, Disconnect retains its reason and cancellation outcome, and request-ID
replay after recovery returns the same terminal operation. This uses injected
runtime effects and a real store reopen; it does not establish OS crash safety.

Runtime policy ownership audit: `cloneRegisterNodeResponse` previously retained
the caller's ClientPolicy pointer in engine plans and rollback snapshots. It now
deep-copies resource policy entries, setting value pointers and exit family/LAN
constraint slices using producer DTOs. The signed-map regression
`TestEngineMapClonePreservesSignedPolicyAfterCallerMutation` verifies that caller
and rollback-copy mutations cannot invalidate the applied snapshot's signature.
`TestClientPolicyCloneOwnsExitConstraints` checks nested exit constraints and
absent policy preservation. This fixes memory ownership, not exit OS acceptance.

Resource read cancellation: overlap and applied-denial projection now check
request cancellation during iteration under the RPC read lock. Applied-denial
comparison is capped at one million with typed LIMIT_EXCEEDED, matching the
existing overlap work bound; no partial catalog is returned on either failure.
`TestResourceProjectionCancellationDuringObservation` checks cancellation by
the observation provider, cancellation before either calculation, and unchanged
durable revision. This does not qualify OS networking.

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
