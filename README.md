# EndlessNet Client

Cross-platform EndlessNet client core, long-running agent, CLI and service
integration for Linux, macOS and Windows.

The maintained Russian client architecture and operations guide starts at
[`docs/ru/client-and-network.md`](docs/ru/client-and-network.md).

Signed service catalog consumption and its limits are documented in
[`docs/service-discovery.md`](docs/service-discovery.md).

Signed L3 application routes, connector discovery and packet enforcement are
documented in [`docs/application-runtime.md`](docs/application-runtime.md).

Client source is imported through pull requests. Control-plane HTTP and signed
wire contracts are consumed from the producer-owned, versioned
`github.com/endless-net/client-api/clientapi` module.

## Build and verify

```text
go build ./cmd/endlessnet-client ./cmd/endlessnet-client-recovery-helper
go test -short ./...
```

CI also runs [installation and basic service acceptance](docs/installation-tests.md)
on Ubuntu, Windows Server and macOS, including local IPC checks, disconnect
persistence across restart, and service removal.

[Control-plane scenario tests](docs/testing-control-plane.md) run the real client
against an isolated, stateful test server using the pinned Client API contract.

The versioned local service and ownership-recovery contracts are documented in
[`docs/client-ipc-v2.openapi.yaml`](docs/client-ipc-v2.openapi.yaml) and
[`docs/client-ownership-recovery.md`](docs/client-ownership-recovery.md).

The repository owns the client binary, Linux service assets, APT package,
immutable Windows core manifest and the producer side of the versioned local IPC
contract. Browser UI and signed Windows installer packaging remain owned by
`endless-net/front`.

## Release boundary

Semantic `v*` tags publish client artifacts from this repository. Windows core
releases do not trigger UI releases. The control-plane repository consumes
immutable client release artifacts for download and compatibility gates; it
does not rebuild the client.

The release matrix is explicit: GitHub Releases publish Windows amd64 and Linux
amd64 core artifacts with versioned manifests and checksums; the APT workflow
publishes Debian/Ubuntu packages for amd64 and arm64. macOS remains covered by
the source and CI portability contract, but no signed/notarized macOS package is
declared as a release-supported artifact yet.

All Go dependencies used by public CI are available without repository deploy
keys. Pull requests run on standard GitHub-hosted Linux, Windows, and macOS
runners; release and APT credentials are restricted to their dedicated jobs.

## Public Go package

`ipc/v2` is the repository's only public Go boundary. It exposes the strict
local service IPC v2 contract and transport clients. Persisted state, identity,
diagnostics implementation, and STUN implementation are internal details.
Control-plane DTOs and signed wire verification remain in the pinned
`github.com/endless-net/client-api/clientapi` module.

## Unified Client API boundary

All control-plane Go DTOs and clients come from the single
`github.com/endless-net/client-api/clientapi` module: HTTP/recovery contracts in
`v1`, protobuf messages in `v1/clientrpc`, and Connect bindings in
`v1/clientrpc/clientrpcconnect`. The [producer source, main](https://github.com/endless-net/client-api/tree/main/clientapi)
owns these contracts. There are no Management or Coordinator API dependencies.

Account-scoped billing, network and route reads use Client API UserService;
node discovery and flow collection use its credential-bound node services.
Session logout uses Client API `/auth/logout`. The CLI has no `approve-route`
or `revoke-route` commands; use the administration console for route approval.
Login may still open the discovered console URL in a browser.

Registration and recovery share one request, identity proof and public error
contract. Registration uses `schema_version` and `idempotency_id`; the old
`idempotency_key` registration body is rejected. Existing schema numbers are
unchanged. IPC remains a separate local contract.

The client pins the published module `v1.12.0` and builds independently, without
a local workspace or module replacement. Server support and release acceptance
are separate from component checks. See the
[producer cutover requirements, main](https://github.com/endless-net/client-api/blob/main/clientapi/UNIFIED-CONTRACT.md).
