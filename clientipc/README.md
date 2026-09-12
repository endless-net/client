# Client IPC Go API

Producer-owned generated Go module for `client.v0`:

- `github.com/endless-net/client/clientipc/v0`: Protobuf messages and descriptors.
- `github.com/endless-net/client/clientipc/v0/clientipcconnect`: typed Connect
  client and handler interfaces, supporting Connect, gRPC and gRPC-Web.

Source: `proto/client/v0`. Regenerate from the repository root with
`buf generate`, using the generator versions pinned in the Protobuf CI workflow.
Commit generated files with schema changes; consumers pin immutable module
revisions. Do not edit generated Go files manually.

Bindings do not implement the runtime, local transport, authorization interceptor
or the domain checks required by `docs/client-ipc-protobuf.md`.
