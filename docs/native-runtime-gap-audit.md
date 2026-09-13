# Client v0 runtime gaps — 2026-09-13

Status: incomplete implementation audit, not runtime acceptance. Source:
[client main at cba6c69](https://github.com/endless-net/client/tree/cba6c6900be75dff0c679fd400d800d03333b81f).
This snapshot distinguishes the published contract and consumer/mock coverage
from real `ClientRPCService` implementation. It does not reduce the agreed scope.

## Follow-up inspection — 2026-09-13, source 53beb3c

Re-inspected [client main at 53beb3c](https://github.com/endless-net/client/tree/53beb3c473a95a66899ec458b613b1671eb33024)
after the historical snapshot below. The nine missing overrides and UI-quit-only
preferences/lifecycle boundary still remain. They are not closed by removing IPC v2.

- [b75d57b](https://github.com/endless-net/client/commit/b75d57bc22f1a39595a9ff4aa8bc2345ac7cb071)
  removed the final `ipc/v2/contract.go`, old status projection and its callers.
  The import guard now scans all Go files in `cmd`, `internal` and `tests`,
  including platform-tagged files. The repository boundary test rejects a
  nonempty retired `ipc/v2` directory. This supersedes the legacy-removal gap
  in the original snapshot; it does not establish full native feature parity.
- [53beb3c](https://github.com/endless-net/client/commit/53beb3c473a95a66899ec458b613b1671eb33024)
  adds native signed-cache metadata coverage: selected/map account projection,
  dual-stack addresses, peers, STUN/relay endpoints and credential redaction in
  protobuf JSON. A verified cache is explicitly not live connection evidence.
  Local `go test -short ./...`, `go vet ./...` and configured golangci-lint passed;
  privileged traffic and installed-service tests were not executed locally.
- [Test run 34749887115](https://github.com/endless-net/client/actions/runs/34749887115)
  for `81272179f889d8892f3d2a816b1ba119ec39db3b` was cancelled with no jobs.
  It does not qualify the new direct-peer timeout diagnostics. The later
  [Test run 34750041550](https://github.com/endless-net/client/actions/runs/34750041550)
  for `53beb3c473a95a66899ec458b613b1671eb33024` was pending at this inspection;
  no successful runtime/platform claim follows from that state.

The remaining runtime families still require implementation in `client`, UI
binding and requirement trace evidence in `client-ui`, and exact-source GitHub
runner validation. Neither these local tests nor legacy removal close US-01–14
or Android/iOS platform acceptance. The original dated findings follow unchanged.

## Direct-peer runner follow-up — 2026-09-13

Direct-peer follow-up: [control-plane job 103707722163](https://github.com/endless-net/client/actions/runs/34750299641/job/103707722163)
at source `b474b49dc91b60aa36fe9f7d4649ed8ff4c65b2e` failed all three attempts
of `TestClientDataplaneDirectPeerTrafficAndWithdrawal`. Each reached the
post-traffic diagnostics gate, then reported 601 requests and 601 request
failures with no decoded diagnostics response. This narrows the investigation
to RPC/subprocess/decoding failure; it is not evidence of a peer ID, handshake
or counter mismatch. It does not qualify withdrawal or full traffic acceptance.
The harness now recognizes only a complete canonical typed CLI failure line,
reports numeric codes, and withholds all other subprocess output. The timeout
and successful tunnel/peer assertions remain unchanged; root cause and the
required runtime fix remain unconfirmed until a fresh runner observation.

## OS-interface MTU regression — 2026-09-13

A subsequent local regression reproduced a concrete diagnostics rejection:
an OS interface MTU of 65536 returned `LIMIT_EXCEEDED`. The provider exposes
all local interfaces, while diagnostics incorrectly imposed a 65535 maximum
on their MTU. [Linux loopback setup](https://github.com/torvalds/linux/blob/master/drivers/net/loopback.c)
uses `64 * 1024`; the existing v0 `Interface.mtu` field is `uint32`.
The runtime now preserves that full representable range for OS interfaces,
without changing tunnel limits or any contract version. The regression covers
0, 1500, 65535, 65536, uint32 maximum, negative input and overflow (values beyond
host `int` are omitted only on 32-bit hosts). Before the fix, 65536 and uint32
maximum failed; negative/overflow inputs remain rejected rather than wrapped.
This is a reproduced runtime defect consistent with the runner symptoms above,
not yet proof that fixing it closes direct-peer, withdrawal or platform acceptance.

## Installed bootstrap fixture mismatch — 2026-09-13

[Windows 2025 installed-service job 103707722069](https://github.com/endless-net/client/actions/runs/34750299641/job/103707722069)
at `b474b49dc91b60aa36fe9f7d4649ed8ff4c65b2e` timed out in the bootstrap
enrollment condition of `TestInstalledClient/enrolled-reinstall`. Current source
still creates that control peer with `testcontrol.New(t)`, whose nil-listener
mode is HTTP, and passes its origin to the stopped-service CLI bootstrap.
`AdoptInitialProfile` validates that stored origin using `rpcProfileOrigin`,
which requires HTTPS. Thus the fixture does not meet native profile admission;
loosening origin validation or reintroducing HTTP fallback is not a fix.

The producer-owned installation test needs the existing TLS listener fixture
with ephemeral public-CA trust available to both bootstrap CLI and installed
service. `testclient.TrustControlTLS` already supplies machine trust on disposable
Windows/macOS runners, but its Linux path relies on per-process `SSL_CERT_FILE`;
that is not evidence of trust in a separately launched systemd service. Preserve
certificate verification, exact trust cleanup and the disposable-runner guard.
This inspection identifies a concrete fixture incompatibility, not a successful
reinstall/upgrade run or proof that no other installation defects remain.

Fixture implementation follow-up: `TestInstalledClient` now creates the HTTPS
listener and calls `TrustInstalledControlTLS` before bootstrap. Windows/macOS
reuse existing machine-trust setup. Linux exclusively creates a random-named
public certificate under `/usr/local/share/ca-certificates`, refreshes the CA
bundle, and removes that exact file plus refreshes the bundle during cleanup.
The helper rejects local and self-hosted execution before any filesystem or
trust-store action; its admission guard is short-tested. This is disposable test
trust, not production configuration or disabled certificate verification. Local
short tests do not execute installation or trust-store mutation; actual hosted
bootstrap/reinstall/upgrade and certificate cleanup remain unqualified.

## Native application/cache fixture follow-up — 2026-09-13

[Contract job 103707722076](https://github.com/endless-net/client/actions/runs/34750299641/job/103707722076)
at source `b474b49dc91b60aa36fe9f7d4649ed8ff4c65b2e` failed native readiness in
both families of `TestControlPlaneNativeApplicationRoute` and
`TestControlPlaneNativeCachedMapExpiry`. These scenarios also used the HTTP
fixture incompatible with native profile adoption. They now explicitly use
`testcontrol.NewTLS` and existing disposable-runner `TrustControlTLS` before
enrollment. The new fixture short test verifies an actual trusted TLS handshake
and rejection with an empty root pool. Lower-level HTTP fixtures are unchanged;
there is no profile-validation bypass or automatic protocol fallback.
Actual application-route traffic, access withdrawal, cached-map expiry and
restart assertions are preserved, but still require fresh hosted execution.

## Hosted Windows source verification — 2026-09-13

[Verify (Windows), job 103716995694](https://github.com/endless-net/client/actions/runs/34754603819/job/103716995694)
completed successfully for exact source
`f299b247613ce713f82d1d818fd08df28fdb2e36`. The job ran `go vet ./...`,
`go test -short ./...` and the client build. Logs confirm successful root-module
tests including `cmd/endlessnet-client`, `internal/client`, `internal/testclient`
and `internal/testcontrol`. This source includes the OS-interface MTU regression,
native exit/resource CLI contract cases, strict CLI failure classification and
the explicit HTTPS fixture handshake tests.

This is hosted Windows source/short-test/build evidence only. Privileged
installed-service, routing, expiry and machine-trust cleanup paths skip under
`-short`; they are not accepted by this job. The nested `clientipc` module has
its own qualification and is not implicitly tested by the root-module command.
At this observation the same-source control-plane and Windows installed-smoke
jobs were still running, so their results remain unclaimed here.

## Installed HTTPS follow-up — source f299b24

Same-source installed jobs now pass the bootstrap enrollment condition:
[Windows 2025](https://github.com/endless-net/client/actions/runs/34754603819/job/103716995560),
[Ubuntu 24.04](https://github.com/endless-net/client/actions/runs/34754603819/job/103716995737),
and [macOS 15](https://github.com/endless-net/client/actions/runs/34754603819/job/103716995719).
Windows subsequently fails initial connection with diagnostics unavailable.
Ubuntu/macOS advance through initial connection, connected upgrade and connected
reinstall before failing the unprivileged Unix transport probe. These are partial
scenario observations; all three jobs failed, not full installation acceptance.

The Unix probe supplied no request/profile/CAS arguments to native mutations,
so CLI validation could reject `connect` before reaching the socket. The fixture
now obtains a public owner snapshot, supplies distinct request IDs plus the exact
profile/instance/revision, and keeps explicit local-forget confirmation. It still
requires exit 1, empty stdout, no timeout and a transport permission-denied error.
Cross-platform short tests verify argument construction; real denied-peer socket
execution, remaining reinstall phases and cleanup need a fresh hosted run.

## Direct-peer post-MTU result — source f299b24

[Control-plane job 103716995697](https://github.com/endless-net/client/actions/runs/34754603819/job/103716995697)
failed all three repetitions later in `exerciseConnectionIntent`, after two
logged agent restarts. The previous diagnostics gate no longer failed: execution
reached this helper only after initial direct traffic/handshake/counter checks,
peer withdrawal denial, restored traffic and application-policy exercise.
This is partial exact-source evidence that the original MTU-related diagnostics
barrier was removed, not a successful complete direct-peer scenario.

The helper previously reported only its outer caller line, so those logs cannot
distinguish connected-after-restart status from subsequent outage/recovery
observations. Fixed public phase labels now identify each boundary without
printing identities, credentials or arbitrary error details. Status predicates,
traffic assertions, timeouts and enrollment-count checks remain unchanged.
Connection-intent restart/outage/recovery acceptance remains open.

## Installed Unix permission follow-up — source 92085ba

[Ubuntu 24.04 job 103717701616](https://github.com/endless-net/client/actions/runs/34754925762/job/103717701616)
ran source `92085ba8dfaad3cc77f3f8718229692f0330a44b`. It passed the
restricted Unix IPC attempts with complete native mutation arguments, then
connected-intent restart and executable-loss repair. The next failure was the
status wait in `cached traffic after service startup without control`.
The preceding current-agent failure observation passed; failure diagnostics
reported the same identity, an OK tunnel, one peer and the expected endpoint.
The cached-traffic probe itself was not reached, so this is not offline traffic
acceptance. Investigate native connection-phase publication separately from
control failure and cached dataplane state; do not relax the connected predicate.

The session recovery, routed-resource and service-catalog scenarios now also use
the explicit HTTPS control fixture and runner-scoped test certificate trust.
Their original authorization, signed-map and actual-traffic assertions remain
unchanged. Short tests do not execute these privileged scenarios; fresh hosted
results are required before claiming HC-014, HC-032 or HC-051 acceptance.

## Native offline connection-phase correction — 2026-09-13

The source path behind job `103717701616` discards the iteration snapshot when
online map retrieval fails. `agentRPCIterationPhase` consequently supplies
`UNSPECIFIED`, including after a successful cached bootstrap. This conflates
control synchronization failure with loss of the independently running dataplane.

Native status now supplements an unspecified phase with a fresh, nonblocking
engine inspection bound to the verified network ID, node ID and map revision.
It requires a successfully configured engine, an active profile, connected
intent and valid cache, and does not override explicit transition/disconnected
phases or run through native trust recovery. Control failure remains independent:
the connected offline dataplane is reported as degraded, not control-ready.
No old snapshot, mere cache presence or requested intent establishes connectivity.

Short tests cover current/foreign/stale/absent map identities, busy and stopped
engines, missing profile, disconnected intent, invalid cache and failed inspection.
An integrated status projection test verifies the exact map binding and preserves
offline control state. Root short tests, vet and lint pass locally. These are not
installed or actual-traffic acceptance: the original hosted offline/recovery
assertions remain unchanged and still need a fresh source run.

## Native traffic fixture completion follow-up — 2026-09-13

The shared native TCP/UDP scenario now uses HTTPS regardless of whether flow
logging is enabled; previously only its flow-log branch enabled TLS. Ephemeral
lifecycle (HC-011) and native exit-route (HC-036) scenarios also use HTTPS with
explicit runner-scoped certificate trust. The replacement ephemeral node shares
the test's OS trust installation and receives its own Linux process CA file;
the same certificate is not registered for cleanup twice.

These changes remove bootstrap origin mismatches without changing packet-count,
route withdrawal, terminal cleanup or recovery assertions. Local root short
tests, vet and lint pass; short mode skips the privileged traffic scenarios.
The exit-route reference still proves only the client egress hop, not a public
Internet address, and hosted traffic acceptance remains outstanding.

## Shared recovery and relay fixture follow-up — 2026-09-13

`nativeControlScenario`, used by recovery/error-matrix and lifecycle scenarios,
now supplies HTTPS and explicit runner-scoped trust before enrollment. Native
relay traffic/failover and machine-sharing scenarios use the same TLS fixture.
Their public-error classification, signed grant expiration, relay-only traffic
and recovery assertions are unchanged; TLS setup is not evidence they passed.

The HTTP fixtures in local IPC unary-timeout and stream-fault tests are not
control-plane agent bootstrap paths: those tests create a separate local
protobuf handler and never start the agent. They do not imply HTTP IPC v2 and
are deliberately not changed by this fixture migration. Unsupported-platform
router/exit-provider CLI checks also require separate native-command review,
not a blind TLS substitution.

## Ubuntu contract repetition result — source 92085ba

[Ubuntu 24.04 repetition 1, job 103717701573](https://github.com/endless-net/client/actions/runs/34754925762/job/103717701573)
tested `92085ba8dfaad3cc77f3f8718229692f0330a44b`. Its log explicitly records
`PASS` for `TestControlPlaneRouteAdvertisement` (including four browser invalid
input/recovery cases) and `TestControlPlaneNativeApplicationRoute` for both IPv4
and IPv6. These results qualify those tests on this source/platform only, not
the whole matrix or subsequent commits.

The same job failed cached-map expiry at its native status wait and diagnostics
export with `invalid bundle result`, then accumulated HTTP-fixture startup
failures before the test process's 40-minute timeout. Later TLS migrations and
offline projection changes are not part of this source and need fresh evidence.
The diagnostics test now reports fixed numeric operation/failure codes and
descriptor-validity booleans on that assertion, without IDs or archive contents.
It retains the success, lifetime and size requirements unchanged.

## Cached-map expiry precondition correction — 2026-09-13

The expiry scenario required durable `DESIRED_STATE_CONNECTED` after expiration,
but bootstrap enrollment followed by agent startup never issued native `Connect`.
Initial profile adoption explicitly preserves intent rather than creating one;
an operating tunnel alone does not prove that durable mutation was accepted.
The test now accepts and awaits native `Connect` with captured profile/CAS and a
retained request UUID, then observes connected intent before shortening map
validity. It still requires that intent and credentials survive expiration while
both TCP and UDP are denied, including after restart and before signed recovery.
Local short checks do not execute this privileged expiry test; its fresh hosted
result is required before claiming expiry or fail-closed acceptance.

## Methods without a runtime override

The [service contract](../proto/client/v0/service.proto) declares these methods,
but `ClientRPCService` has no override in the inspected source. They are inherited
from the embedded generated `UnimplementedClientServiceHandler` in
[`service_rpc_handlers.go`](../internal/client/service_rpc_handlers.go).
The RPC guard maps unimplemented failures to the native unsupported category;
caller admission remains separate. A generated client or testserver response is
not evidence that the production runtime implements a method.

| Runtime gap | Consumer scenario | Required producer outcome |
| --- | --- | --- |
| `GetSession`, `RenewSession` | US-09 expiry/renewal | Authoritative profile session projection and durable renewal, independent of node-credential deadlines; cancellation, failure and restart evidence |
| `ListExitNodes`, `GetExitNode`, `SelectExitNode`, `ClearExitNode` | US-05 exit node | Authorized catalog, requested/effective IPv4/IPv6 state, policy restrictions and durable apply/clear; actual route/traffic and fail-closed checks |
| `ListResources`, `SetResourceEnabled` | US-11 resources | Authorized bounded catalog and policy-aware mutation, invalidation and conflict handling; resource availability must follow real runtime observations |
| `GetUpdateInfo` | US-13 distribution | Verified update metadata and compatibility projection; source failure must not mean up-to-date, and projection alone does not prove installation |

There are nine missing overrides in this snapshot. Presence of overrides for
other methods does not imply that their complete functional or platform scope
is implemented or accepted.

## Partial implementations requiring further work

- `SelectNetwork` implements only exact current-ID reselection as a durable
  no-op. Another network remains unsupported; enrollment/switch/rollback and
  truthful connectivity continuity still require implementation. See the
  [network catalog boundary](native-network-catalog.md).
- `SetPreferences` and `ResetPreferences` accept only the UI-quit key;
  `GetPreferences` projects that subset. `NotifyLifecycle` accepts only UI_QUIT.
  See [`service_rpc_uiquit.go`](../internal/client/service_rpc_uiquit.go).
  The other preferences and OS lifecycle events need real providers and tests,
  not fabricated defaults or successful no-ops.
- Desktop transport tests do not prove Android/iOS native adapters, installed
  services, permissions, suspend/resume, screen-reader behavior or traffic.
- Remaining HTTP IPC v2 handlers, DTOs and tests still need removal/migration.
  Native hosting without fallback is not equivalent to a legacy-free repository.

## Evidence needed to close gaps

`client` owns runtime providers, durable execution, producer tests and desktop
runtime/platform evidence. `client-ui` owns UI binding, shared consumer tests and
mobile/native UI adapters. Consumer fixtures must model unsupported, partial and
failure states as well as planned successful behavior; they must not turn a mock
success into a claim of runtime availability. Keep all UF/UBR/UI-AC requirements
and US-01–14 in the client-ui trace; no scenario is accepted solely by this audit.

For each implemented gap, verify authorization, profile/context binding,
idempotent mutation/recovery where applicable, cancellation, events and truthful
requested/effective state. Then collect exact-source GitHub runner evidence at
the appropriate scope. Simulator/emulator, mock and actual runtime evidence
remain separate. Versions stay unchanged; infrastructure, production and new
release/version actions still require their respective explicit authorization.
