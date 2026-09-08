# Client control-plane test environment

The client owns `internal/testcontrol`, a Go fake server built with `httptest`
and the pinned Client API v1.12.0 DTOs, signing functions and Connect handlers.
It owns no production backend packages. `internal/testclient` drives a separately
built client executable through CLI and Unix IPC on an isolated CI runner.

## Server controls

Create a server with `testcontrol.New(t)`, add a network with `AddNetwork`, and
use its returned join token or `SessionToken()` to authorize enrollment. Give
the client `Trust()` through its existing `--map-signing-trust-file` option.
Every server generates an independent signing key and registers cleanup.

`UpdateMap` accepts a copied projection, validates it, signs it and wakes streams.
`DecideEnrollment` approves/rejects browser enrollment without opening a browser.
`Revoke`, `SetUnavailable`, `FailNext`, `BreakStreams` and `FaultNextMap` exercise
credential revocation, temporary errors, reconnect and untrusted map handling.
`Snapshot` returns a copy. `Await` waits for a redacted event with a context.
Do not change the server from inside an `UpdateMap` callback.

The server implements current HTTP registration, renewal, enrollment polling,
completion, server-key, endpoint, map stream, node deletion and session logout.
Streams negotiate the current protocol and send signed snapshots or resyncs;
this first implementation does not emit deltas. Current cursors wait for a change
or the requested timeout. Clients still perform their normal wire and signature
validation. Wire-only faults never overwrite the server's valid map.

Connect handlers expose a single test account and network/advertised-route reads.
Node RPCs use bearer node credentials, independently of HTTP node headers.
Discovery requires the current application policy and an explicit connector.
Flow consent is off by default; `SetFlowConsent` enables a bounded interval.
Accepted windows are retained in memory and retries must preserve the payload.
Unsupported billing RPCs return `unimplemented`. This is deliberately not an
implementation of production membership, billing, storage or policy evaluation.

## Tests and execution

`go test -short ./...` includes host-safe server tests: real SDK/Connect calls,
request bindings, idempotency, renewal/revocation, browser approval, signed map
updates, bad signatures, unknown keys, expired maps, cancellation and concurrency.

The `Client control-plane scenarios` job in `.github/workflows/test.yml`:

1. Runs the server suite under the race detector.
2. Builds the real client and the `tests` executable.
3. Runs `TestControlPlane*` three times inside a fresh Linux network namespace,
   with loopback enabled and no production control server.

Process tests require `ENDLESSNET_CONTROL_TEST=1`, `ENDLESSNET_TEST_BINARY`, and
a disposable GitHub-hosted Linux runner. They skip under `-short`. Never run
the privileged suite on a developer host. Each client uses its own state,
trust file, IPC socket and interface. State and private identity files are not
read for assertions; CLI/IPC output is parsed in memory and not dumped on errors.
Processes terminate before temporary resources and the fake server are removed.

Scenarios cover enrollment, peer changes, DNS/route projection through diagnostics,
map cursor recovery, temporary control failures without reenrollment, rejection of
invalid maps with preservation of verified cache, revocation, persistent disconnect
across agent restart and logout cleanup. The browser scenario uses the real `up`
command and explicit approval controls. ACL contents are verified at the signed-map
boundary; kernel ACL behavior is covered by the existing separate ACL tests.

## Evidence boundaries

These tests establish client behavior against a controlled peer, not production
compatibility, OIDC/browser UI acceptance, real Relay/STUN, packet delivery, NAT,
or Windows/macOS networking behavior. Installer tests remain separate.
`system-tests` owns follow-up acceptance against pinned real backend and client
artifacts. No fake-server result replaces that release evidence.

Client v0.5.0 was published before this test environment, at commit
`e1c18c463b65b460faa94c0f2fce431c2ed79259`, with Client API v1.12.0:
[release](https://github.com/endless-net/client/releases/tag/v0.5.0),
[platform and installer CI](https://github.com/endless-net/client/actions/runs/34195857477),
[core publication](https://github.com/endless-net/client/actions/runs/34196119110).
APT publication is separate; its initial
[run](https://github.com/endless-net/client/actions/runs/34196119134) failed at push
because the deploy key lacked write access. Creating a replacement key was blocked
by repository policy. The core release does not imply successful APT publication.
