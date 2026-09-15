# Refactoring audit — 2026-09-15

This cleanup preserves the current CLI/IPC contract and unfinished runtime work
tracked in [runtime gaps](client-runtime-implementation-gaps.md). It does not
establish feature completeness or native/system acceptance.

## Candidate decisions

| Candidate | Decision and evidence |
| --- | --- |
| `positiveIntOr` | Removed. An unexported standalone function in the CLI main package with no references in production code, tests, or platform variants. It is not an interface method, registered callback, or required runtime integration. |
| `nativeRequestUUID`, `validRPCUUID` | Replaced with `internal/rpcutil.ValidUUID`. All CLI mutation/managed/bundle and server mutation/bundle consumers use the shared implementation. Nonzero hyphenated hexadecimal validation still accepts either letter case and does not restrict version or variant. |
| `exitWorkerRetryable` and repeated temporary-failure switches | Replaced with `rpcFailureTemporary`. Only typed UNAVAILABLE, DEADLINE_EXCEEDED and LIMIT_EXCEEDED failures qualify. Preferences still require durable containment; network reconciliation still preserves cancellation; exit retains its own worker lifecycle. |
| Exit worker's classification assertions | Moved into dedicated shared-predicate tests, expanded to every current error enum, wrapped errors, nil, cancellation and untyped transport errors. Native-adapter and containment/recovery tests remain. |
| `startExitWorker` and exit executor/containment code | Retained. The documented native exit integration remains unfinished; tests exercise the intended recovery and containment behavior. |
| `AdoptInitialProfile` | Retained. `StartProfileWorker` calls it before serving; it is active initialization code. |
| `magicBindEndpoint` methods without direct named callers | Retained. The compile-time `conn.Endpoint` assertion and `ParseEndpoint`/`Send` signatures establish interface consumption by WireGuard. |
| Native status/operation helpers and WireGuard observation counters | Retained. Consumers live in `tests`, including native control-plane and installation scenarios; scanning only `internal` would miss them. |

## Boundaries and verification

Reference discovery includes both Go modules, platform-specific source and tests.
Generated IPC interfaces, the Dart bridge, protobuf descriptors, CI tools and
release scripts remain consumers/contract boundaries, not deletion candidates
merely because the native agent does not call them directly. Persisted fields
cannot be classified as unused solely from named Go references.

The baseline root and `clientipc` vet, configured lint and short suites passed.
The lint configuration reports new issues only; it is not a complete dead-code
proof. Final verification uses formatting, vet, the configured lint and short
suites in both modules. Cross-platform short CI is checked after push; privileged,
installer and E2E checks are not run locally.

No schema, dependency or release versions change. Control-plane DTOs and wire
verification stay in the pinned `client-api` module.
