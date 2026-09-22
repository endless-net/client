# Remaining cutover execution

Current-source review: 2026-09-22, baseline `8a9fbbe`.
This is a finite work queue, not acceptance or a replacement for the requirement
matrices. IT-01–33, BR/AC-01–20, RULE-01–16, US-01–14 and the local obligations
remain in scope. Historical increment logs describe their commit, not current gaps.

## Execution packages

| Order | User-visible result | Current evidence and missing work | Completion evidence |
| --- | --- | --- | --- |
| 1 | HOST availability does not survive observed route invalidation during collection/publication | `resource_route_observation_engine.go` binds configuration, device, filters, UAPI and relay path; `Current` has no topology invalidator. Later interface/rule checks do not detect a changed earlier destination lookup. `service_rpc_resource_observation.go` currently cancels provider context before returning the receipt. Retain a bounded topology observation through final publication and release every acquired handle on success/error/cancellation. | Changes during batch, final readback and publication yield no positive receipt; event projection withdraws prior availability; cancellation and cleanup assertions. This remains bounded observation, not application reachability or packet-time enforcement. |
| 2 | Linux sleep/wake preferences cause real runtime actions | `agent.go` supplies lifecycle notifications through the Windows service path; the other path passes nil. `startAgentRuntimeLifecycle` returns a no-op for nil events. Preference persistence alone cannot implement suspend/resume. Add the real Linux source, source health/recovery and runtime wiring; audit supported logoff delivery separately. | Source-to-executor tests check durable intent, Suspend/Resume, policy refresh, sync wake, source loss/recovery and shutdown. Later OS acceptance must exercise actual sleep/wake. Readiness gating is not a substitute for the missing source. |
| 3 | LAN_ALLOW safely permits exactly the agreed connected LAN prefixes | Linux native adapter now uses the integrated LAN runtime: packet BTF/NIC/gateway/deadline enforcement, exact nft source/destination/output checks including postrouting, terminal marked routes, durable pin/routing ownership and exact cleanup. Apply/Resume/Observe/Maintain/Contain/Release share the existing effect lock. See the 2026-09-22 integrated increment below. | Integrated unit assertions cover Select→maintenance→loss→Contain→Clear and pending saved-resume health, plus routing/pin recovery boundaries. Real kernel verifier acceptance, per-family traffic, process-death/suspend expiry, NIC replacement, route changes and complete crash/transition acceptance remain mandatory in block 6. |
| 4 | Resources and transitions retain isolation across apply, failure and restart | HOST observations exist; SUBNET/SERVICE/application availability needs its own evidence. Network selection already registers, tears down, activates and applies a target. Audit resources, profile/network switches, preferences and exit together, including late responses and retained protection. | Per-transition assertions for owner/profile/map binding, atomic commit, rollback, cancellation and reopened-store recovery. Exact service address/protocol/port observations cannot be inferred from a WireGuard handshake. |
| 5 | Diagnostics and distribution answer from verified observations | `diagnosticsAs` always marks routes incomplete; agent lookups sample peer targets. `nativeMapDNSDiagnostics` projects desired configuration. `updateInfoAs` always returns SOURCE_UNAVAILABLE. Support metadata has no destination URLs. Implement complete bounded observations and a verified distribution source after establishing its authoritative contract. | Privacy/bounds/authorization assertions, observed resolver/route/resource evidence, and distribution platform/channel/expiry/pairing validation. Unavailable results remain incomplete functionality. |
| 6 | Every remaining contract obligation has inspected assertions | Enrollment/trust/session/connect/disconnect/logout/forget/map expiry, event privacy, SDK/descriptor/CLI/helper and packaging require the full remaining assertion audit. A bounded source search found no production HTTP IPC v2 route, which does not prove full removal. | For every matrix row: implementation, exact assertion, outstanding producer result and platform evidence. Then agree and execute system/platform acceptance, including Windows control-plane recovery. |

Packages 1 and 2 can proceed independently. Package 3 needs a resolved enforcement
design, not another user decision about LAN semantics. Its substeps must finish
the integrated scenario; adding isolated preparation primitives is not closure.
Packages 4–6 preserve the full objective rather than becoming an optional backlog.

Package 1 implementation increment (2026-09-22): the production HOST observer
now subscribes to Linux link/address/route/rule notifications before collecting
its batch. A one-shot lifetime survives collection through final list/event
publication checks. Notification loss, change, cancellation or expiry rejects
positive evidence; release cancels and closes the observer before unlocking
runtime effects. Copies share one close operation. Tests exercise invalidation
during collection and readback, partial factory errors, receipt copies, provider
cleanup and resource availability/event withdrawal. Native delivery remains
asynchronous and needs later Linux qualification; application observations and
the rest of package 4 remain open.
Validation: goimports and vet passed; configured lint reports 0 issues after
making ignored test-only Close results explicit. Full local short suite passed
(internal/client 145.651 s, CLI 12.447 s); its run began before that test-only
errcheck correction. No native/system execution was performed.

Package 2 transition increment (2026-09-22): lifecycle Handle now requires a
transition context, linked to runtime lifetime. Executor/effect/intent-mutation
lock acquisition is cancellable; callbacks receive the transition context.
Cancellation during resume retains an already-held suspend gate. Tests cover
both contended locks, transition expiry during policy refresh, independent
transition cancellation by runtime shutdown and safe shutdown release.
This does not connect Linux events or establish a hard real-time teardown bound:
callbacks and storage must still finish cooperatively. Linux logind integration
needs actual delay-inhibitor FD ownership and completion acknowledgement before
release, plus source generation/recovery. A request to add the pinned D-Bus client
`github.com/godbus/dbus/v5 v5.2.2` and permit its Go-cache writes is pending;
no dependency or existing version has been changed.

Package 2 Linux source increment (2026-09-22): the non-SCM Linux agent now
requires a private native system D-Bus subscription before runtime launch.
It pins the logind unique owner, subscribes to `PrepareForSleep` and
`NameOwnerChanged`, obtains `Inhibit("sleep", ..., "delay")` with a Unix FD on
that connection, checks `PreparingForSleep`, and reads the actual
`InhibitDelayMaxUSec`. A source-owned completion waits for the shared lifecycle
executor's durable intent, teardown and observation before closing the delay FD;
expiry or an unconfirmed transition fails the runtime. Wake reacquires the FD
before the resume gate can open. Source loss first closes the runtime gate,
then uses bounded reconnect and owner/state reconciliation; exhausted recovery
terminates the agent without changing saved intent solely for source health.
The source uses `github.com/godbus/dbus/v5 v5.2.2`, explicitly approved for
this dependency and Go-cache access. This is bounded logind sleep-cycle
coordination, not an arbitrary kernel-suspend guarantee or native acceptance.
Linux logoff delivery remains unimplemented; Windows SCM logoff and power paths
remain synthetic-unit verified pending platform qualification.
Validation: goimports, vet, configured lint (0 issues) and full local short suite
passed (internal/client 142.623 s, CLI 11.119 s). The preceding HOST topology
commit passed short CI [35718905260](https://github.com/endless-net/client/actions/runs/35718905260).

Package 5 diagnostic observation increment (2026-09-22): Linux address lookups
use explicit family and [JSON output](https://man7.org/linux/man-pages/man8/ip.8.html).
Only one result for the requested address with a usable interface and a local/
unicast route type can supply interface evidence; duplicate keys, unrelated
destinations, multiple rows, errors and text output are rejected. This does not
turn address samples into full table/rule coverage: `RouteInspection` currently
describes an address lookup and diagnostics remains incomplete. Linux and Darwin
now report a bounded kernel release from uname, labelled as a kernel version;
hostname, build description and raw inspection errors are excluded. Darwin
kernel release is not represented as a macOS product version. Platform short CI
must compile/test these OS-specific collectors before later native qualification.
Local validation: goimports, vet, configured lint (0 issues) and full short suite
passed on Windows (internal/client 141.091 s); Unix-only uname tests await CI.

Package 6 logout/session audit, delegated block 1 (2026-09-22): Logout and active
Forget require a confirmed Stop before deleting credentials. PRESERVED and
unrecognized enum values fail even with nil error, before restart continuity
normalization. UNKNOWN and NOT_APPLICABLE remain confirmed by the existing
driver contract. Logout admission now persists NetworkID and the original
authority digest; dispatch, remote checkpoints, Stop and final cleanup validate
that binding. Retained remote confirmation and cleanup request correlation check
NetworkID separately. The shared `logoutAuthority` digest is unchanged, so a
network change cannot make Connect accept already-revoked credentials.

Inspected and extended assertions:

- `TestRPCLogoutRequiresConfirmedStopAcrossRestart`: persisted plan binding,
  resumed Down with invalid/PRESERVED continuity, retained active/profile
  credentials and remote progress, explicit retry and durable cleanup.
- `TestRPCLogoutRejectsChangedDispatchContext`: changed network/authority after
  reopen and a deterministic network change after RUNNING admission reject remote
  dispatch and Stop. `TestRPCLogoutRejectsNetworkChangeBeforeStop` rejects a change
  after remote checkpoints. `TestRPCCleanupRejectsContextChangeDuringStop` rejects
  late Logout/Forget results after network/profile/owner/authority changes.
- `TestRPCLogoutConfirmationCannotCrossNetworks`: reopened confirmation retains
  correlation only for its original network, new-network logout starts without
  foreign progress, old callbacks cannot modify its plan, and Connect still
  rejects the revoked credentials. The journal-retention correlation test also
  covers a changed network. `TestRPCLogoutAndForgetStopContract` checks all three
  confirmed continuity values and both rejected values after reopen.
- `TestRPCForgetCancelsSessionRenewalAndDrainsBeforeCleanup`: rejected Forget does
  not cancel renewal; accepted Forget drains the provider and rejects its late
  success; queued/browser-waiting cancellation survives reopen and replays as
  CANCELLED. Logout/renewal conflict tests check both admission orders;
  independent Disconnect preserves disconnected intent through rotation.
- Renewal plans now persist NetworkID. Executor restart/polling assertions reject
  changed-network dispatch and late poll results without replacing the retained
  token. Atomic result assertions reject changed owner/token/profile/network,
  foreign user/request and expired result/replay without partial writes; success
  and request replay retain the rotated session after reopen.

These are bounded client/store/provider assertions. Native driver behavior and
producer/browser end-to-end logout/renewal acceptance remain platform/CI work;
no native, E2E, system or release run was performed for this block. Versions are
unchanged. Other cutover packages remain outside this delegated block.
Validation: `goimports -w .`, `go vet ./...` and configured golangci-lint passed
(0 issues). The final `go test -short ./...` completed all block assertions
without a reported failure, but the full suite is **not green**: internal/client
failed after 143.501 s in `TestResourceHostCurrentRechecksRelayAfterReadback`
(`resource_route_observation_lifetime_test.go:62`, WireGuard fwmark update on a
closed network connection). Resources are outside this block and were not edited.
The preceding push CI [35720342020](https://github.com/endless-net/client/actions/runs/35720342020)
was already completed/failed in the macOS resource-filter test with the same
closed-connection error class; no running workflow needed cancellation.

## Corrected network-selection evidence

`cmd/endlessnet-client/service_rpc_host.go` installs the network catalog,
registration and cleanup providers. `internal/client/service_rpc_host.go` starts
`StartNetworkSelectionWorker`. Activation checkpoints DownStarted, confirms Stop,
then atomically adopts the isolated target; apply checkpoints its attempt and
retains cleanup intent until Stop succeeds. The native profile driver calls
WireGuard Down/Configure or the saved-exit runtime.

Inspected assertions, not just filenames:

- `TestNetworkSelectionThroughNativeTransport`: pending and terminal replay,
  one registration/stop/start, unauthenticated rejection, catalog denial without
  effects, and capability withdrawal when the worker stops. Drivers are injected.
- `TestNetworkActivationStopsSourceBeforeAtomicTargetAdoption`: source retained
  until Stop under the runtime lock, no mixed target configuration, old observation
  invalidation and disk replay without another Stop.
- `TestNetworkActivationRetainsBarrierOnUncertainOrStaleStop`: failed/unconfirmed
  Stop, cancellation, source change and target expiry cannot activate the target.
- `TestNetworkApplyFailureResumesCleanupWithoutReapply` and
  `TestNetworkApplyRecoversUncertainAttemptByStoppingFirst`: disk recovery retains
  failed cleanup and stops an uncertain previous attempt before another apply.
- `TestNetworkApplyContainsContextChangeDespiteProviderSuccess`: late owner/map
  changes and accepted Disconnect prevent success and preserve the newer intent.

These assertions do not prove actual route isolation, all cross-worker races,
producer behavior or provider switching. Those remain open in package 4.

## External results to obtain

Block 2 integrated increment (2026-09-22): production LAN_ALLOW now uses the
native runtime described in [runtime gaps](client-runtime-implementation-gaps.md#integrated-lan_allow-increment--2026-09-22).
This supersedes the preparation-only statements in historical increments below.
The coordinator remains paused; there is no release/version increase or claim
that the whole cutover is complete. Real kernel/syscall/traffic and full
profile/network/preference/crash-boundary qualification remain in block 6.
Recovery commit `5608aa2` passed all short CI platforms in
[35738421695](https://github.com/endless-net/client/actions/runs/35738421695).
Integrated increment local validation: `goimports -w .`, `go vet ./...`,
configured golangci-lint (0 issues), and `go test -short ./...` passed;
`internal/client` completed in 154.573 s. One preceding full run failed during
BLOCK-only `TestNativeExitResumeFailedLiveObservationContainsWithoutReapply`
setup with `failed to update fwmark: use of closed network connection`.
The full rerun passed; this records a non-reproduced failure, not a diagnosed fix.
No local native/E2E/installer/release/system validation was run.

Block 2 recovery increment (2026-09-22): the production native executor now
binds its store to cleanup after observed Stop. Clear and containment retire a
LAN manifest only after exact pin revalidation, BPF link detach, hook absence,
unpin and final scoped-pin absence under retained namespace/directory handles.
Old-boot recovery never looks up or detaches old numeric IDs; a current pin-name
collision prevents retirement. Errors/cancellation preserve the journal. Store
retirement checks both protection copies and survives reopen. Containment's
final commit permits only this verified metadata retirement, retaining all
other operation/context checks. LAN_ALLOW datapath remains in progress.

`TestExitLANBPFCleanupDetachesBeforeUnpinAndRetainsRetry` asserts full/partial/
empty inventory, failed detach/absence/unlink, cancellation, replacement and
reappearing pins. `TestNativeExitLANCleanupRetiresOnlyConfirmedMatchingJournal`
checks native-error/cancellation/CAS failures and both durable copies after
reopen. These are injected kernel/store assertions, not native acceptance.
Local recovery validation: goimports, vet and configured lint (0 issues) passed;
full short suite passed, internal/client 146.635 s. The new packet compiler work
is separate and was not part of that test run. Baseline block 1 short CI
[35736466574](https://github.com/endless-net/client/actions/runs/35736466574)
completed successfully.

| Owner | Required authoritative result | Requirements |
| --- | --- | --- |
| Distribution/product and client-ui release owners; client packaging owns core integration | Approved verifiable release discovery and UI/core compatibility data, trust source, exact build/artifact binding, platform/channel/expiry rules and approved download/support destinations. Existing core hash manifests alone do not attest a UI/core pair. Establish whether an existing producer contract supplies this before designing another format; no version increase is authorized. | IT-33, US-01/13, BR/AC-19, UI-AC-10/11/25 |
| Backend/clientapi owners | Producer-backed approval, registration/replay, map/revocation, renewal and cleanup evidence against the pinned contracts. | Relevant IT-01–24 and US-02–11 matrix rows |
| Client-ui/mobile platform owner | Actual mobile native bridge and consumer conformance; this repository's abstract Dart bridge is not native integration. | US-01 and consumer transport obligations |
| User/acceptance owners after implementation and unit audit | Agreed system/platform run and observed results on the final artifacts. | All acceptance gates |

No external dependency above authorizes changes outside this repository.
Distribution source clarification from read-only source review: Windows pairing
provenance already exists in client-ui at `1a51f7960ef013abd757317247c71fa0e08aab6f`.
[write-release-provenance.ps1](https://github.com/endless-net/client-ui/blob/1a51f7960ef013abd757317247c71fa0e08aab6f/scripts/write-release-provenance.ps1)
binds UI/core commits, manifest/lock/IPC descriptor hashes and signed/unsigned
artifact hashes. Reuse that producer evidence rather than inventing a second
pairing format. Its [release workflow](https://github.com/endless-net/client-ui/blob/1a51f7960ef013abd757317247c71fa0e08aab6f/.github/workflows/release.yml)
publishes the JSON, but the standalone JSON is not among attestation subjects
(MSI, ZIP and SBOM are). Publication alone therefore does not establish a trusted
discovery manifest. The remaining distribution/product/UI-owner result is the
authoritative index/API, trust bootstrap/rotation, channel/platform and expiry/
replay/classification rules, and cryptographic binding to that existing pairing.
No requirement is closed by this review. Baseline push short CI
[35711096789](https://github.com/endless-net/client/actions/runs/35711096789)
succeeded; native/system acceptance remains outstanding.
