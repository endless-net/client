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
| 3 | LAN_ALLOW safely permits exactly the agreed connected LAN prefixes | Native host/worker already run. `native_exit_executor.go` and `native_exit_resume.go` accept BLOCK only. Existing BPF checks mark/deadline, not physical-interface/route identity. Complete the packet path, exact interface-instance binding, gateway exclusion and overlay/resource priority before opening traffic. Complete startup/partial-pin/old-boot recovery through detach, unpin, absence proof and journal removal. Wire this lifecycle into Apply/Resume/Observe/Maintain/Contain/Release under the existing effect lock. | Integrated positive, negative, cancellation and crash-boundary assertions across Select→maintenance→loss→Contain→Clear→restart. Shared authority deadlines must not renew stale evidence. Later real per-family traffic, process-death expiry, NIC replacement and route-change tests are mandatory. No capability claim from preparation helpers. |
| 4 | Resources and transitions retain isolation across apply, failure and restart | HOST observations exist; SUBNET/SERVICE/application availability needs its own evidence. Network selection already registers, tears down, activates and applies a target. Audit resources, profile/network switches, preferences and exit together, including late responses and retained protection. | Per-transition assertions for owner/profile/map binding, atomic commit, rollback, cancellation and reopened-store recovery. Exact service address/protocol/port observations cannot be inferred from a WireGuard handshake. |
| 5 | Diagnostics and distribution answer from verified observations | `diagnosticsAs` always marks routes incomplete; agent lookups sample peer targets. `nativeMapDNSDiagnostics` projects desired configuration. `updateInfoAs` always returns SOURCE_UNAVAILABLE. Support metadata has no destination URLs. Implement complete bounded observations and a verified distribution source after establishing its authoritative contract. | Privacy/bounds/authorization assertions, observed resolver/route/resource evidence, and distribution platform/channel/expiry/pairing validation. Unavailable results remain incomplete functionality. |
| 6 | Every remaining contract obligation has inspected assertions | Enrollment/trust/session/connect/disconnect/logout/forget/map expiry, event privacy, SDK/descriptor/CLI/helper and packaging require the full remaining assertion audit. A bounded source search found no production HTTP IPC v2 route, which does not prove full removal. | For every matrix row: implementation, exact assertion, outstanding producer result and platform evidence. Then agree and execute system/platform acceptance, including Windows control-plane recovery. |

Packages 1 and 2 can proceed independently. Package 3 needs a resolved enforcement
design, not another user decision about LAN semantics. Its substeps must finish
the integrated scenario; adding isolated preparation primitives is not closure.
Packages 4–6 preserve the full objective rather than becoming an optional backlog.

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

| Owner | Required authoritative result | Requirements |
| --- | --- | --- |
| Distribution/product and client-ui release owners; client packaging owns core integration | Approved verifiable release discovery and UI/core compatibility data, trust source, exact build/artifact binding, platform/channel/expiry rules and approved download/support destinations. Existing core hash manifests alone do not attest a UI/core pair. Establish whether an existing producer contract supplies this before designing another format; no version increase is authorized. | IT-33, US-01/13, BR/AC-19, UI-AC-10/11/25 |
| Backend/clientapi owners | Producer-backed approval, registration/replay, map/revocation, renewal and cleanup evidence against the pinned contracts. | Relevant IT-01–24 and US-02–11 matrix rows |
| Client-ui/mobile platform owner | Actual mobile native bridge and consumer conformance; this repository's abstract Dart bridge is not native integration. | US-01 and consumer transport obligations |
| User/acceptance owners after implementation and unit audit | Agreed system/platform run and observed results on the final artifacts. | All acceptance gates |

No external dependency above authorizes changes outside this repository.
No requirement is closed by this review. Baseline push short CI
[35711096789](https://github.com/endless-net/client/actions/runs/35711096789)
succeeded; native/system acceptance remains outstanding.
