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

## External dependencies

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
