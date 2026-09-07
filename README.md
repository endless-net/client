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
