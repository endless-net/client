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
workflow. Full verification, installation and contract matrices run on PR or
explicit manual dispatch. CodeQL and Protobuf contract checks no longer run on
push. Tag-triggered publication workflows are unchanged; publication still
requires exact-source full manual evidence, not a green short-test push. No PR
or manual integration run is created merely to bypass the implementation phase.

The generated handler interface is in
`clientipc/v0/clientipcconnect/service.connect.go`. `ClientRPCService` embeds its
unimplemented handler in `internal/client/service_rpc_handlers.go`. At this audit,
the eight methods below have no runtime overrides; generated SDK methods and CLI
commands must not be counted as their runtime implementations.

| Requirement area | Missing runtime methods | Required implementation and unit evidence |
| --- | --- | --- |
| US-09 session/renewal | `GetSession`, `RenewSession` | Independent session/credential state; protected renewal authority; durable request binding; browser/poll outcomes; expiry, identity mismatch and lost-response recovery |
| US-05 exit | `ListExitNodes`, `GetExitNode`, `SelectExitNode`, `ClearExitNode` | Verified catalog and policy; family/LAN constraints; requested/effective distinction; durable selection; partial apply/clear and fail-closed path loss |
| US-11 resources | `ListResources`, `SetResourceEnabled` | Verified resource identity and policy; search/pagination snapshot binding; local intent; hidden-resource denial, stale context and conflicts |

Additional partial implementations must not be mistaken for complete domains:

| Area | Source evidence | Remaining work |
| --- | --- | --- |
| Preferences/policy | `service_rpc_uiquit.go` accepts only a single `UI_QUIT` key for Set/Reset; `service_rpc_preferences_test.go` and `service_rpc_uiquit_test.go` exist | Implement the remaining specified settings and policy controls; unit-test presence versus false, locks/source, requested/effective state and atomic patch rejection |
| Diagnostics | `service_rpc_diagnostics.go` always reports `diagnostics_os_routes_not_collected`; `service_rpc_diagnostics_test.go` exists | Collect actual platform route observations through bounded providers and test typed projection/failures; do not substitute desired configuration for observed OS state |
| Updates | `service_rpc_update.go` reports `update_source_not_configured`; `service_rpc_update_test.go` exists | Bind an approved distribution source and verify its projection; unavailable is not up-to-date, and unavailable-path tests do not prove update discovery |

Existing test filenames above identify starting points for review, not assertions
that all listed scenarios are already covered.

## Client-owned scenario map

This maps all fourteen system-scenario families to the client boundary. It is
not yet the per-requirement BA/SA completion matrix. All rows remain open until
each applicable requirement and negative outcome has been individually audited.
Paths in the table are under `internal/client/` unless qualified otherwise.

| Scenario | Runtime / contract implementation | Unit starting point and verified limit | Remaining client work / external dependency |
| --- | --- | --- | --- |
| US-01 bootstrap | `service_rpc_host.go`, `service_rpc_capabilities.go`; protected transport in `clientipc/local` | `service_rpc_host_capabilities_test.go`; root short tests do not run the nested `clientipc` module | Audit startup/identity/capability failure variants and separate nested-module unit execution |
| US-02 enrollment | `service_rpc_enrollment_worker.go`, `service_rpc_enrollment_executor.go` | `TestRPCEnrollmentWorkerRecoveryAndShutdown` | Trace approval, denial, expiry, cancellation and ownership outcomes individually; actual backend approval is external |
| US-03 connection | `service_rpc_connect.go`, `service_rpc_disconnect.go` | `TestRPCConnectDurabilityAndFailure`, `TestRPCDisconnectPreemptsApplyOnlyAfterAcceptance` | Audit remaining races and recovery boundaries; unit driver results are not OS traffic evidence |
| US-04 networks/peers | `service_rpc_networks.go`, `service_rpc_select_network.go`, `service_rpc_peers.go` | `TestRPCSelectCurrentNetworkIsDurableNoop`; peer pagination/event suites | Current-network no-op is not cross-network selection; audit real provider switching and stale catalogs |
| US-05 exit | Eight-method gap inventory above includes all four exit methods | No runtime implementation to qualify; CLI/SDK coverage is insufficient | Verified backend policy consumption, durable selection and family-specific effects |
| US-06 trust/recovery | `service_rpc_trust_worker.go`, `service_rpc_trust_recovery.go` | `TestRPCTrustWorkerRecoveryAndIndependentDisconnect` | Audit exact authority tuple, replay and privilege outcomes; helper/OS integration remains later evidence |
| US-07 diagnostics | `service_rpc_diagnostics.go`, bundle worker/store/read handlers | `TestRPCDiagnosticsRejectsChangedContextAndInvalidProvider`, `TestRPCAdministratorBundleScopeAndRestart` | Actual OS route collection missing; inspect redaction, bounds and archive lifecycle coverage separately |
| US-08 profiles/logout | Profile, logout and forget handlers | `TestRPCProfileRemovalGuards`, `TestRPCForgetCancelsQueuedEnrollmentAfterRestart`, `TestRPCProfilePaginationBindingsAndPrivacy` | Audit all removal/cleanup/replay cases; remote revocation depends on backend result |
| US-09 session/renewal | Missing `GetSession` and `RenewSession` overrides | No runtime implementation to qualify | Session authority and durable renewal require updated backend contract consumption |
| US-10 preferences/policy | `service_rpc_uiquit.go`, preference validators | `TestRPCUIQuitRejectsUnsupportedPatchAtomically` rejects mixed UI-quit/DNS patch without changing revision or override | Implement remaining settings; rejection is not DNS/routes/policy functionality |
| US-11 resources | Missing list/mutation overrides | No runtime implementation to qualify | Implement catalog, policy-aware enablement and actual runtime effects |
| US-12 lifecycle | `service_rpc_uiquit.go` | `TestRPCUIQuitPreferencesAndExecution` | UI-quit support is not logoff/suspend/resume adapter implementation; audit each specified event |
| US-13 distribution/help | `service_rpc_update.go`, `service_rpc_handlers.go`; packaging and producer manifest workflows | `TestRPCUpdateInfoDoesNotInferReleaseOrPairing` | Verified update source remains absent; installation/release evidence deferred until implementation phase completes |
| US-14 presentation/privacy | Typed status/operation/log/diagnostics projections | `TestRPCObserverSnapshotExcludesPrivateState`, diagnostics suites | Audit producer reasons/actions and secret redaction; UI rendering, locale selection and assistive technologies belong outside this task |

The historical [runtime gap audit](native-runtime-gap-audit.md) reported nine
missing methods at its pinned source. The current count is eight because
`GetUpdateInfo` now has an explicit unavailable-source implementation. This
reduces missing overrides, not the remaining update-discovery requirement.

US-12 unit increment: `TestRPCUIQuitReplayDoesNotApplyChangedPreference` checks
that an acknowledged keep-intent notification replays the original operation
after changing the preference to disconnect, both before and after reopening
the durable store. Replay must not change revision or connected intent. A new
request ID must still schedule disconnect using the new preference. This tests
local durable admission, not OS suspend/logoff integration or completed Down.

## External dependencies and approvals

- `clientapi` owns backend DTOs, policy validation and session transport. `client`
  currently pins `github.com/endless-net/client-api/clientapi v1.12.0` in `go.mod`.
  The previously inspected producer revision
  `e4fb0a95d2af577cda7425abec064a4ed49a3eae` adds session and policy contracts.
  Consuming an updated dependency requires explicit version-change approval;
  do not copy its DTOs, use a local replacement or rewrite a published version.
- Backend owners must implement the corresponding producer behavior. The
  existence of a producer contract is not evidence of deployed behavior.
- Distribution source selection and external platform/provider capabilities
  require their owners' input. Record each concrete dependency as requirements
  are audited; do not invent product links, sources or acceptance evidence.

These dependencies do not prevent work on independent client-owned functionality
and unit tests. They do prevent claiming the affected requirements complete.
