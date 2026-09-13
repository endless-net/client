# Native CLI catalogs and diagnostics

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
