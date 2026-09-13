# Client v0 runtime gaps — 2026-09-13

Status: incomplete implementation audit, not runtime acceptance. Source:
[client main at cba6c69](https://github.com/endless-net/client/tree/cba6c6900be75dff0c679fd400d800d03333b81f).
This snapshot distinguishes the published contract and consumer/mock coverage
from real `ClientRPCService` implementation. It does not reduce the agreed scope.

## Methods without a runtime override

The [service contract](../proto/client/v0/service.proto) declares these methods,
but `ClientRPCService` has no override in the inspected source. They are inherited
from the embedded generated `UnimplementedClientServiceHandler` in
[`service_rpc_handlers.go`](../internal/client/service_rpc_handlers.go).
The RPC guard maps unimplemented failures to the native unsupported category;
caller admission remains separate. A generated client or testserver response is
not evidence that the production runtime implements a method.

| Runtime gap | Consumer scenario | Required producer outcome |
| --- | --- | --- |
| `GetSession`, `RenewSession` | US-09 expiry/renewal | Authoritative profile session projection and durable renewal, independent of node-credential deadlines; cancellation, failure and restart evidence |
| `ListExitNodes`, `GetExitNode`, `SelectExitNode`, `ClearExitNode` | US-05 exit node | Authorized catalog, requested/effective IPv4/IPv6 state, policy restrictions and durable apply/clear; actual route/traffic and fail-closed checks |
| `ListResources`, `SetResourceEnabled` | US-11 resources | Authorized bounded catalog and policy-aware mutation, invalidation and conflict handling; resource availability must follow real runtime observations |
| `GetUpdateInfo` | US-13 distribution | Verified update metadata and compatibility projection; source failure must not mean up-to-date, and projection alone does not prove installation |

There are nine missing overrides in this snapshot. Presence of overrides for
other methods does not imply that their complete functional or platform scope
is implemented or accepted.

## Partial implementations requiring further work

- `SelectNetwork` implements only exact current-ID reselection as a durable
  no-op. Another network remains unsupported; enrollment/switch/rollback and
  truthful connectivity continuity still require implementation. See the
  [network catalog boundary](native-network-catalog.md).
- `SetPreferences` and `ResetPreferences` accept only the UI-quit key;
  `GetPreferences` projects that subset. `NotifyLifecycle` accepts only UI_QUIT.
  See [`service_rpc_uiquit.go`](../internal/client/service_rpc_uiquit.go).
  The other preferences and OS lifecycle events need real providers and tests,
  not fabricated defaults or successful no-ops.
- Desktop transport tests do not prove Android/iOS native adapters, installed
  services, permissions, suspend/resume, screen-reader behavior or traffic.
- Remaining HTTP IPC v2 handlers, DTOs and tests still need removal/migration.
  Native hosting without fallback is not equivalent to a legacy-free repository.

## Evidence needed to close gaps

`client` owns runtime providers, durable execution, producer tests and desktop
runtime/platform evidence. `client-ui` owns UI binding, shared consumer tests and
mobile/native UI adapters. Consumer fixtures must model unsupported, partial and
failure states as well as planned successful behavior; they must not turn a mock
success into a claim of runtime availability. Keep all UF/UBR/UI-AC requirements
and US-01–14 in the client-ui trace; no scenario is accepted solely by this audit.

For each implemented gap, verify authorization, profile/context binding,
idempotent mutation/recovery where applicable, cancellation, events and truthful
requested/effective state. Then collect exact-source GitHub runner evidence at
the appropriate scope. Simulator/emulator, mock and actual runtime evidence
remain separate. Versions stay unchanged; infrastructure, production and new
release/version actions still require their respective explicit authorization.
