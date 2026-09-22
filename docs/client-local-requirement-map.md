# Local v0 requirements: producer implementation and unit audit

Status: open. This inventory complements the [headless BR/AC/RULE/IT
matrix](client-headless-requirement-map.md); neither document accepts a feature.
Every row requires assertion-level review, implemented effects and later evidence.
The tables identify implementation and unit entry points, not passing coverage
by filename. The [runtime gaps](client-runtime-implementation-gaps.md) remain
open, including methods absent from the runtime and unavailable providers.

Sources read on 2026-09-14 with explicit user permission, without modifying
either external repository:

- Architecture BA `docs/ru/client-ui-business-analysis.md` at
  `bdb5ba63c0e5356122c0760f4d63205e84ef507d`: UF-01–23, UBR-01–40,
  UR-01–13 and UI-AC-01–27.
- Architecture headless SA at the same revision, especially C-01 and its
  explicit reference to the consumer SA.
- Client UI `docs/client-ui-system-analysis.md` at
  `a8df8d4aeb269cb1c7e9b73f1cdfc95565f3a8c2`: US-01–14 and producer obligations.
- Producer `clientapi/v1/resource_policy.go` and `client_policy.go` at the
  approved pin `v1.12.1-0.20260913120316-e4fb0a95d2af`, read with permission.

These are source revisions, not a proposed version increase or acceptance pin.
The source documents retain normative wording; this is client traceability.

## Implementation and unit paths

Every requirement below joins through its US IDs to this implementation/unit
table. Paths are relative to `internal/client/` except where stated. Multiple US
IDs mean all the referenced implementation and unit obligations apply.

| US | Client implementation entry points | Unit review entry points | Client work still open |
| --- | --- | --- | --- |
| US-01 | `service_rpc_host.go`, `service_rpc_capabilities.go`, `clientipc/local`, `clientipc/rpc` | `service_rpc_host_capabilities_test.go`, `service_rpc_capabilities_test.go`, `clientipc/rpc/protocol_test.go` | Platform/capability/identity failure audit; supported platform transport qualification |
| US-02 | `service_rpc_enrollment_worker.go`, `service_rpc_enrollment_executor.go`, CLI approval/registration drivers | `service_rpc_enrollment_worker_test.go`, `cmd/endlessnet-client/service_rpc_approval_driver_test.go`, `cmd/endlessnet-client/enrollment_workflow_test.go` | Every approval/denial/cancel/expiry/ownership and durable request binding |
| US-03 | `service_rpc_connect.go`, `service_rpc_disconnect.go`, `service_rpc.go`, events | `service_rpc_connect_test.go`, `service_rpc_disconnect_test.go`, `service_rpc_restart_test.go`, `service_rpc_events_test.go` | All race/restart/failure outcomes; real apply/Down and first-snapshot continuity |
| US-04 | `service_rpc_networks.go`, `service_rpc_select_network.go`, `service_rpc_peers.go` | `service_rpc_peers_test.go`, `service_rpc_select_network_test.go` | Cross-network selection still missing; identity/map/routes isolation and recovery |
| US-05 | Public exit handlers, durable protection, Linux native executor/host/worker, route/filter/guard readback, pre-exit DNS and release recovery; LAN source/policy preparation | Exit catalog/change/result/executor units, release/protection/host/resume and route/filter/guard observations; LAN assertions below | LAN datapath, full transition/recovery audit, physical classification and actual per-family OS qualification |
| US-06 | `service_rpc_trust_worker.go`, `service_rpc_trust_recovery.go`, recovery helper | `service_rpc_trust_worker_test.go`, `cmd/endlessnet-client/service_rpc_trust_recovery_test.go` | Exact origin/key/announcement, replay/cancellation/privilege audit and real helper execution |
| US-07 | `service_rpc_diagnostics.go`, bundle create/store/worker/read, `route_observations.go` | `service_rpc_bundle_read_test.go`, `service_rpc_bundle_admin_test.go`, `service_rpc_bundle_archive_test.go`, diagnostics tests | Every bound/privacy/preview/export case; complete route/resource observations |
| US-08 | Profile/switch/logout/forget handlers and workers | `service_rpc_profiles_test.go`, `service_rpc_switch_test.go`, `service_rpc_logout_worker_test.go`, `service_rpc_forget_test.go` | Cleanup and stale-response audit; actual provider isolation; remote cleanup evidence |
| US-09 | Session read/renewal/executor/worker and CLI transport | `service_rpc_session_test.go`, `service_rpc_session_clock_test.go`, `service_rpc_session_concurrency_test.go`, `service_rpc_session_cancel_test.go`, `service_rpc_session_worker_test.go` | All owner/profile/cancellation races; browser origins; producer behavior and real continuity |
| US-10 | Preference admission/worker/projection, `network_preferences.go`, `inbound_filter.go`, TUN and WireGuard engine | Worker tests plus `TestInboundPreferenceAtomicPatchRestartAndSelectiveReset` and `TestInboundPolicyLockRejectsWholeMixedPreferencePatch`: mixed atomic commit, disk replay, selective reset and signed lock rejection | Authenticated transport mutation and OS effect observations remain open; other lifecycle keys and full policy/atomic-patch coverage are incomplete |
| US-11 | Resource catalog, SetResourceEnabled admission and shared preference worker, policy/filter enforcement, `resource_reconciliation.go` | Resource public/worker/filter/overlap units, `resource_reconciliation_test.go`, CLI `resource_map_cache_test.go` | Positive route/path/application observations and invalidation; complete apply/rollback/restart assertion audit and native effects |
| US-12 | `service_rpc_uiquit.go`, `service_rpc_runtime_start.go`, signed lifecycle resolver, agent startup before workers; per-profile desktop preferences; `windows_service_windows.go` power queue → `agent_runtime_lifecycle.go` → shared engine gate | UI-quit/events; runtime-start source/lock/reset/replay; no implicit startup connection; desktop policy/default/reset/restart and shared atomic worker; `windows_service_lifecycle_windows_test.go` delivery/overflow, `agent_runtime_lifecycle_test.go` gate/retry/wake/source loss/shutdown; runtime-policy refresh CAS/authority and source/executor recovery units; lifecycle observation ordering, failure, snapshot/privacy/no-probe units; `runtime_lifecycle_events_test.go` owner/observer streams, reattach and reopened-runtime evidence; `windows_session_owners_test.go` owner binding, stale lookup, enumeration/logoff races and bounds; dispatcher units verify callback copying, overflow and terminal status ordering | Non-Windows event sources, logoff source-health recovery, CONNECT execution, full native/subscriber observation audit, complete recovery-race audit and native lifecycle qualification |
| US-13 | `service_rpc_update.go`, support handler, `packaging/`, client release workflows | `service_rpc_update_test.go`, `service_rpc_host_capabilities_test.go`; later packaging tests | Approved signed update source, support destinations, platform artifact/state/pairing audit |
| US-14 | Typed status/reasons, snapshot/events/log/diagnostics projections | `service_rpc_events_role_test.go`, `service_rpc_session_snapshot_test.go`, diagnostics/privacy tests | Exact observer/private field audit, actionable reasons and full capability distinctions |

## Assertion audit increment, 2026-09-21

Ownership audit (2026-09-22): `TestExitLANOwnershipCanonicalFamilyAndJSON`
and `TestExitLANOwnershipRejectsMalformedIdentity` validate bounded persistent
identities, including separate map/program ID namespaces.
`TestExitLANBPFDescribeOwnershipUsesHeldObjectsAndRevokes` and
`TestExitLANBPFDescribeOwnershipFailsWithoutReceipt` tie the description to
held kernel objects and reject interrupted or changed observations.
`TestExitLANOwnershipCheckpointPersistsBothScopesAndClones` checks the durable
checkpoint and clone isolation; admission tests reject stale expected state.
`TestExitClearCannotReleaseRetainedLANManifest` ensures cleanup cannot discard
an outstanding manifest. Native recovery and confirmed pin deletion remain open.

Publication audit (2026-09-22):
`TestExitLANBPFPublicationRequiresLiveSelectedHooks` checks mandatory pre/post
hook observations for all family modes. `TestExitLANBPFPublicationObservationFailureRevokes`
checks failed refresh revocation, including post-swap failure and expiry during
observation; `TestExitLANBPFPublicationSerializesClose` checks the publication
lock retains descriptors through readback. The observation tests reject
duplicate/foreign held objects, stale metadata, detached hooks and cancellation.
These assertions do not establish persistent pin ownership or native acceptance.

`TestExitLANBPFLinksUseOnlySelectedFamilies` verifies single-family success
when the other family cannot attach, failure of a selected family, and no
dual-stack downgrade. `TestExitLANBPFPinsFollowFamilyIdentity` checks stable
family names, the absence of unselected pins, malformed selected sets and old
unselected pin conflicts without deletion. The plan assertion rejects topology
captured for IPv4-only when dual-stack was requested. These remain preparation
assertions; native requested/effective LAN effects still need integration.

`TestExitLANSourceSelectedFamilyIgnoresOtherFamily` and
`TestExitLANSourceSelectedFamilyErrorsFailClosed` separate unused-family
payloads from selected-family failures. `TestExitLANSourceFamilyFilteredAddressAbsence`
checks that an omitted address row cannot grant routes;
`TestExitLANSourceRejectsSelectedAddressLifetime` rejects invalid selected
lifetimes. `TestExitLANHealthRejectsDifferentTopologyFamily` rejects reuse
before peer inspection, and watched capture rejects a mismatched source family.

Hook dump assertions: `TestExitLANHookDumpExactIdentityAndCompletion` checks
LE/BE requests and exact BPF evidence alongside ordinary/nft hooks, waiting for
DONE. `TestExitLANHookDumpRejectsIncompleteAndDuplicateEvidence`,
`TestExitLANHookDumpRejectsMalformedAndLostData` and
`TestExitLANHookDumpWorkBounds` reject incomplete, duplicate, interrupted,
malformed and over-budget observations. Linux collector units inject transport
for successful completion, missing DONE, sender/truncation/receive failures,
pre/late cancellation, blocked receive and setup cleanup. Native sender tests
separately reject nonkernel PID, groups and wrong sockaddr family. These units
do not prove real socket/poller behavior or atomic two-family attachment.

`TestExitLANBPFPinsRetainExactObjectsAfterClose` checks FD-relative pin/get
encoding, both byte orders, reopened identity and pin references after Close
using a fake kernel. `TestExitLANBPFPinFailurePreservesClosedPartialOwnership`
injects syscall failure and late cancellation at each of 21 steps.
`TestExitLANBPFPinConflictAndReadbackMismatch` checks foreign pin conflicts,
swapped readback and final directory invalidation without deleting pins.
`TestExitLANBPFPinsRejectUnsafeNamesAndDirectory` checks path traversal, NUL,
oversize names and closed directory rejection. Native bpffs and live hook
qualification are still absent; pins alone are not LAN authority.
Linux-only directory tests (`TestExitLANBPFDirectoryAcquireAndValidate`,
`TestExitLANBPFDirectoryRejectsUntrustedPaths`,
`TestExitLANBPFDirectoryFailureAndCancellationCleanup`,
`TestExitLANBPFDirectoryReceiptInvalidation`) inject filesystem/lock calls.
They check nofollow opens, root ownership and modes, bpffs identity, missing
directory/EEXIST race, lock contention, all call failures and late cancellation,
and path replacement after acquisition. They do not operate on the CI host's
bpffs or prove native flock support.

`TestExitLANBPFLinksAttachClosedAndRetainIdentity` checks lease withdrawal,
both family bindings, signed priority, duplicate rejection and owned-FD cleanup
in LE and BE fake UAPI calls. `TestExitLANBPFLinkFailureAndCancellationCleanup`
covers syscall failure and late cancellation at each of six attachment steps;
`TestExitLANBPFLinkRejectsMismatchedReadback` rejects each corrupted metadata
field and short info. `TestExitLANBPFLinkRejectsUnsafeInputs` checks reserved
priorities, unknown hook and pre-cancellation. None proves native attachment,
pin lifetime or live-hook membership; those OS assertions remain open.
`TestExitLANBPFLinksRejectDuplicateAndForeignProgramIdentity` rejects duplicate
link IDs, a different program type, zero program ID and truncated program info.

LAN BPF units: `TestExitLANBPFProgramDeadline` and
`TestExitLANBPFProgramDoesNotRenewDeadline` interpret the emitted LE/BE
instructions for missing, expired, exact-boundary and full-width deadlines.
`TestExitLANBPFClosedPreparationAndImmutablePublication` verifies the closed
initial slot, frozen inner publication and FD ownership via a fake syscall.
`TestExitLANBPFPreparationFailureClosesOnlyOwnedFDs`,
`TestExitLANBPFPublicationFailureAndLateInvalidation`,
`TestExitLANBPFFailedRefreshRevokesPreviousLease` and
`TestExitLANBPFPublicationCancellationDuringContention` cover failed creation,
late cancellation/expiry, withdrawal of previous leases, propagation of failed
withdrawal and cancellation behind another publisher. Linux-only units compare
constants with pinned x/sys and check pre-cancellation before native loading.
These are not evidence of verifier acceptance, hook attachment or native LAN
packet enforcement; US-07 and LAN-related requirements remain incomplete.

`TestExitLANBootDeadlineIsAbsoluteAcrossDelaySuspendAndClockChanges` checks
immutable absolute expiry across delayed publication, suspend and realtime
changes. `TestExitLANBootDeadlineRejectsInvalidArithmetic` checks saturation,
overflow, inverted samples and expiry during capture. Linux sampler units
inject all three clock reads, including each read failure and late cancellation;
they do not read the host clocks or prove kernel packet enforcement.
`TestExitLANBootPreparationRechecksAfterTopology` checks expiry/cancellation at
the final clock sample after topology readback.
`TestExitLANBTFMarkLayout`, `TestExitLANBTFRejectsUnsafeLayouts`,
`TestExitLANBTFTraversalBoundsAndQualifiedPointer`, `TestExitLANBTFHeaderAndBounds`
and `TestExitLANBTFSkipsKnownRecordsAndPromotesOnlyAnonymous` use synthetic LE/BE
metadata to verify target offsets and rejection boundaries. They do not prove
that a generated BPF program loads or enforces traffic on a real kernel.
`TestExitLANBTFUnnamedPaddingBitfield` accepts legal nonoverlapping padding and
rejects padding that overlaps the target field.

HOST receipt lifetime assertions: `TestResourceHostCurrentRechecksDeadlineAfterReadback`
crosses the original expiry during inspection;
`TestResourceHostCurrentRechecksRelayAfterReadback` ends the original relay
generation inside the same read;
`TestResourceHostCollectorRejectsLateReadbackExpiryAndCancellation` checks final
inspection success, failure, cancellation and expiry. Synthetic timestamps and
injected route commands establish these races, not native resource reachability.
`TestResourceHostCurrentCannotCrossCommittedExitGrantDeadline` keeps map and
receipt alive while final readback crosses the earlier signed grant deadline.

LAN semantic question is resolved by the user's 2026-09-21 decision and
architecture D-034 (`8719ce7`). The following preparation does not close US-05,
UF-08, UI-AC-17 or native acceptance:

- `TestExitLANWatchCaptureOwnershipAndInvalidation` verifies subscription before
  snapshot, failure cleanup, cancellation/loss and irreversible shared receipt
  invalidation. `TestExitLANPreparationRejectsTopologyLossInsideReadback` closes
  the receipt inside the last UAPI readback and checks that no plan survives;
  cloned plans cannot be revived by caller replacement. These assertions do not
  establish kernel event delivery latency or packet-time interface binding.
- `TestExitLANRouteDatagramKernelChanges` recognizes the subscribed event
  families and batches; `TestExitLANRouteDatagramRejectsLossAndMalformedEvidence`
  rejects foreign senders, truncation, overrun, malformed lengths and control
  messages. These are parser units, not native socket shutdown qualification.

- `TestExitLANHealthCannotRenewAnOldHandshake` rejects renewal by repeated reads
  and configuration-only health. Context/path/device/relay changes and incomplete
  handshakes are rejected by `TestExitLANHealthRejectsChangedContextPathAndIncompleteEvidence`.
- `TestExitLANPreparationIntersectsHealthTopologyAndPolicy` caps preparation by
  the earliest topology, handshake and direct-check deadline;
  `TestExitLANReadbackCannotCrossEvidenceDeadline` expires topology or the original
  receipt inside the final inspection. Synthetic timestamps verify logic only;
  production inspection still requires actual authenticated UAPI evidence.
- `TestExitLANSourceTimedIPv6RAPreferences` preserves all three router
  preferences and caps expiry across countdown snapshots;
  `TestExitLANSourceRejectsInvalidOrChangedRARouteMetadata` rejects missing,
  malformed or changed authority, including overlapping indirect routes.

- `TestExitLANSourceStablePhysicalTopology` and
  `TestExitLANSourcePublicWiFiAndLifetimeCountdown` preserve actual prefix lengths,
  two matching snapshots and the earliest finite address deadline. Injected
  sysfs/IP results do not qualify physical hardware or prove instance continuity.
- `TestExitLANSourceRejectsAmbiguousAndChangingEvidence` rejects gateway/via,
  next-hop IDs, changed carrier/routes, cancellation, malformed/bounded data and
  foreign preferred source. Overlapping physical attachments remain ambiguous
  regardless of route metric.
- `TestExitLANReservationRequiresSignedAuthorityAndPreservesResources` checks
  exact grant/map authority, disabled resource and sticky reservations, address
  expiry, nonempty selected families and immutable preparation output.
- `TestExitLANDestinationSubtractionIsExactAcrossFamilies` enumerates every
  address in bounded IPv4/IPv6 fixtures, including /31 and /32, to reject both
  scope widening and lost permitted destinations. This is CIDR arithmetic, not
  OS packet-path evidence. Native Apply still advertises BLOCK only.

The following reviewed assertions refine US-05/08/11 without closing their OS
or acceptance obligations. They cover committed source `ecd7266`:

| Scope | Assertion inspected | Evidence boundary |
| --- | --- | --- |
| US-05 clear recovery | `TestExitClearReleaseCheckpointSurvivesRestart` reopens disk state inside Release, asserts the shared effect lock, then retries only Release after error, partial result or cancellation; success removes journal/selection | Injected callbacks; native route/firewall release still requires its adapter and qualification |
| US-05/08 retained protection | `TestExitReleaseLateContextRetainsOwnershipForExplicitClear` changes node context during Release, reopens terminal ownership, restores blocking, rejects protected-profile remove/forget and clears the original inactive profile without replacing the current identity | Durable ownership and admission verified; startup uses an injected guard runner |
| US-11 map retirement | `TestResourceChoicesRetireAndReturnWithPolicyAfterRestart` retains old packet denials during suspend/withdraw, reopens disk choices and restores returning intent subject to the new policy lock | Packet-filter unit evidence; does not establish positive resource reachability |
| US-11 stale operation | `TestResourceChoiceRetirementContainsPendingOperationAfterRestart` preserves the accepted journal through map change, reopens it and asserts one Stop, terminal STALE_STATE and preserved retired intent | Shared worker recovery with injected driver; native Stop remains separate evidence |
| US-11 rejected authority | `TestResourceChoiceMapReconciliationRejectsAtomically` rejects tampering, unsigned/foreign maps, revision conflicts and malformed choices with unchanged configuration | Local map-validation boundary; producer delivery and real flows remain open |

The implementation entry points now also include `service_rpc_exit_protection`
state in the exit change/executor/result paths and `resource_reconciliation.go`
called by CLI `cacheNetworkMapChecked`. The older row entry-point lists above
are discovery aids, not assertions that these paths are absent.

## Exit read-model assertion increment, 2026-09-21

These additional assertions do not close US-05 or native qualification:

| Scope | Assertion inspected | Evidence boundary |
| --- | --- | --- |
| US-05 after Clear | `TestNativeExitClearedObservationRechecksReleasedScopeAndOrdinaryRuntime` executes durable Clear, reads fresh native state, configures an ordinary userspace runtime, and rejects changed scope, firewall, routes, rules or UAPI; restart and operation pruning retain only the observation address | Real userspace WireGuard with channel TUN and injected OS commands; the stored scope never substitutes for fresh native readback |
| US-05 selectable modes | `TestExitReadProjectionTracksWorkerAndAdmission` compares signed catalog modes with actual admission; `TestExitCatalogModesNeverWidenPairs` rejects widening diagonal family/LAN support into Cartesian combinations | Trusted worker modes and signed unit map; host readiness remains unadvertised |
| US-05 change events | `TestExitObservationEventsInvalidateOnlyNormalizedTransitions` and `TestExitObservationEventsPublishInitialConfirmedAndAfterPending` assert first confirmed evidence, loss/recovery, no repeated metadata noise and fresh Get independent of the event digest | Injected native observer, durable revision and subscriber queue; no OS atomic observation guarantee |
| US-05 worker lifetime | `TestExitWorkerReadinessRebootstrapsStreamsWithoutCapability` asserts STALE_STATE at raw worker install and stop; `TestExitReadControlRechecksWorkerAfterObservation` rejects the former observer after replacement; `TestNativeExitRuntimeCapabilityOwnedByPublicWorker` binds readiness only to the public native adapter token | Unit worker lifecycle; capability is support metadata, not native traffic qualification |
| US-05 host lifetime | `TestAgentExitSupervisorCancelsAgentBeforeJoiningSibling`, `TestRPCHostReportsFailureBeforeWaitingForRuntimeOwner` and Linux-only `TestAgentNativeExitHostRunsWithoutIPCAndJoinsLockedWorker` cover external cancellation before drain and endpointless runtime shutdown | Controlled workers and an unconfigured real engine; no privileged interface or routing commands are exercised |
| US-05 durable clear scope | `TestExitClearedScopeRequiresSuccessfulFinalCommit` rejects a contained checkpoint as completed Clear, then atomically records scope without credentials; `TestInactiveProfileClearPersistsOriginalArtifactsAndCurrentObservationIdentity` reopens old artifact addresses separately from current identity | Durable store and injected OS readback; foreign scope is rejected, not cleaned |
| US-05 ordinary runtime Clear | `TestNativeExitClearAdoptsOnlyOwnedOrdinaryRuntime` verifies Configure→Clear, rejects foreign owner/profile with the same node without native commands, retries ambiguous containment and preserves identity through device-recreation rollback | Userspace WireGuard/channel TUN and injected router/native commands; no OS traffic qualification |
| US-05 changed interface recovery | `TestNativeExitClearRecoversOriginalInterfaceAndTable` checks original interface/table after restart, ambiguous release retry after another interface change and refusal to stop foreign live runtime; `TestRPCExitExecutorDurabilityAndRevalidation` retains strict Select admission | Injected native commands and durable store; Clear does not authorize applying the old selection on a replacement interface |
| US-05 admission | `TestExitSelectionRejectsUnexecutableRouteScopeBeforeJournal` and `TestExitClearRejectsUnrecoverableIdentityAndRouteBeforeJournal` reject invalid scope without changing state; `TestExitClearUsesRetainedIdentityWithoutCurrentEnrollmentAndPreservesReplay` keeps original cleanup scope and accepted replay | Admission and durable state assertions; native effects are tested separately |
| US-10/12 startup traffic | `TestCmdAgentWithoutSavedIntentDoesNotContactControlPlane` repeats startup without control requests; `TestStartupPolicyFetchRequiresRequestedConnection` distinguishes connected/recovery/explicit CONNECT from missing, blocked and stale intent; `TestRPCBackgroundObservationControlAdmission` suppresses background probes while disconnected/offline and preserves connected probes | Local HTTP fixtures and durable intent; protected probe transport still needs a separate audit |
| US-03/05 control transport | `TestRPCControlProbeUsesOwnedTransport`, `TestRPCControlProbeNeverFallsBackFromUnavailableTransport` and `TestRPCControlProbeRejectsIncompleteResponse` require the engine transport, no cookies/credentials/redirect fallback, bounded requests, body/idle cleanup and rejection of late cancellation | Injected transport and local HTTP; protected DNS/socket behavior still requires native acceptance |
| US-03/05 Disconnect notification | `TestRPCOfflineNotificationRequiresOwnedTransport` requires the runtime's transport after Down, rejects absent/refused transport and late cancellation, closes response/idle connections and preserves local Stop success | Injected transport and local server; no external control or native socket acceptance |
| US-06/11 cancelled candidate | `TestResourceWorkerRestartsApplyOrContainment` covers cancellation during apply and immediately before durable commit, bounded independent Stop and retry after store reopen; `TestNetworkPreferenceWorkerApplyAndContainment` retains the journal after shutdown cleanup | Injected driver and durable store; cancellation never proves that a candidate had no native effects |
| US-05/06/11 retained exit candidate | `TestNativeExitPreferenceWorkerCommitsActualGuardedRuntime` exercises worker→native reconfigure→readback→durable completion→maintenance; `TestNativeExitPreferenceRejectsUnboundCandidates` rejects wrong candidate, operation, barrier, intent, runtime and lock; `TestNativeExitPreferenceLateFailureContainsAndKeepsJournal` checks cancellation, changed intent and native failure | Real userspace engine with injected OS evidence; no native platform acceptance or general profile/network transition authorization |
| US-05/11 resource choice with exit | `TestNativeExitResourceAdmissionAppliesAndRestoresPacketChoice` accepts disable/enable commands, checks admission has no packet effect, observes applied denial/removal, preserves exit guard/selection and confirms terminal replay and maintenance | Real userspace resource filter with synthetic packets and injected OS evidence; not native end-to-end reachability |
| US-02/03/04/06/11 worker lifetime | `TestPreferenceWorkerLockCancellationPreservesRetry` checks both serialization and effect-lock waits, partial-lock release, unchanged journal and successful retry; `TestProfileWorkerShutdownWhileEffectLockHeld` proves the worker entered Disconnect before cancellation and joins without releasing the external effect lock | Real mutexes/worker goroutines and durable operations; effects are injected |
| US-02/03/09 provider lifetime | `TestRPCProviderWorkersCancelContendedAcquisition` checks all seven provider/coordinator lock paths with no callbacks or durable changes; `TestRPCEnrollmentLockCancellationPreservesDurableRestart` reopens and completes the original accepted enrollment after cancelled acquisition | Real store and mutexes, synthetic providers; remote provider cancellation after dispatch is a separate requirement |
| US-11 HOST native observation | `TestResourceHostCollectorBindsNativeRulesRoutesAndPeerPath` distinguishes confirmed route/path from missing routes, UID routing, replaced interfaces and foreign owner; route/path/filter assertions cover source binding, freshness and policy restrictions | Real in-memory engine security UAPI with injected native outputs and captured handshake time; relay/subnet/service/application confirmation and actual OS traffic remain open |
| US-11 HOST publication | `TestResourceNativeObservationRunsOutsideMutationLockAndRechecksScope` covers unauthorized/busy, late owner/map/worker changes, expiry and cancellation; `TestResourceHostProjectionAndEventsRequireFreshEvidence` checks HOST-only availability, path loss/restoration invalidations, no repeated event and denial precedence | Synthetic opaque proof for service boundaries; native observation has separate tests, no end-to-end availability claim |

## External dependency register

Owner labels describe responsibilities, not authorization to change repositories.
They apply to every row referencing the label. Missing evidence is never a
successful unsupported implementation.

| Key | Owner | Required result | Affected scope |
| --- | --- | --- | --- |
| P | Backend services and clientapi owners | Producer-backed enrollment, registration, map, policy, renewal, revocation, replay and cleanup behavior; client consumes pinned DTO/verification only | US-02–06, US-08–11; corresponding headless IT rows |
| O | Client platform adapters; OS provider where external | Client must implement real supported OS effects and later demonstrate protection, traffic, lifecycle and peer authentication. This is primarily client work, not an excuse for missing adapters | US-01, US-03–13 |
| U | Client UI owner | Consumer presentation, locale/accessibility, clipboard, shell/autostart, notification and operation recovery evidence using the producer contract | All US rows; producer obligations remain here |
| D | Distribution/product owners and client packaging owner | Approved signed discovery source, signing trust, channel/platform/pairing/expiry rules and trusted support destinations; client verifies source and owns core packages; UI installer belongs to its owner | US-01/13, UI-AC-10/11/25 |
| A | User and acceptance owners | After complete implementation/unit audit, agree how to run integration/platform tests while working on main without creating an unauthorized PR; same-artifact real resource evidence | All acceptance rows |

## Functional map

| Requirement | Client obligation / scope | Implementation → unit | Dependency |
| --- | --- | --- | --- |
| UF-01 | Installed core identity and platform packages | US-01/13 | D/O/U/A |
| UF-02 | Supply runtime state to shell; UI launch/tray implementation external | US-01/14 | U/O |
| UF-03 | Profiles, enrollment and authoritative approval | US-02/08 | P/U |
| UF-04 | Snapshot, phase, operation and events | US-01/03/14 | O/U |
| UF-05 | Durable connect/disconnect and confirmed result | US-03 | O/P |
| UF-06 | Authorized peers, path health and revision | US-04 | P/O |
| UF-07 | Real authorized network selection | US-04/08 | P/O |
| UF-08 | Exit catalog, selection, clear, LAN/families and fail-closed | US-05 | P/O |
| UF-09 | Separate session/credential/recovery outcomes | US-02/06/09 | P/U |
| UF-10 | Exact trust confirmation with bounded privilege | US-06 | P/O/U |
| UF-11 | Redacted bounded diagnostics and bundles | US-07 | O/U |
| UF-12 | Remote logout versus privileged local forget | US-08 | P/O |
| UF-13 | Core package recovery/removal and installed identity | US-13 | D/O/U/A |
| UF-14 | Typed localizable reasons; UI locale/accessibility external | US-14 | U |
| UF-15 | Privacy-safe changes and independent deadlines | US-09/14 | U/P |
| UF-16 | Profiles and one active tunnel context | US-08 | P/O |
| UF-17 | Session renewal with observed continuity | US-09 | P/O |
| UF-18 | Inbound/DNS/routes requested/effective and set/reset | US-10 | P/O |
| UF-19 | Authorized resource search, availability, overlap and enable | US-04/11 | P/O |
| UF-20 | Effective managed settings, source, lock and next actor | US-10/11 | P/U |
| UF-21 | Runtime intent separate from UI and OS lifecycle | US-12/10 | O/U |
| UF-22 | Verified update and compatibility projection | US-13 | D |
| UF-23 | Exact core build and trusted support/offline-help identity | US-13/14 | D/U |

## Business requirements

| Requirement | Client obligation / unit audit focus | Implementation → unit | Dependency |
| --- | --- | --- | --- |
| UBR-01 | Core package provenance and integrity, installed identity | US-01/13 | D/O/A |
| UBR-02 | Normal owner calls without permanent admin; bounded helper | US-01/06/08 | O/U |
| UBR-03 | All public state/actions through v0; retire old IPC | US-01/03 | U |
| UBR-04 | Distinct authoritative enrollment/connection/recovery states | US-01/02/03/06 | U/O |
| UBR-05 | Non-success reasons and authorized next actor | US-03/06/14 | U/P |
| UBR-06 | No secrets in errors/logs/diagnostics/browser projection | US-02/07/14 | U |
| UBR-07 | Browser completion cannot substitute for producer approval | US-02/09 | P/U |
| UBR-08 | Idempotent connection operations distinct from logout/revoke | US-03/08 | P/O |
| UBR-09 | Distinct account/network/device identity projections | US-04/08 | P/U |
| UBR-10 | Select only authorized networks, no union of access | US-04 | P/O |
| UBR-11 | Block automatic trust, confirm exact authority tuple | US-06 | P/U |
| UBR-12 | Cancellation/denial leaves trust and connection unchanged | US-06 | O/U |
| UBR-13 | Redaction, size/time limits and honest diagnostics preview | US-07 | U/O |
| UBR-14 | Authoritative IPC snapshots; private state remains protected | US-01/03/14 | U/O |
| UBR-15 | Remote-confirmed, unconfirmed and local-forget results | US-08 | P/U |
| UBR-16 | Immutable pairing metadata and core package downgrade policy | US-13 | D/U/A |
| UBR-17 | Repair/upgrade preserve enrollment; removal intent separate | US-08/13 | D/O/A |
| UBR-18 | Commands accessible through IPC; primary UI access external | US-01/03/14 | U |
| UBR-19 | Typed reasons support localization; assistive UI external | US-14 | U |
| UBR-20 | Private filtered events/deadlines; notification controls external | US-09/14 | U |
| UBR-21 | Reject incompatible IPC before command; verified guidance | US-01/13 | D/U |
| UBR-22 | Every mutation enforces owner, approval, policy, entitlements | US-02/04/05/06/08/09/10/11 | P/O |
| UBR-23 | Immutable profile identity/origin, one active context | US-08/04 | P/O |
| UBR-24 | Session/credential expiration and renewal states separate | US-09 | P/U |
| UBR-25 | Never infer renewal continuity from successful response alone | US-09 | P/O/A |
| UBR-26 | Runtime startup, UI launch, quit and sleep independent | US-10/12 | O/U |
| UBR-27 | Effective policy provenance/lock and prohibited mutation | US-10/11 | P/U |
| UBR-28 | Preferences restrict signed authority; expose actual effect | US-10 | P/O |
| UBR-29 | Resource/peer disclosure, search and local enable distinction | US-04/11 | P/O |
| UBR-30 | Ordinary/security/mandatory update versus incompatible pairing | US-13 | D/U |
| UBR-31 | Bounded duration/content preview; no implicit capture/upload | US-07 | U |
| UBR-32 | Exact build and approved help destinations only | US-13 | D/U |
| UBR-33 | Honest supported/deferred/different/not-applicable capabilities | US-01/14 | O/U/A |
| UBR-34 | Mobile permission/background variants; no invented desktop parity | US-01/12 | O/U/A |
| UBR-35 | Distinguish accepted platform differences from runtime failures | US-01/14 | O/U |
| UBR-36 | Durable same-ID replay, conflict/CAS and restart for each mutation | US-02/03/04/05/06/08/09/10/11/12 | P/U |
| UBR-37 | First snapshot includes current phase and pending operations | US-03/14 | U |
| UBR-38 | Switching invalidates old identity/resources; cleanup guarded | US-08/04/11 | P/O/U |
| UBR-39 | Exit failure never leaks through ordinary default route | US-05 | O/P/A |
| UBR-40 | Separate/unknown deadlines; session renew is not credential renew | US-09 | P/U |

## Business rules

| Requirement | Client invariant / unit audit focus | Implementation → unit | Dependency |
| --- | --- | --- | --- |
| UR-01 | Service/control plane authoritative; consumer cannot supply truth | US-01/03 | P/U |
| UR-02 | Installation, enrollment, approval, connection and access separate | US-01/02/03/11 | P/O/U |
| UR-03 | OS admin, installation owner and account authority separate | US-01/02/06 | P/O |
| UR-04 | Least privilege and bounded elevated helper | US-06/08 | O/U |
| UR-05 | Explicit exact trust action, revalidated by runtime | US-06 | P/U |
| UR-06 | Disconnect preserves identity and differs from revocation | US-03/08 | P/O |
| UR-07 | Unconfirmed remote cleanup remains visible | US-08 | P/U |
| UR-08 | Minimized diagnostics, no hidden upload/telemetry | US-07/14 | U |
| UR-09 | Connected is not proof of resource reachability | US-03/11 | O/A/U |
| UR-10 | Endpoint/placeholder is not implementation or acceptance | US-01–14 | A |
| UR-11 | Independent UI/core versions with immutable artifact pairing | US-13 | D/U/A |
| UR-12 | No version increase without exact explicit authorization | All client contracts/packages/dependencies | User |
| UR-13 | Common semantics and explicit platform variants | US-01/12/14 | O/U/A |

## Acceptance obligations

All rows remain unaccepted. These are client unit obligations plus the required
external observation, not permission to start the integration phase now.

| Criterion | Client implementation/unit outcome to establish | Implementation → unit | External result still required |
| --- | --- | --- | --- |
| UI-AC-01 | Install/enroll/approve/connect producer chain | US-01/02/03/13 | D/O/U/A: same-artifact authorized resource access |
| UI-AC-02 | Complete state/reason/action actor projection | US-03/06/14 | U: user comprehension |
| UI-AC-03 | Same durable connect/disconnect result after restart/resume | US-03/12 | O/U/A: primary/quick surface and runtime effects |
| UI-AC-04 | Denial/timeout/unavailable/incompatible never false success | US-01/02/03/06 | P/O/U/A: real failure paths |
| UI-AC-05 | Known-secret exclusion from all public/diagnostic outputs | US-02/07/14 | U: clipboard and consumer logs |
| UI-AC-06 | Trust cancellation unchanged; accepted exact origin/key | US-06 | O/P/U/A: real privilege and authority |
| UI-AC-07 | Unauthorized caller cannot mutate; no existence leakage | US-01/02/06/14 | O/U/A: real authenticated transport |
| UI-AC-08 | Bounded redacted bundle matches preview and support needs | US-07 | U/O: approved support scenarios |
| UI-AC-09 | Remote logout distinguished from unconfirmed local forget | US-08 | P/U/A: actual remote cleanup |
| UI-AC-10 | Core install/repair/upgrade/removal preserve intended state | US-13/08 | D/O/U/A: all accepted installer variants |
| UI-AC-11 | Exact contract/artifact pairing and protected local IPC | US-01/13 | D/O/U/A: provenance, live transport and resource smoke |
| UI-AC-12 | Typed public operations support all key actions | US-14 | U: actual accessibility mechanisms |
| UI-AC-13 | Stable typed localizable security meanings | US-14 | U: accepted locales through complete flows |
| UI-AC-14 | New network snapshot, no stale identity/map/route reuse | US-04/08 | P/O/U/A: new network resource |
| UI-AC-15 | Honest capabilities and explicit supported variants | US-01/12 | O/U/A: every supported platform |
| UI-AC-16 | Full profile lifecycle and guarded removal/switch failures | US-08 | P/O/U/A: identity/route isolation |
| UI-AC-17 | Exact exit LAN/families, partial effects and fail-closed | US-05 | O/P/A: traffic cannot escape outside TUN |
| UI-AC-18 | Independent/unknown clocks and confirmed renewal continuity | US-09 | P/O/U/A: real renewal and traffic |
| UI-AC-19 | False versus absent, reset, lock, atomic rejection, apply failure | US-10 | P/O/A: actual DNS/routes/inbound effects |
| UI-AC-20 | Authorized search/pages, stale refresh and safe enable conflicts | US-04/11 | P/O/A: no hidden route override |
| UI-AC-21 | Separate quit/crash/logoff/suspend/resume/restart behavior | US-10/12 | O/U/A: actual OS lifecycle inputs |
| UI-AC-22 | Same request recovery, conflicting payload/CAS rejects | US-02/03/04/05/06/08/09/10/11/12 | P/U/A: response loss and consumer restart |
| UI-AC-23 | First snapshot has phase/operations; refetch drops old context | US-03/08/14 | O/U/A: attach/reconnect to running runtime |
| UI-AC-24 | Observer gets no private deadlines/IDs/account/peer data | US-09/14 | O/U/A: unary and stream real caller isolation |
| UI-AC-25 | Valid/expired/bad-signature source and bad pairing distinct | US-13 | D: approved source and installer outcome |
| UI-AC-26 | Unsupported, managed denial, permission and temporary separate | US-01/10/14 | U/O: notification controls/privacy/dedup |
| UI-AC-27 | Frozen bundle preview/limits, caller/expiry/offset guards | US-07 | U/O: local export without upload |

## Confirmed bounded evidence

Resource catalog assertions and local verification for commits `42fea7a` and
`712a015` are recorded in the headless matrix. They cover service target search
and explicit single-IP subnet identity, not SetResourceEnabled, effective policy,
runtime observations or full UI-AC-20. No requirement row is closed by these
increments. The next audit must expand the tables to exact assertion branches
and close missing client implementations before any integration acceptance.
