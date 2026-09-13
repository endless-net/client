# Headless requirements: client implementation and unit audit

Status: open implementation audit, not integration execution or acceptance.

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
| IT-21 | Durable renewal identity after response loss/restart | C `service_rpc_credential_test.go` for credential-side review only | Session renewal runtime missing; updated clientapi consumption needs approval; new traffic is later evidence |
| IT-22 | Stop/switch invalidates delayed old responses | I `service_rpc_connect_test.go`; `service_rpc_switch_test.go` | Audit all context switches; preserve SA G-04 constraint |
| IT-23 | Distinguish remote logout confirmation from local forget | I `service_rpc_logout_worker_test.go`; `service_rpc_forget_test.go` | Verify every transport/terminal result; backend cleanup cannot be inferred from local state |
| IT-24 | Respect node revocation and remaining lease limits | I `map_expiry_filter_test.go`; C `recovery_test.go` | Node deletion is backend-owned; online/offline real-pair checks deferred |
| IT-25 | Consent-bound flow collection and replay windows | I `flow_logs_test.go` | Audit durable accepted ID and duplicate-window prevention; backend receipt external |
| IT-26 | Mutation idempotency, conflict and lookup after restart | I `service_rpc_test.go`; `service_rpc_restart_test.go` | Audit every mutation kind, not only generic admission |
| IT-27 | Snapshot-first events and observer privacy | I `service_rpc_events_test.go`; `service_rpc_events_role_test.go` | Audit concurrent transitions and owner operation visibility individually |
| IT-28 | Profile context isolation and removal guards | I `service_rpc_profiles_test.go`; `service_rpc_switch_test.go` | Cross-network/provider switching incomplete; actual route isolation deferred |
| IT-29 | Independent session and node-credential clocks | C `service_rpc_credential_test.go`; `service_rpc_recovery_status_test.go` | Session Get/Renew runtime missing; unknown deadlines must remain unknown |
| IT-30 | Exit apply/clear, locked settings and resource conflicts | I `service_rpc_preferences_test.go` covers validation only | Exit/resources runtime and remaining policy settings missing; backend contract dependency |
| IT-31 | UI quit versus crash; OS lifecycle and stream recovery | I `service_rpc_uiquit_test.go`; `service_rpc_events_test.go` | Logoff/suspend/resume adapters incomplete; do not add untrusted OS events to UI notification RPC |
| IT-32 | Private diagnostics access, bounded export and resource context | I `service_rpc_bundle_read_test.go`; `service_rpc_bundle_admin_test.go` | Resource search runtime missing; independently audit every bundle bound and authorization case |
| IT-33 | Contract mismatch and honest update/distribution status | `clientipc/rpc/protocol_test.go`; I `service_rpc_update_test.go` | Verified update source absent; unavailable-path tests do not prove installation or release pairing |

## Next audit work

- Expand each review entry into exact unit assertions and missing branches; do
  not mark a row implemented from a filename or successful package test.
- Reconcile BA requirement/rule/acceptance IDs with these IT and the
  [client scenario map](client-runtime-implementation-gaps.md#client-owned-scenario-map).
  This file covers the IT inventory, not all BA/SA numbered requirements.
- Implement missing client behavior before the deferred integration phase.
  Keep producer/OS/distribution blockers explicit and do not broaden repository
  write scope to resolve them.
