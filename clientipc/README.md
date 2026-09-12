# Client IPC Go API

Producer-owned generated Go module for `client.v0`:

- `github.com/endless-net/client/clientipc/v0`: Protobuf messages and descriptors.
- `github.com/endless-net/client/clientipc/v0/clientipcconnect`: typed Connect
  client and handler interfaces, supporting Connect, gRPC and gRPC-Web.
- `github.com/endless-net/client/clientipc/local`: authenticated local gRPC
  transport for Windows pipes and Linux/macOS Unix sockets.
- `github.com/endless-net/client/clientipc/rpc`: contract digest, strict metadata,
  typed errors and access-annotation guard with a runtime authorization callback.

Source: `proto/client/v0`. Regenerate from the repository root with
`buf generate` and `buf build -o clientipc/rpc/client.binpb`, using the versions
pinned in the Protobuf CI workflow.
Commit generated files with schema changes; consumers pin immutable module
revisions. Do not edit generated Go files manually.

The module does not implement runtime domain behavior or owner policy. Install
`rpc.Guard` with authorization derived from `local.PeerFromContext`; missing
authorization fails closed. Production handlers must also configure message
limits and apply the domain checks required by `docs/client-ipc-protobuf.md`.
