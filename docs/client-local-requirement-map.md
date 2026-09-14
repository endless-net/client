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
| US-05 | `service_rpc_exit_catalog.go`, exit change/result/executor, `exit_routes.go`, `exit_filter.go` | `service_rpc_exit_catalog_test.go`, `service_rpc_exit_change_test.go`, `service_rpc_exit_result_test.go`, `service_rpc_exit_executor_test.go`, `exit_routes_test.go`, `exit_filter_test.go` | Public get/select/clear; real OS protection before routes; stale-apply containment; lifecycle worker; actual family observations |
| US-06 | `service_rpc_trust_worker.go`, `service_rpc_trust_recovery.go`, recovery helper | `service_rpc_trust_worker_test.go`, `cmd/endlessnet-client/service_rpc_trust_recovery_test.go` | Exact origin/key/announcement, replay/cancellation/privilege audit and real helper execution |
| US-07 | `service_rpc_diagnostics.go`, bundle create/store/worker/read, `route_observations.go` | `service_rpc_bundle_read_test.go`, `service_rpc_bundle_admin_test.go`, `service_rpc_bundle_archive_test.go`, diagnostics tests | Every bound/privacy/preview/export case; complete route/resource observations |
| US-08 | Profile/switch/logout/forget handlers and workers | `service_rpc_profiles_test.go`, `service_rpc_switch_test.go`, `service_rpc_logout_worker_test.go`, `service_rpc_forget_test.go` | Cleanup and stale-response audit; actual provider isolation; remote cleanup evidence |
| US-09 | Session read/renewal/executor/worker and CLI transport | `service_rpc_session_test.go`, `service_rpc_session_clock_test.go`, `service_rpc_session_concurrency_test.go`, `service_rpc_session_cancel_test.go`, `service_rpc_session_worker_test.go` | All owner/profile/cancellation races; browser origins; producer behavior and real continuity |
| US-10 | Preference admission/worker/projection, `network_preferences.go`, `inbound_filter.go`, TUN and WireGuard engine | Worker tests plus `TestInboundPreferenceAtomicPatchRestartAndSelectiveReset` and `TestInboundPolicyLockRejectsWholeMixedPreferencePatch`: mixed atomic commit, disk replay, selective reset and signed lock rejection | Authenticated transport mutation and OS effect observations remain open; other lifecycle keys and full policy/atomic-patch coverage are incomplete |
| US-11 | `service_rpc_resources.go` | `service_rpc_resources_test.go` | Durable SetResourceEnabled, shared service policy, apply/rollback, overlap/conflicts, invalidation/events and observations |
| US-12 | `service_rpc_uiquit.go`, `service_rpc_runtime_start.go`, signed lifecycle resolver, agent startup before workers | UI-quit/events tests; `service_rpc_runtime_start_test.go` covers source/lock/reset/replay, startup intent and pending recovery; `agent_startup_intent_test.go` proves no control-plane request without saved intent | Real logoff/suspend/resume inputs, complete recovery-race audit and platform lifecycle qualification |
| US-13 | `service_rpc_update.go`, support handler, `packaging/`, client release workflows | `service_rpc_update_test.go`, `service_rpc_host_capabilities_test.go`; later packaging tests | Approved signed update source, support destinations, platform artifact/state/pairing audit |
| US-14 | Typed status/reasons, snapshot/events/log/diagnostics projections | `service_rpc_events_role_test.go`, `service_rpc_session_snapshot_test.go`, diagnostics/privacy tests | Exact observer/private field audit, actionable reasons and full capability distinctions |

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
