# Headless requirements: client implementation and unit audit

Status: open implementation audit, not integration execution or acceptance.

The [local v0 matrix](client-local-requirement-map.md) explicitly traces the
additional UF-01–23, UBR-01–40, UR-01–13 and UI-AC-01–27 obligations through
US-01–14 to client implementation/unit entry points and dependency owners.
Both matrices remain open for assertion-level review and completion evidence.

Source: [headless SA, architecture at bdb5ba6](https://github.com/endless-net/architecture/blob/bdb5ba63c0e5356122c0760f4d63205e84ef507d/docs/ru/headless-client-system-design.md#10-спецификации-первых-интеграционных-тестов).
All 33 specified IT identifiers are retained below. Integration execution is
deferred until the implementation/unit phase has been audited. A unit test is
not a substitute for the SA's local, consumer, provider or real-pair observation.

The [HC source catalog](https://github.com/endless-net/architecture/blob/bdb5ba63c0e5356122c0760f4d63205e84ef507d/docs/ru/headless-client-use-cases.md)
is explicitly comparative and draft, not an approved feature-parity roadmap.
Do not infer authorization to implement SSH, file transfer or other unresolved
product features solely from their presence in that catalog.

## Per-IT client trace

`I` means `internal/client/`; `C` means `cmd/endlessnet-client/`.
Test paths below exist and identify review entry points, **not complete unit
coverage**. Each row remains open; exact assertion-by-assertion audit is required.
No integration tests were executed to populate this table.

| SA ID | Client responsibility / implementation area | Unit review entry point | Missing work or external observation |
| --- | --- | --- | --- |
| IT-01 | Enrollment worker and approval driver | I `service_rpc_enrollment_worker_test.go`; C `service_rpc_approval_driver_test.go` | Audit every pending/completion binding; producer approval and actual sync are external evidence |
| IT-02 | Enrollment rejection and authorization separation | C `enrollment_workflow_test.go` | Foreign/expired poll authority and session outcomes need producer-backed verification |
| IT-03 | Validate approval result before registration adoption | C `enrollment_context_test.go`; `service_rpc_approval_driver_test.go` | Audit operation/device binding and missing registration; preserve SA G-02 dependency |
| IT-04 | Registration input, identity and response validation | C `registration_input_test.go`; `registration_transport_test.go` | Account/key authorization is a producer responsibility; separate-node observations deferred |
| IT-05 | Durable registration request and ambiguous response recovery | C `enrollment_workflow_test.go`; I `service_rpc_enrollment_worker_test.go` | Audit same-ID replay and conflicting payload; backend replay semantics remain required |
| IT-06 | Consume join authorization failures without revoking unrelated node state | C `registration_transport_test.go` | Admin revocation and existing-node policy require backend owner; client must not implement admin mutation |
| IT-07 | Map identity/context validation and effective peer projection | C `network_map_global_cache_test.go`; `service_rpc_peers_test.go` | Audit wrong-network rejection; bidirectional traffic is not unit evidence |
| IT-08 | Delta application, signature validation and replay | C `map_replay_test.go`; `agent_map_catchup_test.go` | Audit replay after withdrawal; producer delta and real-flow observations deferred |
| IT-09 | Reject invalid delta base/vector and request full state | C `agent_map_catchup_test.go`; `map_stream_identity_test.go` | Audit each recovery branch without permitting rejected state |
| IT-10 | Reject tampered/expired authority before cursor/effect update | C `map_replay_test.go`; I `map_expiry_filter_test.go` | Audit payload, identity, signature and expiry separately |
| IT-11 | Recover stream and persisted base/checkpoint | C `map_stream_identity_test.go`; `agent_map_catchup_test.go` | Truncated frame/EOF and persisted-base combinations need assertion review |
| IT-12 | Direct/relay path selection and enforcement | I `pathstatus_test.go`; `wireguard_relay_recovery_test.go` | Real two-client nonce echo and denied pair cannot be proved by unit tests |
| IT-13 | Consume relay authorization and preserve direction/network scope | I `wireguard_relay_recovery_test.go` | Private relay RPC caller checks belong to producer; real directed traffic deferred |
| IT-14 | Endpoint changes and stale response rejection | I `pathstatus_test.go`; C `agent_map_catchup_test.go` | Audit old/new projection ordering; network migration remains platform evidence |
| IT-15 | ACL withdrawal and local inbound restriction | I `application_runtime_test.go`; `map_expiry_filter_test.go` | Local inbound preference still unimplemented; contract revoke bounds and control flow required |
| IT-16 | DNS/routes projection and local acceptance preferences | I `application_runtime_test.go`; `service_rpc_preferences_test.go` | DNS/routes preferences incomplete; route approval belongs to backend |
| IT-17 | Discovery binding and application lease enforcement | I `application_runtime_test.go` | Audit wrong node/hash and lease expiry; discovery reporting and producer acceptance require separate evidence |
| IT-18 | Signed sharing grant enforcement and identity replacement | C `map_sharing_expiry_test.go`; `sharing_backend_test.go` | Producer consent/accept/revoke and actual recipient traffic remain external/late-phase checks |
| IT-19 | Explicit trust confirmation and credential recovery | I `service_rpc_trust_worker_test.go`; C `service_rpc_trust_recovery_test.go` | Audit wrong/same key and privilege; OS helper execution deferred |
| IT-20 | Typed recovery classification, no unauthorized enrollment | C `recovery_workflow_test.go`; `service_rpc_recovery_status_test.go` | Audit temporary/terminal/policy/malformed cases against pinned producer contract |
| IT-21 | Durable renewal identity after response loss/restart | I `service_rpc_session_renewal_test.go`; `service_rpc_session_executor_test.go`; `service_rpc_session_browser_test.go`; `service_rpc_session_result_test.go`; `service_rpc_session_worker_test.go`; C `service_rpc_session_transport_test.go` | Bound admission, same-ID retry, polling checkpoints, atomic rotation and host/transport/public wiring implemented; full cleanup/concurrency audit and new traffic remain later evidence |
| IT-22 | Stop/switch invalidates delayed old responses | I `service_rpc_connect_test.go`; `service_rpc_switch_test.go`; `service_rpc_session_concurrency_test.go` | Renewal conflict ordering and independent Disconnect covered; audit remaining context switches; preserve SA G-04 constraint |
| IT-23 | Distinguish remote logout confirmation from local forget | I `service_rpc_logout_worker_test.go`; `service_rpc_forget_test.go`; `service_rpc_session_cancel_test.go` | Renewal cancellation/drain before local forget and restart replay covered; verify remaining transport/terminal results; backend cleanup cannot be inferred from local state |
| IT-24 | Respect node revocation and remaining lease limits | I `map_expiry_filter_test.go`; C `recovery_test.go` | Node deletion is backend-owned; online/offline real-pair checks deferred |
| IT-25 | Consent-bound flow collection and replay windows | I `flow_logs_test.go` | Audit durable accepted ID and duplicate-window prevention; backend receipt external |
| IT-26 | Mutation idempotency, conflict and lookup after restart | I `service_rpc_test.go`; `service_rpc_restart_test.go` | Audit every mutation kind, not only generic admission |
| IT-27 | Snapshot-first events and observer privacy | I `service_rpc_events_test.go`; `service_rpc_events_role_test.go` | Audit concurrent transitions and owner operation visibility individually |
| IT-28 | Profile context isolation and removal guards | I `service_rpc_profiles_test.go`; `service_rpc_switch_test.go` | Cross-network/provider switching incomplete; actual route isolation deferred |
| IT-29 | Independent session and node-credential clocks | I `service_rpc_session_test.go`; `service_rpc_session_snapshot_test.go`; `service_rpc_session_clock_test.go`; `service_rpc_session_worker_test.go`; C `service_rpc_credential_test.go` | GetSession, bound snapshots, renewal worker and clock transitions implemented; full cross-clock assertion audit remains open; platform timing is later evidence |
| IT-30 | Exit apply/clear, locked settings and resource conflicts | I `service_rpc_preferences_test.go` covers validation only; `service_rpc_exit_catalog_test.go` covers signed grants; `exit_routes_test.go`, `exit_filter_test.go`, `exit_tun_test.go` cover isolated enforcement; `exit_engine_test.go` and `exit_export_test.go` cover no implicit defaults; `service_rpc_exit_change_test.go`, `service_rpc_exit_result_test.go`, `service_rpc_exit_executor_test.go` cover durable admission, result validation and serialized same-ID retry | No-selection projection is wired; explicit exit activation, OS fail-closed, containment after stale in-flight effects, public apply/clear/status, resources and remaining settings are missing; injected tests do not prove OS effects |
| IT-31 | UI quit versus crash; OS lifecycle and stream recovery | I `service_rpc_uiquit_test.go`; `service_rpc_events_test.go` | Logoff/suspend/resume adapters incomplete; do not add untrusted OS events to UI notification RPC |
| IT-32 | Private diagnostics access, bounded export and resource context | I `service_rpc_bundle_read_test.go`; `service_rpc_bundle_admin_test.go`; `service_rpc_resources_test.go` | Signed catalog search includes service hostname, port and protocol; resource enablement/observations and independent audit of every bundle bound and authorization case remain open |
| IT-33 | Contract mismatch and honest update/distribution status | `clientipc/rpc/protocol_test.go`; I `service_rpc_update_test.go` | Verified update source absent; unavailable-path tests do not prove installation or release pairing |

## Business requirements and acceptance criteria

Source: [headless BA at the same architecture revision](https://github.com/endless-net/architecture/blob/bdb5ba63c0e5356122c0760f4d63205e84ef507d/docs/ru/headless-client-business-analysis.md).
All BR-01–20 and AC-01–20 are accounted for below. Links to IT rows inherit their
runtime/unit review entry points; **no AC is accepted by this inventory**.
The BA's product-choice and development categories are retained as unresolved
scope/variant dependencies, not silently promoted to implemented release scope.

| BR | AC | Client responsibility / trace | Open dependency or missing evidence |
| --- | --- | --- | --- |
| BR-01 | AC-01 | Packaging, service startup and protected transport; US-01/13, IT-33 | Q-02 supported variants; real reboot without OS login is not a unit assertion |
| BR-02 | AC-02 | Human enrollment and independent session recovery; IT-01–03, IT-20/29 | IdP/approval producer; session runtime missing |
| BR-03 | AC-03 | Registration identity, attributes, replay and failures; IT-04–06/24 | Producer registration/revocation semantics; Q-03/04 |
| BR-04 | AC-04 | Persistent/ephemeral workload and termination behavior; IT-24, US-12 | Q-02/03 workload variants, loss detection and cleanup; userspace mode is not inferred from container tests |
| BR-05 | AC-05 | Admission before use and explicit trust recovery; IT-01–03/19 | Q-05 independent approval model; backend admission and actual denied traffic |
| BR-06 | AC-06 | Durable intent and lifecycle settings; IT-22/26/31 | Remaining lifecycle preferences/adapters incomplete |
| BR-07 | AC-07 | Single active profile and isolated context transitions; IT-22/23/28 | Q-06 variants; real provider/network switching incomplete |
| BR-08 | AC-08 | Peer projection and allowed versus denied service use; IT-07/12 | Actual application traffic; status alone is insufficient |
| BR-09 | AC-09 | DNS projection and external resolver preference; IT-16 | Accept-DNS setting incomplete; resolver behavior is platform evidence |
| BR-10 | AC-10 | Revocation and local restriction intersection; IT-08–10/15/30 | Q-04 revoke bounds; local inbound preference incomplete |
| BR-11 | AC-11 | Direct/relay recovery, network change and bounded offline authority; IT-11–14/20 | Q-10 recovery bounds and Q-07 role variants; HA is not inferred from basic recovery |
| BR-12 | AC-12 | Subnet/resource routing and conflicts; IT-16/30 | Q-07 overlap policy; resource API/preferences incomplete; hosting/provider responsibilities remain distinct |
| BR-13 | AC-13 | Authorized exit selection, LAN and fail-closed behavior; IT-30 | Exit runtime missing; Q-07 and updated backend policy contract |
| BR-14 | AC-14 | Logical application discovery and lease-bound routing; IT-17 | Q-08 host/discovery semantics; producer report acceptance and real resource access |
| BR-15 | AC-15 | Remote commands/files/forwarding are product-development scope | Q-01/09 accepted feature variants and producer authority are unresolved; no fabricated client implementation claim |
| BR-16 | AC-16 | Machine sharing enforcement; IT-18 | Q-09 file-transfer scope unresolved; sharing does not imply whole-network access |
| BR-17 | AC-17 | Private/public publication remains product-choice scope | Q-01/09 audience, authorization, termination and HTTPS identity; URL presence is not authorization |
| BR-18 | AC-18 | Typed readiness/failures, logs, diagnostics and bounded automation; IT-25/27/32 | Q-10/11 data and timing requirements; route sampling remains partial |
| BR-19 | AC-19 | Update/recovery identity and explicit reset; IT-21/23/33 | Verified update source missing; real installation/resource outcomes deferred |
| BR-20 | AC-20 | Local removal distinguished from server revocation; IT-23/24 | Q-04 lease/revoke bounds; remote lost-device cleanup cannot be proven locally |

## Business-rule obligations

Each rule below remains open for exact assertion review. Existing code/test
families are identified in the IT table, rather than counted as proof by name.

| Rule | Client invariant and trace | Unit audit / external limit |
| --- | --- | --- |
| RULE-01 | Installation privilege does not grant account/network authority; IT-02/04/19 | Audit local administrator versus backend authorization separately |
| RULE-02 | Context changes cannot union access; IT-22/28 | Delayed response and profile isolation assertions |
| RULE-03 | Enrollment authority revocation differs from node revocation; IT-06/24 | Backend owns revocation; client must preserve typed distinction |
| RULE-04 | Separate machines have separate identity; IT-04/05 | Registration replay is not identity cloning; Q-03 replacement semantics |
| RULE-05 | Pending registration/approval is not permission; IT-01–03/17 | No credential/map/route use before valid completion |
| RULE-06 | Local settings may only restrict central authority; IT-15/16/30 | Missing preference/resource runtime must enforce policy intersection |
| RULE-07 | Intent, apply result and actual availability differ; IT-07/27/30 | Snapshot/operation assertions; real traffic remains separate evidence |
| RULE-08 | Ephemeral access has explicit normal/crash termination; BR-04, IT-24 | Q-03 and backend expiry/cleanup; no inferred perpetual authorization |
| RULE-09 | Public audience requires explicit authorized choice; BR-17 | Product/provider dependency; never infer publication permission from URL |
| RULE-10 | Sharing one machine does not share its network; IT-18 | Recipient/direction/identity replacement and unrelated-target denial |
| RULE-11 | Offline operation cannot extend trust indefinitely; IT-10/11/20/24 | Expiry and replay tests; Q-04/05 bound remaining authority |
| RULE-12 | Disconnect/logout/stop/reset/uninstall/revoke remain distinct; IT-22–24/31/33 | Audit each durable outcome and failure path independently |
| RULE-13 | Declared unattended mode works without desktop session; BR-01/04 | Service/OS acceptance deferred; GUI startup is outside this client task |
| RULE-14 | Diagnostics protect secrets and access scope; IT-25/32 | Redaction, owner binding, expiry and bounds; Q-11 governs transmission/storage |
| RULE-15 | Unsupported/unverified never becomes success; IT-20/30/33 | Explicit failure and partial-status assertions; does not excuse missing required functionality |
| RULE-16 | Existing account limits affect results, no invented tariff rules; IT-02/04/17/30 | Producer entitlements and Q-12; do not introduce client-side product policy |

## Remaining assertion audit

US-04/05/10/11, IT-27: `TestRPCCatalogChangesInvalidateDespiteIdenticalStatus`
checks scoped invalidations for network/peer/exit/preference/managed/resource
projections when the status bytes stay unchanged but the map, policy, trust,
owner or map/exit/application deadline changes. Hashing actual source data also
detects tampering that retains a claimed payload hash. Observer streams receive
no private domain IDs; old owners lose their stream. Duplicate observations
do not advance revision or emit events. `TestRPCCatalogRacePreservesLastAcceptedFingerprint`
checks that a rejected in-flight probe does not consume the pending source
change, and the next current probe publishes it. Root short, vet, configured
lint and goimports passed locally on Windows on 2026-09-14. This establishes
invalidation on observation; an independent host deadline clock is still needed
when there is no network observation. It does not establish route/resource effects.

US-10/12, RULE-06, UI-AC-19/21: `TestRPCUIQuitManagedBaselineLockAndReset`
covers account/device provenance, unlocked override, conflicting lock rejection
without a journal/state change, equal locked override, reset to the managed
baseline after restart, and managed DISCONNECT admission through the existing
durable Down executor before success. `TestRPCUIQuitRejectsInvalidPolicyAndReplaysAcceptedRequest`
covers signature tampering, map expiry, wrong recipient/revision, missing trust,
unsupported managed CONNECT, and a new lock overriding an older local request.
Invalid-policy read/set/reset/new-notify fail without state changes, while a
previously accepted notification replays unchanged before and after restart.
Preference and local-transport fixtures now carry signed maps rather than empty
unsigned placeholders. These assertions do not qualify OS Down, all lifecycle
behaviors, cross-profile policy changes or policy invalidation events.
Local root `go test -short ./...`, `go vet ./...`, configured golangci-lint
and `goimports -w .` passed on Windows on 2026-09-14 after replacing those
fixtures. No integration or platform acceptance run was performed.

Resources / US-11 / IT-30/32: `TestRPCResourceServiceTargetsSearchAndIdentity`
asserts that TCP and UDP on the same port and two TCP ports have distinct IDs,
correct typed targets, and search by name, hostname:port, port and protocol
(including case/whitespace normalization and no-match). IDs remain stable across
filters, port ordering and service rename; reads preserve the signed store and
do not claim effective enablement. Local root `go test -short ./...`, `go vet
./...`, and configured golangci-lint passed on Windows on 2026-09-14.
This is catalog evidence only. The pinned producer's `ManagedResourceSetting`
uses `(kind, id, cidr)` identity and explicitly shares policy across a service's
ports; mutation must bind every port row to that service policy.
`TestRPCResourcesPreserveExplicitSingleIPSubnet` verifies that signed explicit
subnet references retain /32 and /128 resources alongside host addresses,
ordinary addresses remain host-only, and even managed default routes stay out
of this catalog. Tampered policy, hidden peers and undisclosed prefixes reject
the entire read without exposing resources or mutating the store. Root short,
vet and configured lint passed on Windows on 2026-09-14 after this correction.
Policy application, effects and overlap remain open. Producer behavior and
actual resource access require later evidence.

IT-26 admission assertions inspected: `TestRPCDurableReplayBeforeCAS` reopens
the saved config, checks a new instance identity, forbids preparation during
replay, accepts refreshed CAS for the same semantic payload, rejects a changed
payload and rejects a genuinely new stale-instance request.
`TestRPCConcurrentSameRequestHasOneDurableAdmission` adds sixteen synchronized
callers with independently cloned copies of one request. It requires one domain
preparation, one new admission, one journal record, one revision increment and
identical operations for every caller, then replays the result from disk after
restart. This qualifies generic local admission only; per-method preparation,
worker effects, response-loss transport and the full IT-26 outcome remain open.

- Expand each review entry into exact unit assertions and missing branches; do
  not mark a row implemented from a filename or successful package test.
- Reconcile the local v0 requirement variants with these BA/IT rows and the
  [client scenario map](client-runtime-implementation-gaps.md#client-owned-scenario-map).
  This file inventories headless BR/AC/RULE/IT IDs; it is not yet the complete
  assertion-level matrix for all applicable local v0 requirements.
- Implement missing client behavior before the deferred integration phase.
  Keep producer/OS/distribution blockers explicit and do not broaden repository
  write scope to resolve them.
