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
