# Native CLI catalogs and diagnostics

`service preferences` and `service managed-settings` read their native producer
methods with a required profile ID. Output retains protobuf presence, effective
values, setting source, locks and mutation restrictions; the CLI does not invent
defaults or clear policy. Exact request/response local transport fixtures cover
both commands, and missing profile is rejected before connecting. These read-only
commands do not implement runtime preference application or policy administration.

`service set-preferences --patch '<protobuf JSON>'` preserves optional field
presence, including explicit false; absent fields remain untouched. The parser
rejects empty/unknown/duplicate/invalid fields and unknown lifecycle values.
`service reset-preferences --keys accept-dns,ui-quit` removes only those explicit
user overrides; empty, unknown or duplicate keys fail. Both commands require the
normal profile/request UUID/instance/revision CAS and return an operation, not an
assurance that settings were applied. Producer restrictions remain authoritative:
currently only the implemented UI-quit behavior can be changed successfully.
Exact native transport tests cover false-versus-absent and explicit reset keys.

`service session --profile-id <id>` reads GetSession without inferring expiry
from token presence. `service renew-session` sends RenewSession with an explicit
profile, retained request UUID and instance/revision CAS. Acceptance is only an
operation; use `service operation` for its actual outcome. No implicit enrollment,
browser callback, credential replacement or retry is performed. The strict local
fixture verifies exact requests and typed responses; missing profile/CAS fails
before connection. These commands do not imply a live renewal provider: the
current runtime still lacks authoritative backend session expiry/renewal input
and returns an unimplemented failure. Provider integration belongs to client and
the upstream session API owner; expiry must not be guessed from node credentials.

Negative local transport fixtures cover unsupported session/renewal providers,
NEEDS_LOGIN, stale CAS and owner denial. Both the transport code and typed Failure
must survive unchanged, stdout stays empty and the strict request sequence allows
no automatic retry or enrollment. Renewal rejects caller callback URLs, tokens,
enrollment token files and browser-login flags before opening a connection.

The retired HTTP recent-logs route and agent callback are removed. A regression
test requires 404 on that old path. Buffer redaction tests now exercise the buffer
directly; native log transport and profile isolation tests own RPC evidence. The
remaining old diagnostics DTO/buffer consumers and full ipc/v2 deletion are still
pending; this removal does not claim their migration.

`service peers --profile-id <id>` calls native ListPeers with optional `--search`,
`--page-size` and `--page-token`. It preserves the producer's map revision,
snapshot state and cursor in protobuf JSON; it does not flatten peers into an old
status envelope or automatically merge pages. The strict local transport fixture
checks the exact profile/search/page request and a nonempty typed peer response.
Real provider boundaries and pending acceptance are described in
[Native peer catalog](native-peer-catalog.md).

`service networks`, `diagnostics` and `logs-recent` now call ListNetworks,
GetDiagnostics and ListRecentLogs through native bootstrap and local RPC. Each
requires `--profile-id`; networks/logs accept `--page-size` (maximum 500) and an
opaque `--page-token`. Output is the generated protobuf JSON response, not an
HTTP v2 envelope. Paging is explicit; the CLI does not combine stale catalogs.

`service select-network` and `diagnostics-bundle` now require the retained
`--request-id`, `--profile-id`, `--expected-instance-id` and `--expected-revision`.
Selection requires an exact `--network-id`, not positional/name lookup. Bundle
creation returns an operation, not a filesystem path. Recover its outcome with
`service operation`; CLI bundle streaming/export remains follow-up work.
Neither acceptance nor helper/process exit proves runtime completion.

The old generic HTTP command dispatcher and its transport-selection wrapper were
removed. Rewritten short tests exercise all five commands against the strict
producer fixture over an authenticated local pipe/socket, checking exact request
messages, pagination/CAS and output. A typed unavailable error is returned with
no success output and no replay/fallback. Missing contexts and oversized pages
fail before connection. This does not prove actual runtime providers, network
switching or bundle export. Unimplemented runtime capabilities still fail closed;
remaining legacy core handlers/consumers and ipc/v2 deletion are separate work.

## Native operation wait hardening

Waiting now handles CANCELLED as a terminal unsuccessful operation, rejects a
missing GetOperation response/message/operation without panicking, and stops
before reporting/polling an already canceled context. Accepted operations and
reporter values are cloned so output callbacks cannot rewrite the retained
lookup identity or the caller's input. Short tests cover these negative outcomes,
one terminal cancellation lookup and reporter mutation without command replay.
The now-unused legacy IPC client factory is removed. Other legacy DTO users and
runtime/test harness migration remain outside this completed CLI wait change.

## Managed enrollment-to-connect binding

`up` now rejects cross-profile/cross-instance plans and validates each accepted
or polled operation against the retained request ID, kind, profile and instance.
Revisions must not regress, including relative to prior progress. Successful
enrollment must identify the expected profile and a nonempty enrolled node before
its revision can become Connect CAS. Nil acceptance responses fail without panic.
Inputs are cloned and cancellation is checked before the first mutation.

Short tests cover mismatches at enrollment acceptance, polling and connection
acceptance; missing responses, malformed enrollment outcomes, regressing revision,
and invalid plans. Invalid enrollment never dispatches Connect and neither
mutation is replayed. These consumer tests do not validate signed node material
or real tunnel continuity; those remain producer/system acceptance work.

## Native recent-log query and transition source

The runtime service now implements ListRecentLogs through an explicitly
profile-scoped `ClientRPCRecentLogsProvider`. It authorizes before collection and
again after collection, rejects a changed configuration, honors cancellation,
and never passes configuration or credentials to the provider. Unknown protobuf
fields are discarded and messages pass through producer redaction. Responses
accept at most 500 chronological entries of at most 4096 UTF-8 message bytes;
invalid timestamps/text/order and oversized snapshots fail rather than silently
truncating (ListRecentLogs has no truncation indicator).

Page cursors bind the caller, profile, instance, config revision, page size and
the complete redacted log snapshot. Append/rotation invalidates a continuation
with STALE_STATE, even without a config revision change. Consumers can explicitly
restart from the first page; the server never silently combines snapshots.

Short tests cover admission, provider failure sanitization, cancellation,
ownership/revision changes during collection, snapshot ownership, empty/final
pages, changed callers/page sizes/buffers, redaction and malformed/oversized data.
This is handler-level evidence, not installed-agent or cross-platform acceptance.

The default native service now uses a process-local diagnostic transition source,
so the agent's ListRecentLogs no longer returns UNSUPPORTED. Committed operations
and accepted status observations create profile-attributed records even when no
UI is subscribed. Replayed/rejected mutations and unchanged/rejected observations
do not produce duplicate successful-transition records. Only enum type/state,
continuity and failure code are formatted; names, request payloads, browser URLs,
identity material and arbitrary error strings never enter this source.

Retention is the most recent 500 records across all profiles for this process,
filtered by the requested profile and ordered oldest-first. Deleted profiles are
purged on publication. Restart clears this diagnostic window; durable operation
recovery remains GetOperation, not log parsing. Clock rollback preserves append
order with nondecreasing timestamps. The agent no longer captures the shared
global logger into the retired unscoped IPC buffer. Normal process/debug logging
is unchanged and is not exposed as profile-scoped native records.

Short source tests verify committed creation, replay/rejection, profile isolation
and deletion, status changes, cancellation, clone ownership, bounded retention,
restart and clock rollback. The existing authenticated local transport test also
reads the real default log provider after creation and replay (no scripted log
response). These are local tests, not installed-service or distribution evidence.

**Still incomplete:** this source covers native transitions, not all network,
tunnel, DNS or OS diagnostics. The owning `client` repository must supply those
additional profile-scoped records and complete diagnostics/bundle providers;
`client-ui` must verify refresh after stale cursors against the real provider.
The whole diagnostics capability remains unadvertised until its family is ready.
Raw debug-log redaction and full runtime/system acceptance remain open work.

GetDiagnostics now has a [partial active-profile runtime provider](native-diagnostics-runtime.md)
for native status, transition logs, interface metadata and tunnel summary. Missing
sections are explicit failures with `truncated=true`; this does not complete the
diagnostics family or enable its capability.
