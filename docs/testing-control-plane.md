# Client control-plane test environment

The client owns `internal/testcontrol`, a Go fake server built with `httptest`
and the pinned Client API v1.12.0 DTOs, signing functions and Connect handlers.
It owns no production backend packages. `internal/testclient` drives a separately
built client executable through CLI and public IPC (Unix sockets or Windows
named pipes) on disposable GitHub-hosted runners.

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

The parallel `Client contracts` matrix in `.github/workflows/test.yml` runs
`TestControlPlane*` three times on Ubuntu 22.04/24.04, Windows 2022/2025 and
macOS 15 ARM/Intel. Fail-fast is disabled. Each job preserves sanitized text and
JSONL results; all six jobs are required by verification and publication gates.
Windows uses the same checksum-pinned Wintun dependency as the installer suite.
Windows agent restart in this driver uses process termination; graceful Windows
service-manager restart remains a distinct installation-suite observation.

The `Client control-plane scenarios` job retains the Linux dataplane fixture:

1. Runs the server suite under the race detector.
2. Builds the real client and the `tests` executable.
3. Runs `TestClientDataplane*` three times inside a fresh Linux network namespace,
   with loopback enabled and no production control server.

Process tests require `ENDLESSNET_CONTROL_TEST=1`, `ENDLESSNET_TEST_BINARY`, and
a disposable GitHub-hosted runner. They skip under `-short`. Never run
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
DNS projection uses normal CLI sync followed by IPC diagnostics from a disconnected
agent, so the scenario does not depend on the host resolver knowing namespace-local
interfaces. Agent failure snapshots feed redacted IPC errors into timeout diagnostics;
the snapshots and complete client state are never printed.

The scenarios exposed a client runtime gap after v0.5.0: ordinary agent sync did
not clear revoked enrollment. Agent sync and endpoint updates now recognize only
validated terminal Client API error responses (including matching request ID),
stop the tunnel, and clear node-bound state. Device keys, ownership, trust, session
and connection intent are preserved. Temporary/malformed errors and failed tunnel
teardown cannot trigger cleanup. This correction is subsequent to the v0.5.0 tag.

## Evidence boundaries

Common contract results are attributed separately to each runner's OS and
architecture. They do not prove that OS's complete dataplane, DNS resolver,
route, firewall or service lifecycle behavior. The two-client traffic fixture
currently runs on Linux only; Windows/macOS equivalents remain required work.

These tests establish client behavior against a controlled peer, not production
compatibility, OIDC/browser UI acceptance, real Relay/STUN, packet delivery, NAT,
or Windows/macOS networking behavior. Installer tests remain separate.
The server race check and three consecutive process-suite runs passed on
`96c37a7fe7b2e570c555a364790cb903b9d1864a`:
[control-plane CI](https://github.com/endless-net/client/actions/runs/34199199908/job/101973847667).
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

APT authentication now uses the `endlessnet-apt-publisher` GitHub App, installed
only on `endless-net/apt`. The Client repository supplies Actions secrets
`APT_APP_ID` and `APT_APP_PRIVATE_KEY`; the workflow requests a temporary token
for `apt` with `contents: write` and uses it for HTTPS checkout and push.
`APT_REPO_DEPLOY_KEY` is no longer consumed. The APT signing key remains separate.
The token action revokes its token during job cleanup. This configuration change
alone does not establish successful publication or validate the secret contents.
Rerunning the old tag's workflow still uses its old authentication configuration;
publishing version 0.5.0 must retain the original tagged source, not rebuild newer
main code under the existing version.
