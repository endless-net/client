# Client Protobuf IPC v0

- Status: **accepted**, 2026-09-12.
- Owner: `client`.
- Contract version: **v0**, explicitly selected by the user.
- Additive BA/SA alignment: 2026-09-13 (credential deadlines and initial
  connection/operation snapshot). Version and accepted baseline are unchanged.
- Contract corrections: 2026-09-13 (operation kinds, initial-claim annotations,
  explicit IPv4/IPv6 exit selection and per-family application state).
- Canonical source: [service.proto](../proto/client/v0/service.proto),
  [common.proto](../proto/client/v0/common.proto),
  [runtime.proto](../proto/client/v0/runtime.proto),
  [features.proto](../proto/client/v0/features.proto).
- Package: `client.v0`; service: `client.v0.ClientService`.
- Input: [Client UI business analysis, main](https://github.com/endless-net/architecture/blob/main/docs/ru/client-ui-business-analysis.md),
  read on 2026-09-12.

This is the accepted RPC specification for the analyzed functions. Contract
acceptance is separate from implementation and release acceptance. Existing HTTP IPC is still defined
by [the active OpenAPI v2 contract](client-ipc-v2.openapi.yaml). Protobuf v0 is a
new protocol identity, not a lower compatible revision of that HTTP protocol.
No fallback or simultaneous production protocol support is specified.
The runtime, CLI, privileged helper and UI have not been migrated.

## Ownership and structure

| File | Responsibility |
| --- | --- |
| `proto/client/v0/common.proto` | Caller access, capabilities, errors, operation lifecycle, pagination |
| `proto/client/v0/runtime.proto` | Status, enrollment, profiles, network/peer state, diagnostics |
| `proto/client/v0/features.proto` | Exit nodes, preferences, resources, policy, lifecycle, update/support projections |
| `proto/client/v0/service.proto` | RPCs, requests/responses, access annotations, event stream |
| `buf.yaml` | Schema lint/build configuration |
| `contracts/proto-baseline/client.binpb` | Accepted v0 descriptor used for breaking-change detection |
| `.github/workflows/protobuf-contract.yml` | CI formatting, lint, descriptor compilation and mandatory breaking check |

Generated Go bindings are committed in the separate [clientipc module](../clientipc/README.md):
`github.com/endless-net/client/clientipc/v0` contains messages and
`clientipc/v0/clientipcconnect` contains Connect client/handler bindings.
The [Dart package](../packages/client_api/README.md) contains Protobuf messages
and gRPC bindings and exports `package:endlessnet_client_api/client_api.dart`.
Both use [buf.gen.yaml](../buf.gen.yaml) and must be consumed from pinned
immutable source. Generator pins match the Management pipeline:
protoc-gen-go 1.36.10, protoc-gen-connect-go 1.19.1, protoc_plugin 25.0.0.
Run `buf generate`, `buf build -o clientipc/rpc/client.binpb` and
`goimports -w clientipc` from the repository root. The embedded descriptor is
the runtime digest source, not the frozen compatibility baseline.
Then run `go run ./internal/dartmetadata` from `clientipc` to regenerate the
exported Dart `ClientContract` pairing metadata from that same descriptor.
CI regenerates both SDKs, checks tracked and untracked changes, tests Go unary
and streaming calls over Connect/gRPC, and analyzes Dart with locked dependencies.
Control-plane DTOs remain owned by their existing producer modules; these
messages are local UI projections and do not duplicate signed wire formats.

The accepted baseline is `contracts/proto-baseline/client.binpb`.
CI always runs `buf breaking proto --against contracts/proto-baseline/client.binpb`
using `breaking: FILE`. A missing or invalid baseline fails the job; comparison
is never skipped. A negative CI fixture removes GetStatus and verifies that the
gate rejects RPC deletion after the modified schema successfully compiles.

Ordinary schema changes must not regenerate the baseline merely to make CI pass.
An intentional hard cutover requires an explicit decision and a reviewed baseline
update. No legacy behavior or fallback is required by this check. New versions
need separate explicit authorization. Buf configuration format `v2` is unrelated
to IPC version v0. CI descriptors are build evidence, not release acceptance.
Buf's PACKAGE_VERSION_SUFFIX lint rule requires a nonzero major version, so it
is excluded for the explicitly selected v0 package. All other STANDARD lint
rules and FILE breaking rules remain enabled.

## Transport and identity

The desktop binding is gRPC over local unencrypted HTTP/2, using OS-authenticated
Windows named pipes and Linux/macOS Unix sockets. The producer-owned
`clientipc/local` Go package supplies the listener and generated client;
`clientipc/rpc` enforces annotated access through a runtime authorization callback
and exact protocol metadata. No TCP listener or HTTP/1 fallback is created.
The Unix runtime/package owner must create a protected parent directory and
assign the socket group; socket permissions are 0660. Stale-socket recovery is
an explicit runtime responsibility, never an arbitrary-path deletion by the SDK.
Go transport tests cover unary and streaming calls. The Dart pipe adapter and
production runtime integration remain cutover work, not established acceptance.

Android and iOS use the same messages through a separately validated native
runtime/VPN-service or extension bridge. They do not assume a desktop daemon,
Unix socket, root access or a desktop privilege helper. Capabilities distinguish
unsupported platform behavior from policy denial, missing OS permission and
temporary unavailability. A capability must remain unavailable until its provider
and platform adapter exist. Missing capability entries mean unsupported.

`GetRuntimeInfo` is the bootstrap call. `protocol` is
`endlessnet-client-ipc`, `ipc_version` is 0 and `contract_sha256` identifies the
exact published descriptor. All subsequent RPCs require exactly one value for
each metadata header: `X-EndlessNet-IPC-Protocol: endlessnet-client-ipc`,
`X-EndlessNet-IPC-Version: 0`, and `X-EndlessNet-IPC-Contract-SHA256` matching
the descriptor digest. Authorization precedes metadata validation; authenticated
bootstrap alone may omit these headers to diagnose mismatched installation.
Reject mismatches before side effects. A generated default
zero does not prove successful negotiation: protocol, digest and instance ID must
all be nonempty and valid. Version ranges in update metadata describe pairing
only; they do not enable negotiation to HTTP v2.

## Access and disclosure

Every RPC has an `(access)` annotation. This is a normative minimum role;
generated code alone does not enforce it. Implement a server interceptor plus
domain checks. The peer's OS identity or authenticated mobile bridge identity
determines access. No request supplies a role, UID/SID, IPC endpoint, state path,
shell command, private key or arbitrary executable.

- Observer: sanitized active status, capabilities, product information and
  filtered events. Do not disclose account/profile identity, browser approval
  URLs, peer addresses, session deadlines or owner operations to other users.
- Owner: installation owner and device administrator, limited to the caller's
  profiles and already granted account/network permissions.
- Administrator: fixed-purpose trust and local-forget operations. On mobile,
  advertise these only after an equivalent secure authorization design exists.

For a fresh unowned installation with no enrollment, initial `CreateProfile`
or `Enroll` may atomically claim ownership from the authenticated peer. An
existing ownerless enrollment requires an administrator. Concurrent claims must
have one winner. Ownership is retained after logout and local forget.

Only methods annotated `allows_initial_ownership_claim = true` may use that
exception; both still require an authenticated local peer. The annotation is
permission to evaluate the exception, not an authorization grant. The runtime
must atomically recheck that owner and enrollment are absent together with
persisting the winning owner and accepted mutation. A losing different identity
receives OWNER_REQUIRED; it cannot observe the winner's operation. A retry from
the winning identity follows normal durable deduplication. Unannotated methods
never claim ownership. Existing ownerless enrollment cannot use this exception.

Responses are caller-filtered before serialization, including nested status,
diagnostics, operations and streams. Unauthorized lookup returns NOT_FOUND.
Observer event streams receive neither session payloads nor operation updates.
No account, network, resource, policy or exit-node hosting administration is
introduced.

## Mutations, consistency and operations

All mutations require a nonempty UUID `request_id`, `expected_instance_id`
and nonzero `expected_revision` from an authorized fresh snapshot. For an
inactive profile the owner can obtain that revision through `ListProfiles` or
a profile read. Snapshot revision is a monotonic runtime-wide state revision,
not a map revision; instance ID prevents ABA after restart.

The server checks authorization first, then durable deduplication, then revision,
capability, policy and domain preconditions. Repeat of the same request ID and
semantic payload returns the same operation, even after restart or revision
change. Reuse with a different RPC or payload fails INVALID_ARGUMENT. Request
and operation records remain queryable for at least 24 hours after terminal
completion; waiting/running records cannot expire. Sensitive enrollment tokens
must not be stored in plaintext in deduplication records.

`GetOperation` accepts either operation ID or original request ID. It recovers
a command whose acceptance response was lost. Caller timeout or stream disconnect
does not cancel accepted work. Operation acceptance and requested state must be
durable before returning; crash recovery reconciles them. No generic cancellation
RPC is specified because remote revocation, trust and context switching cannot
always be safely rolled back.

`PENDING -> RUNNING -> [WAITING_FOR_USER -> RUNNING] -> terminal`.
Terminals are SUCCEEDED, FAILED or CANCELLED (provider/OS cancellation).
WAITING_FOR_USER has a typed `UserAction`. Nonterminal operations have no outcome;
FAILED/CANCELLED have `Failure`; SUCCEEDED has exactly one success outcome.
Terminal outcome and actual continuity are immutable. Success means the requested
effect is achieved, not merely that a command was queued. Profile/network/exit
switches and renewal report PRESERVED, INTERRUPTED, UNKNOWN or NOT_APPLICABLE;
they must never infer uninterrupted access from RPC success.

Every mutation RPC has an `operation_kind` annotation matching one distinct
`OperationKind`. The runtime sets `Operation.kind` before durable acceptance;
it remains immutable in responses, GetOperation, current_operations and events,
including inactive profiles and restart recovery. UNSPECIFIED and unknown kinds
are not valid producer outputs. UI must not infer the kind from operation ID,
outcome, active profile or the last command it remembers sending.

Mutations affecting the active context serialize. While enrollment, renewal,
trust or a switch is pending, conflicting mutations fail BUSY. Explicit
disconnect must remain possible and prevents a pending operation from restoring
connected intent. Enrollment may be pending approval, with status and an operation
showing the action until confirmed.

| Mutation family | Success outcome |
| --- | --- |
| Enroll | `EnrollmentResult` |
| CreateProfile, SelectProfile, SelectNetwork, SelectExitNode, ClearExitNode | `SelectionResult` (new/selected ID; empty only on clear exit node) |
| Logout, ForgetLocalEnrollment | `CleanupResult` |
| RenewSession | `RenewalResult` plus continuity |
| CreateDiagnosticsBundle | `BundleResult` |
| Other mutations | `ChangeResult` |

## Profile, recovery and routing semantics

Status.connection_phase reports disconnected/connecting/connected/disconnecting
independently of desired intent and authorization state. A newly attached UI
must not infer Connecting solely from desired_state. On blocked/error states,
the UI prioritizes the service/control reason over the progress indication.
UNSPECIFIED means unavailable information, never connected.

Status.current_operations contains all nonterminal operations visible to the
owner, including inactive profiles. The first stream snapshot and GetStatus
include this list atomically with connection_phase. Its entries have profile IDs.
There are at most 32 nonterminal operations per installation; new commands beyond
the bound fail LIMIT_EXCEEDED without side effects, while Disconnect remains
available. The runtime serializes/coalesces Disconnect to honor the bound.
Terminal transitions remove the entry, emit operation_changed and update status.
An observer receives the connection phase but no operation list or credential
deadlines. A reconnect refetches persisted terminal outcomes by saved request ID.

Status.credential describes node-credential expiry independently of Session.
Absent expiry is unknown and cannot be replaced with the session deadline.
warning_at must not exceed expires_at. Credential renewal is runtime-owned:
RenewSession renews the user session and never claims to renew a node credential.
Expose automatic renewal support, recovery restriction and the associated
operation ID when one exists. Authoritative credential changes update status.
No credential or session deadline may be inferred from UI wall-clock defaults.

A profile is one immutable control origin plus a locally stored account identity,
registration, selected network, requested preferences and connection intent.
At most one profile is active; at most one tunnel context may apply routes.
Creating a profile creates an empty inactive context. `Enroll` binds an empty
profile to an authorized identity; account identity cannot be edited by renaming.
An inactive profile must be selected before enrollment, connection, renewal,
network/exit selection, resource changes or preference application.

`SelectProfile` stops the old tunnel before applying the new context. On failure,
no mixed routes/credentials may remain; report the actual active profile and
safe disconnected state. No automatic return to the previous profile is promised.
A profile's connected intent is restored only subject to current platform and
managed restrictions.

`RemoveProfile` rejects an active profile (PROFILE_ACTIVE), and rejects any
remaining registration/session (REMOTE_CLEANUP_REQUIRED). Logout must first
confirm remote cleanup. If unavailable, it preserves local registration and
returns a failed operation. Only explicit administrator
`ForgetLocalEnrollment(confirmed=true)` performs local-only cleanup, reports
REMOTE_UNCONFIRMED and preserves the control-plane correlation ID.
Apply the [existing cleanup matrix](client-ownership-recovery.md) per profile;
installation ownership and installation keys are retained.

Trust requires exact control origin, key ID and announcement ID comparison.
Persist trust and the recovery operation atomically before network recovery.
Apply the existing typed terminal/retryable recovery rules. A supplied boolean
does not replace OS privilege verification.

Exit-node choices and resources are already authorized catalog entries.
Selection cannot grant access, advertise routes or turn this device into an exit
node. Exit selection separates requested and effective IDs and LAN behavior.
A selected but unreachable exit remains fail-closed; never silently route
protected traffic through the local default route. Clear is an explicit,
policy-checked operation. Report ApplyState and typed failure on apply errors.

SelectExitNode requires an explicit `family_mode` from the selected node's
`allowed_family_modes`. The catalog is already filtered by platform/provider and
policy; an empty list means no selectable mode. UNSPECIFIED, NONE and unknown
request enum values fail INVALID_ARGUMENT. A known mode absent from the catalog
is rejected using its actual capability/policy restriction, never downgraded.
IPV4_ONLY protects IPv4, IPV6_ONLY protects IPv6, DUAL_STACK protects both.
The unselected family follows ordinary effective routing/policy; the UI must
explicitly disclose that it is not protected by the selected exit node.

GetExitNode always includes `ipv4` and `ipv6` states. Each carries optional
requested/effective exit IDs, apply state, failure and actual fail-closed
enforcement. Absence of an ID means no exit selection for that family, not
unknown status. For a disabled/cleared family both IDs are absent and APPLIED
means its ordinary policy has actually been restored. NONE is the cleared
requested mode; UNSPECIFIED is not a valid producer status.

Aggregate requested ID/mode express intent. Aggregate effective ID is present
only when every requested family has converged to that node and every excluded
family has cleared its prior exit. Aggregate APPLIED additionally requires LAN
policy convergence; otherwise FAILED takes precedence over PENDING. Per-family
state remains authoritative for partial application. SUCCEEDED requires full
convergence, never IPv4-only success for a dual-stack request. On path loss each
selected family must block fallback before reporting fail_closed; inability to
enforce blocking is a failure, not a protection claim. The aggregate fail_closed
is true only with nonempty selection and protection for every requested family.
Clear removes both families; partial clear must remain visible as pending/failed.

Overlaps expose only caller-visible resource IDs; conflicting enablement fails
RESOURCE_CONFLICT instead of silently choosing a route.

## Preferences and platform lifecycle

Boolean and lifecycle settings carry effective value, optional user request,
source, lock, reason and next-action owner. The runtime resolves platform,
device and account constraints; user preferences may only narrow the permitted
behavior. UI cannot mutate policy. Conflicting authoritative constraints fail
closed with a policy reason rather than guessing precedence.

SetPreferences requires at least one present field. Omission leaves a setting
unchanged; ResetPreferences removes explicit overrides for a nonempty set of
unique keys. Explicit false is different from absence. UNSPECIFIED enum inputs
are invalid. All fields are authorized and validated before persisting the
requested patch. On apply failure, report FAILED, retain observable requested
versus actual effective values and reconcile safely; never claim complete apply.
Platform-impossible combinations fail UNSUPPORTED.

Runtime start, graceful UI quit, user logoff, suspend and resume have separate
effective behaviors. Runtime start also covers start after reboot. Actual
logoff/suspend/resume come from OS adapters. NotifyLifecycle accepts UI_QUIT only
from the owner; loss/crash of a UI connection is not a quit command.
UI autostart, tray behavior, theme, localization, accessibility, notification
preferences and mobile navigation remain UI-owned.

## Streams, pagination and bounds

WatchEvents starts with sequence 1 containing runtime info and a full authorized
status snapshot. Subsequent events contain full status/session/operation payloads
or typed domain invalidations. Include the current metadata on every event.
Sequence is per stream, strictly increasing, with no replay/resume token.
On reconnect discard cached domain data, consume a fresh snapshot and refetch
visible domains. A profile switch invalidates all profile-scoped UI data.
Consumers refetch on resume after mobile suspension.

Slow subscribers are disconnected with LIMIT_EXCEEDED, never silently dropped
events. Queue limit is 64 events or 8 MiB, whichever is reached first.
Domain invalidations may be coalesced before assigning sequence numbers.
Revision changes in capabilities require a new SnapshotEvent.
Owner operations can always be recovered by GetOperation after reconnect.

Pages default to 100 items, maximum 500. Tokens bind caller, query, profile,
instance and snapshot revision and expire after 5 minutes. Stale tokens fail
STALE_STATE; restart the query. Searches are case-insensitive substring matches
against disclosed names/addresses, limited to 256 UTF-8 bytes. IDs are opaque
nonempty values of at most 256 bytes; display names are 1..128 UTF-8 bytes.
No search reveals a hidden resource's existence.

Mutation messages are at most 64 KiB serialized, excluding transport framing.
Unary responses and individual events are at most 4 MiB; paginated services
may return fewer entries than requested to honor this bound. Diagnostics alone
may be truncated, with `truncated=true`; do not silently truncate normal lists.
Bundle max is 5 MiB, expiration 15 minutes, read chunks default 64 KiB/max
256 KiB. Bundle handles are owner/profile-bound; offset must be within the
immutable artifact. Logout/removal invalidates its handles. No arbitrary paths,
upload, packet capture or configurable tracing are added.
Redact before serialization/archive generation, including logs and browser URLs.

## Errors and validation

Failures before acceptance are RPC errors with typed Failure details. Failures
after acceptance appear in Operation.outcome; the transport may still succeed.
Domain failure codes replace HTTP parsing/header errors; no legacy text-based
mapping is specified. Failure.reason_key is for localization, not a diagnostic
message. Unknown codes are treated as failure and never authorize cleanup.

| Failure | RPC code |
| --- | --- |
| INVALID_ARGUMENT | invalid_argument |
| UNAUTHENTICATED | unauthenticated |
| OWNER_REQUIRED, ADMINISTRATOR_REQUIRED, POLICY_BLOCKED, PERMISSION_REQUIRED | permission_denied |
| NOT_FOUND | not_found |
| UNSUPPORTED | unimplemented |
| STALE_STATE, BUSY | aborted |
| LIMIT_EXCEEDED | resource_exhausted |
| UNAVAILABLE | unavailable |
| DEADLINE_EXCEEDED | deadline_exceeded |
| CANCELLED | cancelled |
| INTERNAL, APPLY_FAILED | internal |
| Remaining domain/state/precondition codes | failed_precondition |

Proto3 cannot enforce semantic required fields. Runtime must validate nonempty
references, allowed enum values, required oneofs, timestamp ordering, port ranges,
CIDRs and origins. `browser_login` and `confirmed` must be true when selected.
Resource kind must match its target oneof. Browser URLs must be HTTPS and validated
against the profile's trusted origin/provider policy; support/update links must
come from trusted configured sources. Tokens and browser actions never belong
in diagnostics or notification bodies.

## Updates and distribution

GetUpdateInfo exposes installed runtime identity and, when available, a verified
projection of an authoritative signed distribution manifest. Missing/verifiably
invalid metadata yields UNKNOWN/SOURCE_UNAVAILABLE/VERIFICATION_FAILED, never
UP_TO_DATE. Update availability requires a verified manifest for the exact
platform/architecture/channel and UI/runtime pair. Available metadata is omitted
on verification failure. Caller-reported UI identity is not attestation.

Installation, download execution, repair, removal, update application and their
terminal outcome belong to the package/store/distribution owner. There is no
InstallUpdate RPC, arbitrary download URL or ability to run an installer through
the daemon. A mandatory classification does not itself authorize forced install
or disconnection. After external update, UI reconnects and verifies installed
identity and pairing again. Runtime restart/identity observation alone cannot
prove installer success or rollback; that requires the distribution contract.

## Business-function coverage

All rows are covered by the **accepted specification**, not implemented or
accepted for release.

| Function | Proposed coverage / owning boundary |
| --- | --- |
| UF-01 installation | Package/store contract; GetRuntimeInfo for installed identity |
| UF-02 native shell | UI-owned; GetStatus, WatchEvents |
| UF-03 enrollment | CreateProfile, Enroll, GetOperation, Status.pending_action |
| UF-04 status | GetRuntimeInfo, GetStatus, WatchEvents |
| UF-05 connect/disconnect | Connect, Disconnect, ConnectionIntent, Operation |
| UF-06 peers and paths | ListPeers, AgentStatus, PathCandidate |
| UF-07 network selection | ListNetworks, SelectNetwork |
| UF-08 exit-node use | ListExitNodes, GetExitNode, SelectExitNode, ClearExitNode |
| UF-09 recovery | Status.recovery/session/pending_action, GetOperation, WatchEvents |
| UF-10 identity trust | GetServerIdentity, TrustServerIdentity, privileged platform adapter |
| UF-11 diagnostics | GetDiagnostics, CreateDiagnosticsBundle, ReadDiagnosticsBundle, ListRecentLogs |
| UF-12 logout/forget | Logout, ForgetLocalEnrollment, CleanupOutcome |
| UF-13 update/repair/uninstall | External distribution lifecycle; GetUpdateInfo/GetRuntimeInfo support observation |
| UF-14 localization/accessibility | UI-owned; stable enums/reason keys/action owners |
| UF-15 notifications | WatchEvents, Session.warning_at, domain invalidations; UI-owned policy and presentation |
| UF-16 profiles | List/Create/Select/Rename/RemoveProfile, per-profile enrollment and cleanup |
| UF-17 session renewal | GetSession, RenewSession, RenewalResult and actual continuity |
| UF-18 preferences | Get/Set/ResetPreferences, requested/effective values and apply outcome |
| UF-19 resource browser | ListPeers, ListResources, SetResourceEnabled, bounded search/pagination/overlap |
| UF-20 managed settings | ListManagedSettings, SettingControl, CapabilityStatus |
| UF-21 runtime/UI lifecycle | RuntimeLifecycle, typed preference patch, NotifyLifecycle; UI shell stays UI-owned |
| UF-22 update/compatibility | GetUpdateInfo, signed-manifest projection, GetRuntimeInfo; external installer outcome |
| UF-23 help/about | GetSupportInfo, BuildIdentity, offline_help_key |

## Adoption and follow-up owners

- **client**: select transport; integrate generated Go bindings, interception,
  persistence/deduplication, RPC adapters and capability providers; migrate CLI
  and helper; test authorization, stream reconnect, interruption, crash recovery,
  cleanup and requested/effective divergence. Current code does not implement
  this surface. CI schema compilation is not runtime revalidation.
- **client-ui**: consume pinned generated Dart messages, implement desktop/mobile
  bridges and UX, protect disclosure, refresh invalidated data, recover operations
  by request ID; implement UI-only functions and distribution outcome handling.
- **coordinator / identity / management / billing**, as applicable: establish
  authoritative exit-node/resource visibility, renewable-session and policy data.
  This schema is not evidence those provider capabilities exist.
- **client / client-ui / releases**: define signed update source ownership,
  pairing and installer outcome contract before enabling update discovery.
- **system-tests**: validate pinned UI/runtime/backend artifacts on accepted
  platforms, including actual routing/renewal continuity and negative access cases.
- **architecture**: maintain cross-repository decisions and links once transport,
  mobile ownership, release scope and distribution contracts are accepted.

Open decisions include concrete RPC transport and mobile binding, first-release
capabilities, managed-policy sources, external update outcomes, and generated SDK
publication. The schema provides explicit unavailable states while these are
unresolved. File transfer, local web UI, SSH and exit-node hosting remain outside
this BA and this contract.
