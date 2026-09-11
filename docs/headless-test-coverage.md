# Headless client coverage ledger

Status: in progress. This is a coverage audit, not a completion claim.

Scope: [HC-001–HC-065, architecture main](https://github.com/endless-net/architecture/blob/main/docs/ru/headless-client-use-cases.md),
[business requirements](https://github.com/endless-net/architecture/blob/main/docs/ru/headless-client-business-analysis.md),
and [system design / IT specifications](https://github.com/endless-net/architecture/blob/main/docs/ru/headless-client-system-design.md).

## Active goal: Client only

The user narrowed the working goal on 2026-09-11. This scope supersedes the
earlier cross-repository plan, including producer follow-ups recorded below.

Cover every applicable HC-001–HC-065 client scenario and its BR/AC requirements
with positive, negative and recovery tests owned by this repository. Exercise
the real headless client against a testserver implementing published wire
contracts. Observe public CLI/IPC, protocol exchanges and actual application
traffic; do not read databases or private persisted state, or import service
runtime internals into these integration tests. The testserver must model
contract behavior, not copy a backend implementation or accommodate a client bug.

Keep test drivers, fixtures, coverage documentation and CI changes in Client.
Include installation, service lifecycle, privileges, update and removal in the
Client-owned OS suite. Fix client defects revealed by these tests and verify
them with regressions. Require the applicable client scenarios in Client CI
and gate client publication on successful checks for the exact source commit.

Do not develop or modify Coordinator or other services, their tests or CI.
Real backend processes, provider conformance, cross-service acceptance,
system-tests, Infrastructure and production deployment are outside this goal.
External contract gaps are recorded as constraints; they do not authorize
changes to the producer. Existing P/R results below are historical evidence,
not required work or substitutes for Client-owned coverage. No rollback of
previously committed changes is implied by this scope correction.

Completion requires a traceable Client-owned test for every agreed applicable
scenario/variant and successful required Client CI. Unsupported features and
unresolved product requirements remain explicit gaps, never successful skips.
This scope change does not reduce coverage of the client itself.

## Evidence levels

- **L**: [installation suite](../tests/installation_test.go), driven by real OS
  services and public CLI/IPC. Platform limits: [installation notes](installation-tests.md).
- **C**: [control-plane suite](../tests/control_plane_test.go), real client
  process against [testcontrol](../internal/testcontrol/server.go) through wire
  contracts, observed through public IPC/CLI. Linux isolated CI only.
- **D**: [testcontrol self-tests](../internal/testcontrol/server_test.go), public
  SDK/protobuf clients against the double. D does not prove real client or producer.
- **U**: component tests exist but may use internal functions/state; they cannot
  replace contract-only consumer/provider integration.
- **P**: real producer conformance through published contracts, owned by that producer.
- **R**: real service/client pairs with exact source pins. Their bounded evidence
  is distinct from product acceptance on an immutable manifest, owned jointly
  with [system-tests, main](https://github.com/endless-net/system-tests/tree/main).

Rows identify starting evidence and remaining work, not full HC coverage. A row
is complete only with agreed platform/feature variants, positive/negative/recovery
cases, a successful CI run for exact source/artifact pins and the appropriate
evidence level. No row below is declared complete. Missing tests and unresolved
product scope are different conditions; neither is a successful skip.

## Initial audit

| HC | Existing or added starting point | Remaining contract-level outcome |
| --- | --- | --- |
| HC-001 | L supported installation matrix | Unsupported platform/privilege outcomes and explicit supported variants |
| HC-002 | L installed CLI version | Artifact/dependency failure paths |
| HC-003 | L noninteractive installation | Repeat and interrupted installation |
| HC-004 | L real service and IPC | Local authorization and unavailable-service outcomes |
| HC-005 | C process lifecycle | Duplicate agent and termination semantics |
| HC-006 | L service restart without interactive login | Actual machine reboot and late-network availability |
| HC-007 | C BrowserEnrollment | Real Identity/SSO pair, account binding |
| HC-008 | C BrowserEnrollment | Poll expiry/cancellation and foreign poll authorization |
| HC-009 | C enrollment; P wrong/expired join authorization; R two real CLI registrations | Full authorized/denied attribute and platform variants |
| HC-010 | C RegistrationResponseLoss; D ResponseLossPreservesOperation; R distinct node IDs | Real producer replay currently fails; image-cloning and batch variants |
| HC-011 | No C/R evidence audited | Decide ephemeral lifecycle, then normal/crash expiry tests |
| HC-012 | D request/proof tests | Producer-owned allowed/denied attributes and effective access |
| HC-013 | C BrowserEnrollment; R registered-node rejection and reapproval restore real traffic | Remaining pending/denial and browser completion variants |
| HC-014 | U recovery matrix | C/R session expiry, reauthentication and preservation |
| HC-015 | R revoked join key denies a new client while existing map access/renewal survives | Agent/dataplane, expiry and offline variants; node revocation remains separate |
| HC-016 | C initial cached-map status | Actual allowed application traffic |
| HC-017 | C disconnect/restart/connect; R repeated disconnect/connect and process restart block/restore direct TCP/UDP with original identities | Other OSes and failure variants |
| HC-018 | C and R connected/disconnected intent survives process restart with traffic checks | Host reboot, crash during intent write and other platform/network variants |
| HC-019 | U configuration tests | Public preference mutation and observed effect |
| HC-020 | No C/R evidence audited | Product decision for profiles, isolation and switching |
| HC-021 | U control-endpoint security | Public origin/trust changes and wrong endpoint |
| HC-022 | C Lifecycle logout; U typed logout | Remote cleanup unconfirmed, local forget, profile semantics |
| HC-023 | C Lifecycle peer projection; R approval changes applied by running agent and WireGuard | Remaining authorization and peer absence variants |
| HC-024 | C IPv4 ICMP/TCP/UDP; R real Coordinator and two agents exchange direct TCP/UDP nonce data | Other platforms, IPv6, Relay/NAT and full policy variants |
| HC-025 | C DNS CLI/proxy, DNS wire lookup and application access by FQDN | OS resolver integration, live reload, split DNS, IPv6 and upstream variants |
| HC-026 | U DNS/router configuration | External DNS control and IP-access preservation |
| HC-027 | C delta/resync and TCP grant withdrawal with retained established/fresh UDP flow; historical R node/port withdrawal | Remaining Client direction/destination correlation and platform variants |
| HC-028 | C direct IPv4 traffic; U Relay implementation | Forced Relay, NAT and path transitions with packet probes |
| HC-029 | U endpoint/reconnect tests | External network change and stale response ordering |
| HC-030 | C typed/malformed errors; R fresh and established direct TCP/UDP survive short Signing dependency and Coordinator process outages and recover without registration | Map/lease expiry, edge/transport/storage outage and remaining variants |
| HC-031 | Central ACL tests are not evidence of a local inbound preference | No local inbound toggle found in current CLI/IPC/config; product and contract gap |
| HC-032 | U routes/application tests | Real consumer to resource behind router |
| HC-033 | U configuration tests | Public selection/disable and actual route effect |
| HC-034 | U SNAT/forwarding tests | Advertisement versus approval versus effective traffic |
| HC-035 | No C/R evidence audited | Site-to-site scope and reverse-path tests |
| HC-036 | U exit-route configuration | Egress IP and independent IPv4/IPv6 probes |
| HC-037 | U exit-LAN rules | LAN allowed/denied with real exit traffic |
| HC-038 | U router configuration | Real client acting as egress for another client |
| HC-039 | No C/R evidence audited | Role-specific HA semantics and failure recovery |
| HC-040 | U application discovery/runtime | Node RPC → signed effective map → real connector traffic |
| HC-041 | No C/R evidence audited | Overlap product decision and unambiguous target tests |
| HC-042 | No C/R evidence audited | SSH feature scope, authority and commands contract |
| HC-043 | No C/R evidence audited | Forwarding scope and allowed/denied listener tests |
| HC-044 | No C/R evidence audited | File-transfer scope, integrity and interrupted transfer |
| HC-045 | No C/R evidence audited | Personal-device transfer decision and recipient restriction |
| HC-046 | No C/R evidence audited | Private publication contract and external denial |
| HC-047 | No C/R evidence audited | Public exposure contract, authorization and external reachability |
| HC-048 | No C/R evidence audited | Audience restrictions, lifetime, shutdown and crash expiry |
| HC-049 | No C/R evidence audited | Certificate/publication scope and frontend/backend TLS |
| HC-050 | U sharing/encrypted engine tests | R grant/consent/revoke/expiry/new identity without internal access |
| HC-051 | U service discovery/runtime | Logical service host approval, loss and actual traffic |
| HC-052 | L/C bounded IPC waits | Public readiness conditions and noninteractive timeout results |
| HC-053 | L/C structured IPC; D RPC authorization | Public machine output/errors and repeated report acceptance |
| HC-054 | No C/R evidence audited | Container persistent versus ephemeral lifecycle |
| HC-055 | No C/R evidence audited | Userspace/no-TUN product scope and application proxy behavior |
| HC-056 | C status/diagnostics; U path diagnostics | Distinguishable control/path/DNS/application failures |
| HC-057 | D FlowConsentAndIdempotency; U flow tests | Real client consent/retry behavior and safe diagnostic export |
| HC-058 | L version and runtime platform | CLI/daemon/artifact mismatch and exact release identity |
| HC-059 | U trust/recovery matrix | C explicit trust confirmation; independent node-signing scope separately |
| HC-060 | L same-source installation | Updating existing enrolled installation, artifact gates and restored access |
| HC-061 | Publication gate, fixture tests and real GitHub API check | Supported update channels, artifact acceptance and update failures |
| HC-062 | C process restart during outage | Repair of damaged installation separately from identity reset |
| HC-063 | U local-forget/recovery tests | Public privileged reset and new identity, real producer outcomes |
| HC-064 | L uninstall | Explicit binary/state retention versus full removal, enrolled machine |
| HC-065 | C terminal revoke; R deleted Client sync denied and peer withdrawn without node recreation | Agent/dataplane retirement, offline leases and separately verified local removal |

## Current implementation increment

Commit `0c0dd6b` adds a one-shot post-registration response loss in testcontrol,
safe operation ID/request digest observations, a D replay test and real-process
C registration retry and outage/restart tests. No request bodies, secrets or
private state are exposed. A response-loss event establishes what the double
did; it is not evidence of a real Coordinator commit.

Local `go test -short ./...` and `go vet ./...` passed. Short mode compiles but
does not execute L/C. Full execution is tracked by
[Test run 34585429781](https://github.com/endless-net/client/actions/runs/34585429781);
failed the real-client registration replay assertion in all three repetitions:
the second `up` process generated a new operation ID. Outage/restart and all
installation/platform jobs passed. This is a discovered client defect, not a
test waiver. The follow-up persists the pending direct request and its origin
before sending, rejects changed retry input, and clears pending state only with
the validated registration result or explicit enrollment cleanup. Its subsequent
CI run must pass before this registration case is considered verified.

That fix is commit `aa46030`, verified by successful
[Test run 34585856847](https://github.com/endless-net/client/actions/runs/34585856847),
including all three control-plane repetitions and the installation/platform jobs.
This establishes consumer retry behavior against the double, not real producer
idempotency or release acceptance.

The next increment adds `TestControlPlaneRecoveryErrorMatrix` (eight published
error codes) and `TestControlPlaneMalformedErrorsPreserveEnrollment` (five invalid
wire responses). They observe only public IPC identity/cache indicators and map
recovery. Misleading diagnostic text must not trigger enrollment cleanup. A
scoped persistent wire fault is cleared to demonstrate recovery for nonterminal
responses; a separate double test verifies fault isolation and restoration.
The error matrix is commit `e9b5fa8`, verified in
[Test run 34586300405](https://github.com/endless-net/client/actions/runs/34586300405).
All three consumer repetitions and all mandatory platform/installation jobs pass.

The existing `control-plane` job selects all `TestControlPlane*` tests, runs them
three times and is required by `verify`. These additions therefore run on each
main push. Core and APT publication now call `tools/verify-source-ci` before
building or publishing. The gate requires a successful `Test` main-push run for
the checked-out SHA, all eleven required jobs, and stable run-attempt evidence.
Missing, skipped, failed, pending or inconsistent evidence blocks publication;
an older success cannot mask a newer failed run. GitHub API contract references:
[workflow runs](https://docs.github.com/en/rest/actions/workflow-runs) and
[workflow jobs](https://docs.github.com/en/rest/actions/workflow-jobs).
The fixture-based gate tests run in short CI on every platform; a read-only
`Check source CI gate` workflow exercises the real API after successful source CI.
Commit `d76df3a` passed [Test run 34586736163](https://github.com/endless-net/client/actions/runs/34586736163).
The [real API gate check 34586887809](https://github.com/endless-net/client/actions/runs/34586887809)
passed and reported that exact commit, source run and attempt 1. This does not
attest published artifacts or backend release acceptance, and deploys nothing.

`TestControlPlaneRejectsRegistrationResponseMismatch` adds six negative/recovery
cases: another operation ID, signed request binding, device fingerprint,
credential node, credential network, and invalid map signature. The consumer
must reject the response without installing a node/credential/cache, then recover
the same operation and node on retry. The double tests inspect public wire DTOs,
verify valid signatures on deliberately mismatched responses, and ensure the
committed replay remains unchanged. Some rejections originate in the pinned SDK;
the real-process test also checks local lifecycle and retry effects. Local short
tests, vet and lint pass. Commit `3b66bca` passed all mandatory jobs in
[Test run 34587071399](https://github.com/endless-net/client/actions/runs/34587071399),
including all three repetitions of each mismatch case. Its subsequent
[source gate check](https://github.com/endless-net/client/actions/runs/34587265374)
also passed.

The next increment adds a retained peer delta to testcontrol and
`TestControlPlanePeerDeltaRecovery`: add/remove peers, reject incorrect base hash
and source revision, retain the last verified cache, retry, and recover a skipped
history with a full signed resync. Assertions use CLI/IPC and redacted wire-event
observations. `TestPeerDeltaAndResync` additionally verifies peer replacement and
applies the double's events with the published SDK. The double keeps only one
delta; this is a consumer recovery fixture, not evidence of Coordinator history
retention. [Test run 34587437499](https://github.com/endless-net/client/actions/runs/34587437499)
failed the new consumer case in all three repetitions: the test assumed revision
1 after disconnect, but disconnect had already published a node-state revision.
The follow-up establishes an explicit post-teardown signed baseline and derives
its exact revision before checking delta transitions. All other mandatory jobs
passed; corrected consumer execution still needs CI evidence.

`TestControlPlaneDirectPeerTrafficAndWithdrawal` introduces two real agents in
separate disposable Linux network namespaces connected by a private underlay
bridge. The client manages overlay routes with `--route-table auto`. The test
requires bidirectional overlay ICMP, successful WireGuard handshakes and byte
counters, denial after receiver-side peer withdrawal, then restored traffic.
It uses public maps and IPC; no client config, identity keys or runtime internals
are read. It covers a direct IPv4 path with a control double, not real backend
authorization, forced Relay, NAT traversal, application TCP/UDP or other OSes.
Initial execution is pending CI; this is not yet confirmed coverage of HC-024,
HC-027 or HC-028 as a whole.

In [Test run 34587947570](https://github.com/endless-net/client/actions/runs/34587947570)
the corrected delta case passed all three repetitions. The new real-peer case
failed during enrollment: its underlay control address was plaintext outside
loopback, which the pinned SDK rejects. The follow-up uses HTTPS with an
ephemeral test certificate, keeps its private key only in testserver memory, and
supplies public CA trust via `SSL_CERT_FILE` only to the spawned client processes.
A double test proves that an untrusted certificate is rejected and explicit test
trust permits the public API call. The client security rule remains enforced.

The real-peer scenario now also runs `tools/packetprobe` inside the receiving
namespace and verifies random payload echoes over TCP and UDP on two ports.
After baseline success, a published peer ACL permits TCP on one port and UDP on
the other; each denied combination is probed three times while local application
health is independently checked. Removing the restriction must restore all four
combinations. Helper failures and corrupt payloads are distinct from network
denial. This tests newly opened flows; established-flow withdrawal and broader
policy variants remain separate requirements. Local short tests, vet and lint
pass. Commit `f3aed7e` passed all mandatory jobs in
[Test run 34588456392](https://github.com/endless-net/client/actions/runs/34588456392).
The HTTPS real-peer/application/policy scenario passed in all three repetitions;
[source gate run 34588626955](https://github.com/endless-net/client/actions/runs/34588626955)
also passed. This establishes the stated Linux consumer scope, not all platform
variants or producer authorization.

The subsequent increment holds the original TCP and UDP sockets across a
published ACL withdrawal. Each must exchange a random payload before withdrawal,
then fail to exchange after the new map arrives while the local receiving
application stays healthy. Fresh application access must recover when the grant
is restored. The helper protocol reports only readiness and exchange outcomes;
its short test proves repeated exchanges reuse one connection and distinguishes
closure from success. This lifts the existing kernel-level established-flow
test into a real-client contract scenario. Full execution awaits CI; timing
guards remain test deadlines, not accepted business revoke SLAs.

[Test run 34588791343](https://github.com/endless-net/client/actions/runs/34588791343)
failed established TCP withdrawal in one repetition; two repetitions passed.
The consumer assertion had waited only for cached map revision. The follow-up
requires public IPC `Agent.SnapshotState=current`, the same applied snapshot/map
revision, no agent error, and a healthy WireGuard instance before checking policy
effects. Unexpected session outcomes are now reported as bounded categories.
This addresses an insufficient readiness assertion; it does not waive the failed
packet result or prove that runtime revocation is correct. CI must verify again.

The same real-peer fixture now exercises explicit `service disconnect`, process
restart while disconnected, `service connect`, repeated connect, and restart with
connected intent. Application traffic must stop/resume accordingly while node
identity and verified cache remain. It also checks bidirectional/application
traffic during a temporary control outage with still-valid cached authority and
recovery to a new map after service restoration. The transcript must contain only
the two initial registrations. This extends HC-017/HC-018/HC-030 beyond state-only
assertions; actual host reboot, lease expiration and OS variants remain separate.
Local short tests, vet and lint pass; the new intent/outage checks await CI.

[Test run 34589108703](https://github.com/endless-net/client/actions/runs/34589108703)
reached the final registration-count assertion in all three repetitions: applied
ACL withdrawal, established-flow denial, disconnect/restart/connect and outage
traffic checks completed. The final assertion failed because the double recorded
both node creation and credential-authenticated refresh as `registered`.
`service connect` legitimately refreshes the existing node through the published
registration operation. The corrected transcript separates `registered` from
`registration-refreshed`; a double/SDK test proves refresh preserves node ID,
identity key and WireGuard key. The consumer still requires exactly two created
nodes and rejects a refresh for any other identity. This is pending CI evidence,
not a successful aggregate result for run 34589108703.

HC-031 was audited separately: current central ACL tests cannot establish a user
controlled inbound-blocking preference. No such operation appears in current
CLI dispatch, IPC contract or client configuration. Its product/contract decision
remains open, alongside the explicitly unresolved BA scope questions; no missing
feature is counted as test coverage.

Commit `0b1a60e` passed all mandatory jobs in
[Test run 34589433548](https://github.com/endless-net/client/actions/runs/34589433548),
including all three real-peer repetitions with established-flow withdrawal,
intent/restart and outage traffic checks. Its
[source gate check 34589675950](https://github.com/endless-net/client/actions/runs/34589675950)
also passed. Node creation and credential refresh are now distinguished by the
fixture and verified through the public SDK and client outcomes.

The next HC-025 increment drives real `dns resolve` and `dns serve` commands and
accesses the receiving application by FQDN using an explicit DNS resolver. It
checks exact IPv4 answers, real TCP/UDP payload exchange, NXDOMAIN for absent and
removed peers, and restored name/application access after the signed map restores
the peer. Each `dns serve` process intentionally starts from its current verified
snapshot. This is CLI/proxy evidence, not live reload or OS resolver integration;
split DNS, upstream failover, IPv6 and automatic DNS policy remain separate.
The helper distinguishes DNS name-not-found from timeout/transport failure. Its
short test uses actual DNS wire messages and a local application; local short
tests, vet and lint pass. Commit `8ccd0fc` passed all mandatory jobs in
[Test run 34589868156](https://github.com/endless-net/client/actions/runs/34589868156),
including all three DNS/application repetitions; its
[source gate check](https://github.com/endless-net/client/actions/runs/34590070988)
also passed.

A [cross-service source audit in Architecture, main](https://github.com/endless-net/architecture/blob/main/docs/ru/evidence/2026-09-11-headless-provider-contract-gap.md)
found plaintext Coordinator authorization errors at its recorded source snapshot.
[Subsequent provider evidence](https://github.com/endless-net/architecture/blob/main/docs/ru/evidence/2026-09-11-headless-provider-recovery.md)
records the typed-error fix and real-binary/YDB checks for credential recovery,
join-key validity, approval, and node-key policy expiry. Direct registration
replay still returns 401 after a consumed key and blocks Coordinator publication.
Consumer replay success alone therefore does not establish provider compatibility.
Do not make the client forget enrollment on arbitrary 401 responses to conceal it.

The [Coordinator-owned real-pair test](https://github.com/endless-net/coordinator/blob/ab4a94722b4e5ede948d7ecf751a470c9a5575a3/tests/providercontract/client_pair_test.go)
passed in [CI 34597438553](https://github.com/endless-net/coordinator/actions/runs/34597438553/job/103256466928)
against this Client at `ffa46a14282f9a063bd63ddfd92392c6fb0a2032`.
It drives real `up`, `sync`, and `status --json`, checks two distinct nodes,
credential renewal in a new process without a join key, and peer withdrawal after
administrative deletion. Revoking a reusable join key denies a third Client with
the provider's typed error and preserves the original Client's map access and
renewal; public inventory confirms no third node. It never reads private Client files. A declared edge
double serves Signing public keys and proxies Coordinator requests unchanged.
An actual provider temporary map error during Signing outage preserves accepted
CLI revision/identity; restored sync succeeds without a registration attempt.
After node deletion the removed Client's sync fails on the real typed revoked
error, with no registration attempt or recreated node. These assertions cover
CLI calls, not automatic agent recovery or enrollment-state cleanup.
Signing/Billing/Relay are doubles; agent traffic, real Gateway/Signing and
manifest acceptance remain unproven by this test. The overall CI is failed on
the separate registration replay subtest.

The [running-agent extension](https://github.com/endless-net/coordinator/blob/8912929d117a5021c28a0dcb95d6abade11df003/tests/providercontract/client_agent_test.go)
passed in [CI 34598600137](https://github.com/endless-net/coordinator/actions/runs/34598600137/job/103260229551),
with the same Client pin. Public IPC confirms approval withdrawal/restoration in
the current applied snapshot and WireGuard peer count. After the actual provider
returns a typed temporary map/endpoint failure, the agent recovers automatically
without registration, preserving identity and credential. This privileged hosted
CI test never reads private state files. Packet delivery, existing-flow denial,
Gateway readiness, OS service installation and manifest acceptance are separate.

## Real direct traffic and rejected-node recovery

The [Coordinator-owned traffic suite](https://github.com/endless-net/coordinator/blob/dbede53d2a62b3154769b5e862b7b663dddf264f/tests/providercontract/client_traffic_test.go)
uses this Client and packetprobe at `a73e943a63115efc568f2f9529b39a383443b71d`.
[CI 34600366604](https://github.com/endless-net/coordinator/actions/runs/34600366604/job/103265978598)
passed bidirectional direct IPv4 TCP/UDP nonce exchange, fresh-traffic denial
after withdrawal with a live destination application, and restored exchange
after reapproval under the original node IDs. Public IPC confirms applied maps,
WireGuard peers, handshakes and RX/TX counters. Separate network namespaces
prevent same-host overlay delivery from bypassing the tunnel.

The earlier Client stopped map polling after a signed rejected state, so it
could not observe later approval. A component regression reproduced this, then
passed after pending/rejected nodes were kept as signed-map observers without
online heartbeat. The real-pair test then confirmed restored traffic. Client's
own [Test CI 34600218754](https://github.com/endless-net/client/actions/runs/34600218754)
passed all mandatory jobs; optional external STUN compatibility was not run.

[Architecture evidence](https://github.com/endless-net/architecture/blob/main/docs/ru/evidence/2026-09-11-headless-real-traffic.md)
records exact pins, the failing baseline and the fix. This initial slice does not
establish real Gateway/Signing conformance, Relay/NAT, IPv6 or manifest
acceptance. Coordinator's overall gate still fails on direct registration replay.

[Coordinator 7217a98](https://github.com/endless-net/coordinator/tree/7217a989bca078ddcd2175bde920f04aa60a4c1a)
extends the same Client pin with four persistent sockets, TCP and UDP in both
directions. [CI 34601192560](https://github.com/endless-net/coordinator/actions/runs/34601192560/job/103268699745)
confirmed successful nonce exchange before approval withdrawal and unavailable
exchange on the same sockets afterwards. Both applications remained reachable
over loopback and the independent underlay. Reapproval restored fresh traffic.
This is node-approval withdrawal, not a complete port-policy, lease or latency
matrix; another authorized overlay flow remains a separate control case.

[Coordinator 3ba91ff](https://github.com/endless-net/coordinator/tree/3ba91ff4774fbc9dcfc7e0d5084ca1a85bb1a72f)
added a short Signing dependency outage with both real agents reporting the
provider's temporary map/endpoint error. In
[CI 34601934992](https://github.com/endless-net/coordinator/actions/runs/34601934992/job/103271192505),
four established TCP/UDP sockets and fresh exchanges in both directions kept
working with valid cached maps. After automatic recovery the same sockets still
exchanged nonces; no registration request occurred. This does not cover expired
maps/leases, indefinite offline access or a complete Coordinator process outage.

## Real reconnect with the enrolled hostname

[Coordinator baseline CI 34602586998](https://github.com/endless-net/coordinator/actions/runs/34602586998/job/103273334657)
failed on IPC connect after disconnect. Renewal used the operating-system
hostname instead of the custom enrolled name, violating Coordinator's identity
binding. `TestConnectSyncPreservesEnrolledHostname` reproduced the typed binding
error after the consumer double added the missing hostname check.

[Client dcb619a](https://github.com/endless-net/client/tree/dcb619af0d7cf93e7c47a518b1ce381c8f430def)
preserves the enrolled name when renewal has no explicit hostname option.
Format/vet/lint/short checks and all mandatory
[Client CI 34603816925](https://github.com/endless-net/client/actions/runs/34603816925)
jobs passed. Optional external STUN compatibility did not run.

[Coordinator 4a57069](https://github.com/endless-net/coordinator/tree/4a57069ffa17ec5b279264055058f68d5cbcd7ee)
pins this Client. In
[CI 34603841552](https://github.com/endless-net/coordinator/actions/runs/34603841552/job/103277471582),
all five traffic subtests passed. Repeated disconnect preserves public identity,
credential and valid cache while blocking four established sockets and fresh
TCP/UDP in both directions; underlay applications stay alive. Repeated connect
restores applied maps, WireGuard handshake/RX/TX and fresh bidirectional traffic
with the same node IDs. The test distinguishes credential renewal through
`/nodes/register` from a new enrollment, which remains forbidden in this case.

This adds HC-017 real-pair evidence for a running Linux agent and direct IPv4.
Real-pair process restart, reboot, other OSes, old-socket recovery and Relay/NAT
remain separate. Coordinator's overall publication gate still fails on the
existing single-use registration replay defect.

[Coordinator 17b02ea](https://github.com/endless-net/coordinator/tree/17b02ea51b5d9385fb308cac229c0842ee017d00)
extends the same Client pin with forced agent process restarts after successful
IPC disconnect and connect. In
[CI 34604418707](https://github.com/endless-net/coordinator/actions/runs/34604418707/job/103279351728),
all five traffic subtests passed. The driver waits for actual process exit and
starts a new process with the same arguments, without reading private state.
Disconnected intent still blocks established and fresh TCP/UDP in both
directions while underlay applications remain alive. Connected intent restores
applied maps, WireGuard handshake/counters and fresh traffic without another
connect command or new enrollment. Node IDs stay unchanged. The other agent
and applications keep running. This adds HC-018 real-pair Linux/direct IPv4
evidence; reboot, system-service autostart, crash during intent writes and other
OS/transport variants remain separate. Overall provider failure is still the
single-use registration replay defect.

[Coordinator 75856ee](https://github.com/endless-net/coordinator/tree/75856eeac17753ac7d3569ace1b3334b0dcfc76c)
adds actual Coordinator process termination and restart with the same Client pin.
[CI 34605075606](https://github.com/endless-net/coordinator/actions/runs/34605075606/job/103281500242)
passed all six traffic subtests. During confirmed process exit the edge observes
upstream failures and returns HTTP 502 on map/endpoint. Both agents report
control errors while retaining identities, credentials, valid maps and WireGuard
peers. Four existing sockets and fresh TCP/UDP exchanges in both directions
continue to work. Restart uses the same binary, configuration and disposable
YDB without repeated migrations or manual state restoration. After readiness,
both agents recover current applied maps and exchange data on fresh and original
sockets without any registration request. This is a short upstream process
outage with a live edge/storage; map expiry, edge connection loss, long offline,
storage failure and other transports remain separate.

[Coordinator 89438f9](https://github.com/endless-net/coordinator/tree/89438f9c16de17de582d4f4205e1c6ca842d9f02)
adds a real Coordinator port-policy case using its published Management projection
API, with the same Client pin. In
[CI 34605743032](https://github.com/endless-net/coordinator/actions/runs/34605743032/job/103283699810),
all seven traffic subtests passed. Eight sockets cover TCP/UDP on ports 24001
and 24002 in both directions. After policy withdraws 24001, its four established
sockets and fresh overlay probes are blocked while 24002's original sockets
and fresh traffic continue. Retained sockets are checked before and after each
denied exchange; applications on the withdrawn port remain live over underlay.
Regrant restores fresh 24001 traffic without disrupting retained 24002 sockets
or issuing a registration request. Real Management intent/compilation,
directional and correlated grants, local inbound preference, production revoke
SLO and other OS/transport variants remain separate. Overall provider failure
is still registration replay; this does not close HC-027 as a whole.

## Client-only selective withdrawal evidence

[Client 5cd716f](https://github.com/endless-net/client/tree/5cd716fa78fa14410ed1252be58a0cda6667369f)
extends `TestControlPlaneDirectPeerTrafficAndWithdrawal` using only real Client
agents and the contract testserver. A signed replacement map withdraws TCP
24001 while retaining UDP 24002 on the same peer. After the public agent
snapshot confirms application, three iterations require the old TCP socket
and fresh TCP attempts to be blocked while the old UDP socket and fresh UDP
exchanges continue. The destination TCP application remains healthy. Subsequent
deny-all and restoration checks remain in the scenario.

[Client CI 34608000071](https://github.com/endless-net/client/actions/runs/34608000071)
passed all mandatory jobs, including three control-plane repetitions and the
platform/install matrix. Optional external STUN compatibility was not run.
Local format/vet/lint/short checks also passed. This is Client-owned Linux
direct IPv4 evidence for HC-027 / BR-10 / IT-15; it neither depends on a real
backend process nor proves the complete direction/destination/platform matrix
or a production revocation SLO.

## Next work

Reconcile the HC matrix with Client-owned consumer/OS coverage, then implement
missing contract-only testserver scenarios and client regressions. Historical
producer failures are constraints, not tasks to fix outside Client. Keep
contract gaps and platform decisions explicit; do not replace unresolved client
scenarios with generic smoke tests or infer completion from historical P/R runs.
