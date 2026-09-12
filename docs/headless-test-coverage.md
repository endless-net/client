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

The user additionally requires functional confirmation on every supported OS,
using parallel GitHub-hosted runners. The Client CI contract matrix targets
Ubuntu 22.04/24.04 amd64/arm64, Windows 2022/2025 amd64 and macOS 15 ARM/Intel, with fail-fast
disabled and three isolated runner jobs per platform, each with its own text/JSONL
report, compiled inventory, source SHA and repetition identity (24 reports total).
Every matrix job is mandatory for verification and exact-source publication.
This matrix runs the common real-client `TestControlPlane*` suite. The isolated
two-client `TestClientDataplane*` suite currently has a Linux namespace fixture;
Two-client fixture parity and remaining resolver, route and firewall variants
remain explicit work beyond the native cases qualified below; they cannot be
inferred from unrelated passing contracts or installation.

## Evidence levels

- **L**: [installation suite](../tests/installation_test.go), driven by real OS
  services and public CLI/IPC. Platform limits: [installation notes](installation-tests.md).
- **C**: [control-plane suite](../tests/control_plane_test.go), real client
  process against [testcontrol](../internal/testcontrol/server.go) through wire
  contracts, observed through public IPC/CLI. Evidence is attributed to each
  platform below; isolated two-client dataplane evidence remains Linux-only.
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
| HC-003 | L noninteractive installation and enrolled same-artifact reinstall on all eight runners, preserving identity, intent and real TCP access | Interrupted installation and artifact replacement variants |
| HC-004 | L real service and IPC; stopped-service read/mutation/subscription failure and same-identity/intent recovery passed all eight installation runners at `38050bc` | Local authorization and remaining unavailable-service variants |
| HC-005 | C SingleAgentOwnership including lexical/file-symlink aliases, concurrent duplicate rejection and successor startup passed all 24 repetitions at `4f117a0`; Unix SIGTERM requires clean exit | Hard-link aliases, concurrent startup without an existing owner and remaining termination variants |
| HC-006 | L service restart without interactive login | Actual machine reboot and late-network availability |
| HC-007 | C BrowserEnrollment | Client account binding and completion/error variants against the contract testserver |
| HC-008 | C BrowserEnrollment; expiry recovery on six runners; pending CLI interruption/resume and active rejection/reapproval passed all 24 repetitions on eight native runners | Interactive/server-side cancellation and foreign poll authorization |
| HC-009 | C enrollment; P wrong/expired join authorization; R two real CLI registrations | Full authorized/denied attribute and platform variants |
| HC-010 | C RegistrationResponseLoss; D ResponseLossPreservesOperation; historical R distinct node IDs | Client image-cloning and batch variants; historical producer replay failure is an external constraint |
| HC-011 | CLI join-token --ephemeral forwarding has a component test; no C/R lifecycle evidence | Define client normal/crash lifecycle observations; token-option forwarding does not prove expiry |
| HC-012 | D request/proof tests | Real Client allowed/denied registration attributes and effective access against published contract responses |
| HC-013 | C BrowserEnrollment; R registered-node rejection and reapproval restore real traffic | Remaining pending/denial and browser completion variants |
| HC-014 | U recovery matrix | C session expiry, reauthentication and preservation |
| HC-015 | R revoked join key denies a new client while existing map access/renewal survives | Agent/dataplane, expiry and offline variants; node revocation remains separate |
| HC-016 | C initial cached-map status and native direct IPv4/IPv6 TCP/UDP and ICMP echo on all eight runners | Other paths/protocols and denied-access variants |
| HC-017 | C native direct IPv4/IPv6 TCP/UDP and ICMP echo blocked/restored by disconnect/connect and agent restart on all eight runners with original identity; historical R direct TCP/UDP | Remaining established-flow, other paths and operation-failure variants |
| HC-018 | C connected/disconnected intent survives process restart with native IPv4/IPv6 TCP/UDP and ICMP echo checks on all eight runners; historical R traffic checks | Host reboot, crash during intent write and other platform/network variants |
| HC-019 | C durable route-table off/auto passed three times on all eight native platforms; U configuration tests | Other public preferences and live mutation variants |
| HC-020 | C NetworkSelectionBoundary passed all 24 native repetitions: enrolled network listing/selection, disconnected response before/after restart and foreign-network rejection | Product decision for multiple saved profiles and switching; network-scoped selection is not profile support |
| HC-021 | C TLS trust/hostname, unchanged-key intent/traffic, connected/disconnected ordinary and interrupted map-signing rotation, and changed-key native traffic passed all 24 repetitions at qualified source `4f117a0` | Certificate expiry and remaining origin variants |
| HC-022 | C Lifecycle logout; LocalForgetAfterUnconfirmedLogout passed three times on all eight runners | Remaining revocation retry, traffic-retirement and profile semantics |
| HC-023 | C Lifecycle peer projection; R approval changes applied by running agent and WireGuard | Remaining authorization and peer absence variants |
| HC-024 | C two-Client Linux direct traffic; native real Client IPv4/IPv6 TCP/UDP and ICMP echo on all eight runners (increments below); historical R two agents with real Coordinator | ICMP errors/PMTU, IPv6 underlay, Relay/NAT and full policy variants |
| HC-025 | C DNS CLI/proxy, DNS wire lookup and application access by FQDN; eight-platform UDP/TCP lookup, withdrawal, restoration and split-DNS isolation | OS resolver integration, live reload, IPv6 and remaining upstream variants |
| HC-026 | C explicit default/split upstream selection and denied-domain isolation; U DNS/router configuration | System DNS control and IP-access preservation |
| HC-027 | C delta/resync and TCP grant withdrawal with retained UDP; native IPv4/IPv6 TCP/UDP port withdrawal/restoration, ICMP denial under TCP-only grants, ICMP-only TCP/UDP isolation and exact destination denial/recovery on all eight runners; historical R node/port withdrawal | Remaining Client direction/destination correlation, ICMP errors/PMTU and other policy/transport variants |
| HC-028 | C native single-Relay and two-Relay failover IPv4/IPv6 TCP/UDP, outage denial, recovery and agent restart passed all 24 native repetitions | NAT, direct/Relay transitions, existing-session failover, healthy-backup failback and remaining Relay variants |
| HC-029 | U endpoint/reconnect tests | External network change and stale response ordering |
| HC-030 | C typed/malformed errors; R fresh and established direct TCP/UDP survive short Signing dependency and Coordinator process outages and recover without registration | Map/lease expiry, edge/transport/storage outage and remaining variants |
| HC-031 | Central ACL tests are not evidence of a local inbound preference | No local inbound toggle found in current CLI/IPC/config; product and contract gap |
| HC-032 | C RoutedResource passed three times on all eight native platforms: IPv4/IPv6 TCP/UDP through an IP forwarding peer, route withdrawal and recovery | Full Client router roles, SNAT, HA and remaining resource variants |
| HC-033 | C durable route installation disable/restore passed three times on all eight native platforms; U configuration tests | Per-resource selection and remaining route-selection semantics |
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
| HC-050 | U sharing/encrypted engine tests | C grant/consent/revoke/expiry/new identity against published contracts without internal access |
| HC-051 | U service discovery/runtime | Logical service host approval, loss and actual traffic |
| HC-052 | L/C bounded IPC waits; native independent event subscriptions/cancellation/restart passed all 24 repetitions at qualified source `88f335e` | Remaining public readiness conditions, slow consumers and noninteractive timeout results |
| HC-053 | L/C structured IPC; native request validation, non-object rejection, body-size boundaries and event subscribers passed all 24 repetitions at qualified source `88f335e`; D RPC authorization | Remaining machine output/errors and local-user authorization variants |
| HC-054 | No C/R evidence audited | Container persistent versus ephemeral lifecycle |
| HC-055 | No C/R evidence audited | Userspace/no-TUN product scope and application proxy behavior |
| HC-056 | C status/diagnostics; U path diagnostics | Distinguishable control/path/DNS/application failures |
| HC-057 | C export/reuse, retention and IPv4 UDP flow consent/retry/expiry passed all 24 native repetitions at `a9a1a21`; D FlowConsentAndIdempotency; U flow tests | Crash recovery, comprehensive redaction and remaining retention/flow variants |
| HC-058 | L version and runtime platform; native IPC negotiation/restart passed all 24 repetitions at `a9a1a21` | Artifact mismatch and exact release identity |
| HC-059 | No qualified independent client-endorsed node-admission evidence; server trust/recovery tests belong to HC-021 | BR-05 / Q-05 product decision: independently signed node admission, unsigned-node denial, signing-device loss and recovery |
| HC-060 | L same-source installation | Updating existing enrolled installation, artifact gates and restored access |
| HC-061 | Publication gate, fixture tests and real GitHub API check | Supported update channels, artifact acceptance and update failures |
| HC-062 | C process restart during outage | Repair of damaged installation separately from identity reset |
| HC-063 | U local-forget/recovery tests; local forget deliberately retains identity | Full identity-reset product/interface gap; do not infer it from local forget |
| HC-064 | L uninstall | Explicit binary/state retention versus full removal, enrolled machine |
| HC-065 | C terminal revoke, native direct IPv4/IPv6 TCP/UDP retirement and ICMP echo denial across agent restart on all eight runners; Linux direct TCP/UDP; historical R deleted Client sync denied and peer withdrawn | Offline leases and remaining path/platform variants, separately verified local removal |

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

## Client-only credential retirement and probe regression

[Client ab2199d](https://github.com/endless-net/client/tree/ab2199d58cd364e41d8a693f06cca038cee2132f)
adds [credential retirement](../tests/control_plane_retirement_test.go) to
`TestControlPlaneDirectPeerTrafficAndWithdrawal`. Before revocation the real
client exchanges TCP and UDP application payloads on persistent sockets. A
terminal credential response must clear public enrollment/map status and block
both existing sockets and fresh overlay traffic, including after agent restart.
The receiver retains its peer map, and its applications remain reachable over
the underlay. No new registration request may occur. Observations use public
IPC, application traffic and the contract testserver's request events.

The first [CI run](https://github.com/endless-net/client/actions/runs/34608602369)
failed earlier in the policy scenario: the persistent probe process closed.
[Client d80ab0d](https://github.com/endless-net/client/tree/d80ab0d006c044d081e79692e91219d6aa47de6c)
fixes a reproduced probe defect: late full or partial TCP replies after a
deadline must not corrupt the next exchange. Only an echo of the current nonce
proves success; an old known reply is discarded and an unknown reply remains
fatal. Regression tests also prove that an old reply alone cannot establish
current reachability. This changes the test utility, not Client access policy.

[Control-plane job 103295833928](https://github.com/endless-net/client/actions/runs/34609410344/job/103295833928)
passed all three repetitions (16.95s, 16.94s and 16.90s for the direct scenario),
including retirement. The overall run failed `Verify (Windows)` because the
DNS proxy could not bind UDP on a TCP-selected ephemeral port. Thus this run
proves the bounded Linux consumer scenario, not a successful complete CI gate.
The DNS listener follow-up alternates which transport selects the ephemeral
port; explicit ports never move, and failed reservations are closed. Its
regressions are component tests and do not replace contract-only C evidence.

[Client 90faa0e](https://github.com/endless-net/client/tree/90faa0eaeee1d772763e510aff701114d32be048)
contains that DNS fix. [CI 34610099868](https://github.com/endless-net/client/actions/runs/34610099868)
passed every mandatory job, including Windows Verify, the platform/installation
matrix and all three control-plane repetitions. The direct scenario passed in
16.98s, 16.92s and 16.98s. Optional external STUN compatibility was skipped and
is not part of this evidence. Local format, vet, lint and short tests passed.
This establishes the stated direct IPv4 Linux retirement behavior against the
contract testserver; offline lease expiry, other paths/platforms and full local
removal remain separate HC-065 gaps.

## Parallel platform contract rollout

[Client 321706d](https://github.com/endless-net/client/tree/321706dc0f4c08bae5769fe1456b149c74a3ce84)
adds `TestControlPlaneBrowserEnrollmentExpiryRecovery`. It expires a pending
request in the testserver, requires a new request, resumes that replacement
from a separate CLI process and enrolls only after explicit approval. The double
retains an expired result for an exact operation replay. A component regression
then reproduced the Client's reuse of the old operation ID. The correction in
[c5a6402](https://github.com/endless-net/client/tree/c5a6402f3ab9ff83191332ff15d0f0963363c349)
generates a new operation ID and signs it with the existing device identity.
[CI 34610874047](https://github.com/endless-net/client/actions/runs/34610874047)
passed all mandatory jobs and three Linux expiry-scenario repetitions. The
preceding test-only run was cancelled and is not failure evidence; the recorded
local regression supplied the before/after reproduction.

[Client 9f4ef49](https://github.com/endless-net/client/tree/9f4ef49d5c75bbf2db421351bc497baea154404f)
adds the six-platform matrix, named-pipe Client driver and required publication
checks. [First matrix run 34611414889](https://github.com/endless-net/client/actions/runs/34611414889)
executes 11 top-level common contract scenarios three times per platform.
Both Ubuntu versions and both Windows versions passed all 33 outcomes without skips.
Both macOS architectures passed 30 and failed all three `TestControlPlaneLifecycle`
repetitions: Darwin `ifconfig` rejected IPv4 assignment on utun with
`Destination address required`. This is a Client platform defect, not a waiver
or a fixture skip. The fix adds the IPv4 point-to-point destination, consistent
with [WireGuard's Darwin address setup](https://git.zx2c4.com/wireguard-tools/tree/src/wg-quick/darwin.bash).
The first run did not verify that correction. The overall first matrix run failed, as required by the
mandatory platform gate.

[Client 4716b53](https://github.com/endless-net/client/tree/4716b53e25c9d35ba951ff33f8d0c8e25bdd0c98)
contains the Darwin correction and address-command regression.
[CI 34612021750](https://github.com/endless-net/client/actions/runs/34612021750)
passed every mandatory job, including the six contract jobs, six installation
jobs, platform verification and Linux dataplane. Per-job logs confirm the same
11 top-level scenarios and three repetitions on all six runners: 198 successful
top-level outcomes, with no failed or skipped top-level scenarios.

| Runner | Successful / attempted | Sum of scenario times | Evidence |
| --- | --- | --- | --- |
| Ubuntu 22.04 amd64 | 33 / 33 | 38.67s | [job](https://github.com/endless-net/client/actions/runs/34612021750/job/103304596630) |
| Ubuntu 24.04 amd64 | 33 / 33 | 20.89s | [job](https://github.com/endless-net/client/actions/runs/34612021750/job/103304596701) |
| Windows 2022 amd64 | 33 / 33 | 164.78s | [job](https://github.com/endless-net/client/actions/runs/34612021750/job/103304596820) |
| Windows 2025 amd64 | 33 / 33 | 120.28s | [job](https://github.com/endless-net/client/actions/runs/34612021750/job/103304596627) |
| macOS 15 arm64 | 33 / 33 | 22.14s | [job](https://github.com/endless-net/client/actions/runs/34612021750/job/103304596581) |
| macOS 15 Intel | 33 / 33 | 31.27s | [job](https://github.com/endless-net/client/actions/runs/34612021750/job/103304596540) |

Times include fixture/process/OS setup within each scenario and are not product
throughput or latency benchmarks. The Windows runs take materially longer, so
the suite retains bounded operation deadlines and a 12-minute job test deadline.
Optional external STUN compatibility did not execute. These results establish
the common contract suite on each platform, not completion of HC-001–HC-065 or
native Windows/macOS application traffic, routing, resolver and firewall checks.

## Six-platform DNS wire increment

[Client c6bc9fb](https://github.com/endless-net/client/tree/c6bc9fb35fd7164e711d3c9f5fcfa5bdaa700408)
adds [TestControlPlaneDNSWireRecovery](../tests/control_plane_dns_test.go).
The real `dns serve` CLI receives UDP and TCP DNS questions. Three map phases
require a peer's A record to appear, disappear with NXDOMAIN, and reappear.
Absent private names remain NXDOMAIN; public names use the explicit default
upstream, split-domain names use their selected upstream, and a more-specific
blocked split domain returns SERVFAIL. Both DNS fixtures record wire questions:
only the intended domain may reach each upstream. No private client state is
read, and no real backend is required.

[CI 34612884945](https://github.com/endless-net/client/actions/runs/34612884945)
passed all three new DNS repetitions on each of the six platforms. The overall
run failed because one Windows 2025 lifecycle repetition failed its public
`service disconnect` call. The original driver withheld CLI output and did not
classify the cause, so that log alone does not prove a timeout or runtime defect.
The driver had imposed a 3-second mutation timeout, while the CLI default is
30 seconds. The follow-up uses that default, bounds the process to 35 seconds,
reports slow operation duration and allowlisted failure categories, and still
fails on any unsuccessful operation. No automatic mutation retry is added.

Each DNS proxy invocation loads a fresh verified map. These results do not
establish live reload, automatic OS resolver setup, IPv6 DNS, TCP upstream
fallback, or actual overlay application traffic on Windows/macOS.

[Client 0aa41ff](https://github.com/endless-net/client/tree/0aa41ffaa430db19e2bcec9965eb55afb2990fac)
contains the driver follow-up. [CI 34613835101](https://github.com/endless-net/client/actions/runs/34613835101)
passed every mandatory job. Comparing the logs confirms an identical set of
12 top-level contract scenarios with three successful repetitions per platform:
36/36 on every runner, 216/216 overall. The DNS-specific repetitions were:

| Platform | DNS scenario durations | Evidence |
| --- | --- | --- |
| Ubuntu 22.04 | 0.23s, 0.12s, 0.19s | [job](https://github.com/endless-net/client/actions/runs/34613835101/job/103310702629) |
| Ubuntu 24.04 | 0.17s, 0.11s, 0.12s | [job](https://github.com/endless-net/client/actions/runs/34613835101/job/103310702626) |
| macOS ARM | 0.38s, 0.23s, 0.33s | [job](https://github.com/endless-net/client/actions/runs/34613835101/job/103310702671) |
| macOS Intel | 0.83s, 0.45s, 0.44s | [job](https://github.com/endless-net/client/actions/runs/34613835101/job/103310702558) |
| Windows 2022 | 9.22s, 2.63s, 2.62s | [job](https://github.com/endless-net/client/actions/runs/34613835101/job/103310702571) |
| Windows 2025 | 7.67s, 3.40s, 3.39s | [job](https://github.com/endless-net/client/actions/runs/34613835101/job/103310702563) |

These durations include process and fixture setup. No service operation in
this run triggered the greater-than-or-equal-to-three-second diagnostic; the
earlier unclassified Windows IPC failure therefore remains unexplained, rather
than a proven timeout fixed by increasing a limit. Future failures must use the
new diagnostic. Optional external STUN compatibility was skipped. Local
format/vet/lint/short checks passed, and no Client runtime change was made by
this DNS/driver increment.

## Native IPv4 UDP and peer ACL increment

[TestControlPlaneNativeUDPTraffic](../tests/control_plane_native_udp_test.go)
uses one real Client with the runner's native TUN and routes. A reference
WireGuard peer uses a channel TUN, so its overlay address is not assigned to
the host and application traffic cannot take a local-address shortcut. The
contract testserver publishes the peer and signed ACL changes; application
assertions require fresh nonce echoes through the encrypted path. No producer
runtime or private Client state is used.

The scenario checks both fresh and established UDP flows to two ports, blocks
one port while preserving the other, restores both grants on the same sockets,
then disconnects and requires new traffic to fail. Reference peer counters
report handshake message counts and decrypted/echoed packet counts without
printing payloads or keys.

The initial fixture runs failed before establishing traffic:
[34615012933](https://github.com/endless-net/client/actions/runs/34615012933)
used a loopback endpoint, which Client deliberately excludes from direct paths;
[34615766536](https://github.com/endless-net/client/actions/runs/34615766536)
selected a native address but lacked the return endpoint required by the
pinned Tailscale WireGuard engine. The fixture now configures that endpoint
from the Client's public IPC listen port. Neither result proved a Client
handshake defect.

[Client 9e64e19](https://github.com/endless-net/client/tree/9e64e1953b462a6415e26fa471d4e0b2a539ed0e)
and [CI 34616766281](https://github.com/endless-net/client/actions/runs/34616766281)
established initial traffic on all six platforms and exposed distinct failures:

| Runners | New scenario failure, three repetitions each |
| --- | --- |
| Ubuntu 22.04 / 24.04 | Previously denied UDP socket did not recover after restoring its grant |
| Windows 2022 / 2025 | Retained UDP socket stopped responding after the other port's grant was withdrawn |
| macOS ARM / Intel | Withdrawn UDP socket continued delivering nonce echoes |

Every platform passed the other 36 top-level outcomes. All six contract jobs
and the aggregate run failed, as required. In the implementation, peer ACL
enforcement depended on Linux hooks which Windows/Darwin routers did not apply;
changing those hooks also caused OS interface/route reconfiguration.

[Client a8d38af](https://github.com/endless-net/client/tree/a8d38af969156a46d979de5dbd91aa6e799def5e)
moves userspace peer ACL enforcement before WireGuard encryption on all OSes.
Every outbound packet is checked, including established flows. Rules retain
destination/port correlation and longest-prefix ordering. While applying a
map, the old/new permission intersection is enforced; newly granted access
opens only after runtime apply succeeds, and a failed apply closes the filter.
Peer port changes no longer alter router hooks or tear down the native
interface. Exported kernel WireGuard configuration still owns its Linux hooks.
Local format, vet, lint and short tests passed, including withdrawal/restoration,
failed apply, protocol correlation and IPv4/IPv6 prefix regressions. Native CI
verification is recorded separately below.

[CI 34617628535](https://github.com/endless-net/client/actions/runs/34617628535)
passed on the exact runtime source `a8d38af969156a46d979de5dbd91aa6e799def5e`.
Log comparison confirms the identical 13 top-level scenarios, each passing
three times on every runner: 39/39 per platform, 234/234 overall, with no
failed or skipped top-level scenarios. The native scenario's assertions were
unchanged between the failing `9e64e19` run and this successful runtime fix.

| Runner | Native UDP repetitions | Common suite | Evidence |
| --- | --- | --- | --- |
| Ubuntu 22.04 amd64 | 13.32s, 13.33s, 13.31s | 39/39 | [job](https://github.com/endless-net/client/actions/runs/34617628535/job/103323401714) |
| Ubuntu 24.04 amd64 | 13.31s, 13.33s, 13.34s | 39/39 | [job](https://github.com/endless-net/client/actions/runs/34617628535/job/103323401805) |
| macOS 15 arm64 | 13.70s, 13.61s, 13.61s | 39/39 | [job](https://github.com/endless-net/client/actions/runs/34617628535/job/103323401629) |
| macOS 15 Intel | 13.77s, 13.96s, 13.83s | 39/39 | [job](https://github.com/endless-net/client/actions/runs/34617628535/job/103323402006) |
| Windows 2022 amd64 | 17.15s, 17.43s, 17.12s | 39/39 | [job](https://github.com/endless-net/client/actions/runs/34617628535/job/103323401876) |
| Windows 2025 amd64 | 18.07s, 18.07s, 18.07s | 39/39 | [job](https://github.com/endless-net/client/actions/runs/34617628535/job/103323401831) |

All six installation jobs, platform verification, the Linux two-real-Client
dataplane job and aggregate `verify` also passed. Optional external STUN
compatibility did not execute. Durations include fixture startup and intentional
negative-probe deadlines; they are not throughput or latency benchmarks.

This is native direct IPv4 UDP evidence for parts of HC-024/HC-027, including
selective port enforcement and recovery. It does not establish full coverage of
those use cases, IPv6 packet delivery, TCP dataplane on Windows/macOS, automatic
OS resolver integration, Relay/NAT, every firewall direction, or all HC-001–HC-065.

## Native connection intent and credential retirement increment

[Client 432dbf2](https://github.com/endless-net/client/tree/432dbf20ade92d7f6671d8b3171236e74a9b6d90)
extends the native UDP scenario for HC-017/HC-018/HC-065. After disconnect, a new
agent process must retain disconnected intent and enrollment while new UDP
traffic remains blocked. Explicit connect, repeated connect and another process
restart must restore traffic with the same public node and WireGuard identity,
without creating another enrollment. Credential refresh through the registration
API is allowed only for the same node and validated identity binding. A restart
while disconnected must send no registration/refresh request. The fixture updates only its return UDP
endpoint from public IPC when the Client chooses a new listen port.

The scenario then establishes a fresh UDP session and revokes the node
credential through the contract testserver. Public IPC must report
`needs_enrollment`, no node ID, no credential and no cached map. Both the old
session and new probes must fail before and after another agent restart, with
no automatic registration attempt. The reference peer retains its original
keys, routes and echo handler throughout retirement; fixture-side withdrawal
cannot account for the loss of access.

[CI 34618687176](https://github.com/endless-net/client/actions/runs/34618687176)
reached reconnect with working UDP traffic on Ubuntu and macOS ARM, then failed
an incorrect test assertion that counted credential refresh as new enrollment.
The existing registration contract validates the current node/key/fingerprint
binding for credential refresh. The test must distinguish that operation from
creating a new node; it must still reject any registration/refresh attempt
after terminal retirement. This run did not reach the retirement assertions.
The corrected source superseded it: both Windows jobs and macOS Intel were
cancelled, so their terminal results provide no completed-run evidence.

[Client c38df03](https://github.com/endless-net/client/tree/c38df030171f3baa05a9e4706000003ae7c08954)
corrects the assertion using the fixture's independently validated registration
outcomes. Local format/vet/lint/short checks passed.
[CI 34619235033](https://github.com/endless-net/client/actions/runs/34619235033)
passed on this exact source. Comparing all six job logs confirms the same 13
top-level scenarios, each passing three times: 39/39 per platform, 234/234 in
total, with no failed or skipped top-level scenarios. Each log contains three
executions of the final retirement-restart phase, followed by successful
scenario completion. The suite size is unchanged: this increment extends an
existing scenario rather than adding another top-level test.

| Runner | Extended native scenario durations | Evidence |
| --- | --- | --- |
| Ubuntu 22.04 amd64 | 29.72s, 29.71s, 29.75s | [job](https://github.com/endless-net/client/actions/runs/34619235033/job/103328812909) |
| Ubuntu 24.04 amd64 | 29.64s, 29.65s, 29.65s | [job](https://github.com/endless-net/client/actions/runs/34619235033/job/103328813003) |
| macOS 15 arm64 | 29.99s, 30.06s, 30.18s | [job](https://github.com/endless-net/client/actions/runs/34619235033/job/103328813244) |
| macOS 15 Intel | 30.95s, 30.62s, 30.59s | [job](https://github.com/endless-net/client/actions/runs/34619235033/job/103328813285) |
| Windows 2022 amd64 | 38.63s, 38.64s, 38.57s | [job](https://github.com/endless-net/client/actions/runs/34619235033/job/103328812996) |
| Windows 2025 amd64 | 40.45s, 41.02s, 41.24s | [job](https://github.com/endless-net/client/actions/runs/34619235033/job/103328812995) |

All required platform verification, installation, Linux two-real-Client
dataplane and aggregate jobs passed. Optional external STUN compatibility was
skipped. Durations include process/interface setup and deliberate negative
probe deadlines and are not performance benchmarks.

This is process restart evidence, not host reboot, service-manager restart,
offline lease expiry, TCP/IPv6 retirement or local software removal. Windows
process restart uses termination; installation-suite service observations are
separate. No runtime Client change is included in this test increment.

## Browser enrollment expiry during active polling

[Client d3cd7ca](https://github.com/endless-net/client/tree/d3cd7caee0c78af44b4e90e89d625f6077475109)
adds [TestControlPlaneBrowserEnrollmentExpiresDuringPolling](../tests/control_plane_enrollment_poll_test.go)
for HC-008. The real `up` CLI runs with a bounded approval wait. Only after the
testserver observes at least two status requests, and the CLI is still running,
does the fixture expire its pending request. The same invocation must report
expiry; it must not attempt completion or create a node credential.

A later CLI process must create exactly one distinct replacement. The expired
request remains unapprovable; only explicit approval of the replacement permits
enrollment. A real agent then exposes the single approved node through public
IPC. Assertions use HTTP request observations, public CLI output and IPC rather
than private persisted state. No new testserver behavior or Client runtime
change is added.

Local format/vet/lint/short checks passed.
[CI 34620489439](https://github.com/endless-net/client/actions/runs/34620489439)
passed on exact source `d3cd7caee0c78af44b4e90e89d625f6077475109`. Comparing all
six job logs confirms the same 14 top-level scenarios, each passing three
times: 42/42 per platform, 252/252 overall, with no failed or skipped top-level
scenarios. The new polling-expiry scenario outcomes were:

| Runner | Three successful repetitions | Evidence |
| --- | --- | --- |
| Ubuntu 22.04 amd64 | 2.09s, 2.09s, 2.09s | [job](https://github.com/endless-net/client/actions/runs/34620489439/job/103333416543) |
| Ubuntu 24.04 amd64 | 3.01s, 2.29s, 2.34s | [job](https://github.com/endless-net/client/actions/runs/34620489439/job/103333416726) |
| macOS 15 arm64 | 2.10s, 2.22s, 2.14s | [job](https://github.com/endless-net/client/actions/runs/34620489439/job/103333416272) |
| macOS 15 Intel | 2.28s, 2.25s, 2.28s | [job](https://github.com/endless-net/client/actions/runs/34620489439/job/103333416443) |
| Windows 2022 amd64 | 3.48s, 3.45s, 3.46s | [job](https://github.com/endless-net/client/actions/runs/34620489439/job/103333416767) |
| Windows 2025 amd64 | 4.00s, 3.86s, 3.71s | [job](https://github.com/endless-net/client/actions/runs/34620489439/job/103333416347) |

All installation, platform verification, Linux dataplane and aggregate jobs
also passed. Optional external STUN compatibility did not execute. Timings
include process/fixture setup and the contract polling interval; they are not
performance benchmarks. Cancellation and foreign poll authorization remain
separate HC-008 variants; no browser UI or real OIDC provider is involved.

## Native IPv6 UDP increment

[Client f63e7a1](https://github.com/endless-net/client/tree/f63e7a1e27c394792b0221ccb1c652eb23cd6bc0)
adds `TestControlPlaneNativeIPv6UDPTraffic` alongside the IPv4 scenario in
[the native traffic suite](../tests/control_plane_native_udp_test.go). Both
invoke the same assertions for two-port UDP delivery, selective grant withdrawal
and restoration, disconnected/connected intent across agent restart, and
terminal credential retirement with established and fresh traffic checks.

The IPv6 scenario supplies a dual-stack signed map with a ULA node address and
publishes an IPv6-only route to the reference peer. The real Client configures
the OS interface and route; the peer's IPv6 overlay address is not assigned to
the runner host. Packetprobe chooses IPv6 for the literal IPv6 destination,
without IPv4 fallback. The reference channel-TUN peer echoes fixed-header IPv6
UDP packets with the existing 32-byte nonce. Unit checks verify its mandatory
UDP checksum and rejection of zero checksums, unrelated addresses/ports,
truncation and unsupported extension/fragment headers.

The encrypted WireGuard underlay remains IPv4. This increment cannot prove
IPv6 underlay reachability, IPv6 DNS resolution, TCP/ICMP, IPv6 fragmentation,
Relay/NAT or performance. No Client runtime or published contract change is
included. Local format/vet/lint/short checks passed; actual native outcomes
belong to [CI 34621919584](https://github.com/endless-net/client/actions/runs/34621919584).
That run passed on exact source `f63e7a1e27c394792b0221ccb1c652eb23cd6bc0`.
Comparison of all six job logs confirms the same 15 top-level scenarios, each
passing three times: 45/45 per platform, 270/270 overall, with no failed or
skipped top-level scenarios. The IPv6-specific repetitions were:

| Runner | Three successful IPv6 repetitions | Evidence |
| --- | --- | --- |
| Ubuntu 22.04 amd64 | 21.81s, 22.12s, 22.08s | [job](https://github.com/endless-net/client/actions/runs/34621919584/job/103337778579) |
| Ubuntu 24.04 amd64 | 21.86s, 21.87s, 21.73s | [job](https://github.com/endless-net/client/actions/runs/34621919584/job/103337778622) |
| macOS 15 arm64 | 22.33s, 22.15s, 22.26s | [job](https://github.com/endless-net/client/actions/runs/34621919584/job/103337778509) |
| macOS 15 Intel | 22.96s, 23.06s, 23.51s | [job](https://github.com/endless-net/client/actions/runs/34621919584/job/103337778518) |
| Windows 2022 amd64 | 30.65s, 30.70s, 30.38s | [job](https://github.com/endless-net/client/actions/runs/34621919584/job/103337778587) |
| Windows 2025 amd64 | 32.54s, 36.61s, 32.91s | [job](https://github.com/endless-net/client/actions/runs/34621919584/job/103337778571) |

All required installation, platform verification, Linux two-real-Client
dataplane and aggregate jobs also passed. Optional external STUN compatibility
was skipped. Durations include interface/process setup and negative-probe
deadlines; they are not IPv4-versus-IPv6 performance measurements.

## Native TCP protocol-stack increment

[Client d6fcfc3](https://github.com/endless-net/client/tree/d6fcfc30f4be9959b3a0cc2ae5fd40421ddfc1b5)
adds native TCP scenarios for IPv4 and IPv6 to the same six-runner matrix.
[The reference TCP peer](../internal/testwireguard/tcp.go) uses the pinned
WireGuard module's netstack with two TCP listeners. Its overlay address exists
only in that stack; the Client uses the runner's real OS TCP stack, TUN and
routes. Successful assertions require complete fresh 32-byte nonce echoes.
Listeners, accepted connections and worker goroutines are closed before the
reference WireGuard device is removed.

Both TCP variants require fresh and established sessions on two ports. A
signed ACL update withdraws one port while the other established session and
fresh connections must keep working. Restoring the grant must admit fresh
connections on both ports. The denied TCP connection itself may terminate or
enter retransmission backoff; this test does not require its immediate reuse.
Disconnected/connected intent, agent restart and terminal credential retirement
use the same public CLI/IPC and traffic assertions as the UDP cases.

The newly explicit indirect requirements in `go.mod` use the versions already
selected by the pinned WireGuard module: gVisor, btree and x/time. No dependency
version or Client runtime behavior changes. Local format/vet/lint/short checks
passed on Windows with the current Go toolchain.
[CI 34623788903](https://github.com/endless-net/client/actions/runs/34623788903)
passed for that exact source commit. All six runner logs contain the same 17
top-level contract scenarios, each passing three times: 306/306 outcomes, with
no failures or top-level skips. Both new TCP scenarios passed on every runner.

| Runner | TCP/IPv4 repetitions (seconds) | TCP/IPv6 repetitions (seconds) |
| --- | --- | --- |
| Ubuntu 22.04 amd64 | 31.71, 31.73, 31.72 | 23.85, 23.85, 23.81 |
| Ubuntu 24.04 amd64 | 31.70, 31.68, 31.66 | 23.80, 23.80, 23.88 |
| macOS 15 ARM | 32.05, 32.07, 32.28 | 24.14, 23.88, 24.19 |
| macOS 15 Intel | 33.28, 33.57, 33.18 | 25.57, 26.47, 25.29 |
| Windows 2022 amd64 | 38.66, 38.53, 38.62 | 30.47, 30.50, 30.54 |
| Windows 2025 amd64 | 42.35, 44.21, 42.41 | 33.58, 35.55, 33.72 |

All six installation jobs, three platform verification jobs, Linux dataplane
scenarios and the aggregate gate also passed. Optional external STUN did not
execute. Durations include process/interface setup and intentional negative
deadlines; Windows takes longer, but this is not a transport performance comparison.
IPv6 still uses an IPv4 WireGuard underlay. These tests do not qualify lossy-network
TCP recovery, Relay/NAT or all policy directions, or complete HC-001–HC-065.

## Native ICMP policy and lifecycle increment

[Client 5ad2709](https://github.com/endless-net/client/tree/5ad27093849daf566bf34773e50e29a61312db45)
extends the native TCP/IPv4 and TCP/IPv6 scenarios with OS ICMP echo probes.
The existing reference netstack handles ICMP; its address remains absent from
the host. No Client runtime changes or dependency/version increases are involved.

Echo must succeed with the unrestricted peer, fail three times under the
TCP-only port grant while authorized TCP still works, and recover when the
original peer grant returns. Disconnect and disconnected restart must deny
echo. Reconnect and connected restart must restore it with the same enrollment.
Terminal credential revocation must deny echo before and after agent restart,
while the reference peer remains running with unchanged keys and addresses.

The driver requires a successful native ping process and an echo-reply line
from the exact numeric peer address with RTT data. An unreachable response,
aggregate receive count or another source address cannot satisfy the assertion.
Unit cases exercise those false-positive boundaries. Unix output uses the C
locale; the Windows driver targets the English GitHub-hosted images. The probe
has a three-second process bound and sends one request per invocation. These
are native OS echo checks, not the packetprobe TCP/UDP nonce protocol.
CLI details follow [Microsoft ping](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/ping)
and [Apple ping6 source](https://github.com/apple-oss-distributions/network_cmds/blob/main/ping6.tproj/ping6.c).

Local format/vet/lint/short checks passed.
[CI 34625686577](https://github.com/endless-net/client/actions/runs/34625686577)
completed all six jobs: 294 PASS and 12 FAIL outcomes. Both Ubuntu runners
passed 51/51. Both macOS runners failed the IPv4 TCP scenario's new ICMP denial
assertion in all three repetitions; both Windows runners failed the IPv6 TCP
scenario's new positive ICMP assertion in all three repetitions. All other
common scenarios passed. This is not six-platform ICMP qualification. The
original failure message did not identify the phase or include ping output,
so these results alone do not establish a Client defect or a probe defect.

The diagnostic follow-up labels each ICMP phase, records native ping output
only on unexpected results, observes reference WireGuard transport packet
counts and probes the numeric address before publishing the reference peer.
It preserves every reachability assertion. A reply outside the Client tunnel
is a possible address collision to investigate, not an established cause.
The common test-process budget becomes 15 minutes within the existing 20-minute
job bound: the preceding successful Windows 2025 TCP run consumed 715 seconds
of the old 720-second budget before these additional probes. Individual CLI,
ping and application-operation deadlines are unchanged. The failed ICMP run
completed its assertions and was not a timeout failure.
The suite still has 17 top-level scenarios, each repeated three times on six
runners; these are new assertions within two existing scenarios. IPv6 overlay
still travels over an IPv4 underlay. Explicit ICMP-only grants, inbound policy,
ICMP errors/PMTU, IPv6 underlay and Relay/NAT remain separate coverage gaps.

### ICMP probe corrections and platform report gate

[Diagnostic source 3b00533](https://github.com/endless-net/client/tree/3b00533eaf2a44ccbf84e027e7d0f5112256890c)
and [CI 34627036920](https://github.com/endless-net/client/actions/runs/34627036920)
identify two test defects. Both macOS runners receive IPv4 echo replies before
the reference peer is published and after Client disconnect, with zero reference
transport packets during the latter probe. The shared-space address
`100.94.0.20` is reachable outside the fixture's tunnel; the assertion was testing
another network path. Native IPv6 ICMP completes on macOS. Both Windows runners report
a real initial IPv6 echo reply and reference transport packets, but a trailing
space in the reply line makes the original parser reject it. These observations
do not establish a Client runtime defect.

The correction moves the native fixture's IPv4 network to `198.18.94.0/24`,
within IANA's [benchmarking range](https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry.xhtml).
Before any Client setup, an ICMP reply from the selected fixture address now
fails with an explicit address-collision error. No runner firewall/drop route
is introduced to manufacture a denial. All existing traffic and lifecycle
assertions remain required. The Windows parser permits trailing horizontal
whitespace, with the observed IPv6 line and a non-whitespace suffix rejection
as regression cases. IPv6 overlay and the IPv4 underlay are unchanged.

The same increment adds [verify-contract-results](../tools/verify-contract-results/main.go)
to the existing required `verify` job. It downloads the six artifacts from its
own workflow run and compares each compiled test inventory, source SHA and
JSONL execution stream. All declared scenarios must start and pass exactly
three times on every platform; root/subtest skips or failures, missing or
unequal inventories, wrong source identities, missing reports and incomplete
package completion fail the gate. Unit regressions cover those failure cases
and replay reduced action/package/test events from the first ICMP run's real
Ubuntu JSONL artifact. No provider or Infrastructure work is involved.
[Corrected source d2be781](https://github.com/endless-net/client/tree/d2be7819c77a0bc95c23eff54094845b89757915)
passed [CI 34628269044](https://github.com/endless-net/client/actions/runs/34628269044).
All six runner logs have the same 17 scenarios, each passing three times.
The new required report gate also completed successfully and reported 306 PASS
outcomes with no skips. All installation, platform verification and Linux
dataplane jobs passed; optional external STUN did not execute. Both native
TCP scenarios, including their IPv4/IPv6 ICMP assertions, passed every repetition.
Neither preceding failing ICMP run is passing evidence. This confirms the
bounded echo/policy/lifecycle cases on these six runners, not all ICMP behavior
or Linux ARM, which is addressed by the expansion below.

## Linux ARM coverage expansion

The release audit finds an existing supported artifact without native runtime
evidence in the preceding matrix: [APT publication](../.github/workflows/publish-apt.yml)
already builds and indexes both amd64 and arm64 packages. Successful amd64
jobs and arm64 cross-compilation do not qualify the Linux ARM client.

The expansion adds `ubuntu-22.04-arm` and `ubuntu-24.04-arm` to both installation
and contract matrices, using [standard hosted ARM runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).
The Debian job builds for the runner's actual Go architecture and verifies the
package Architecture field against dpkg before installation. It retains the
existing release version. Contract executables and packet probes compile and
run natively on each ARM runner, exercising the same declared scenario inventory.

Both new installation jobs and both contract jobs become mandatory in the
exact-source publication verifier. The report comparer requires eight matching
artifacts, with explicit missing-ARM regression cases. Windows ARM is not a
declared release artifact; macOS keeps its existing source/CI support boundary.

[Source 4bf3263](https://github.com/endless-net/client/tree/4bf326393beac8bff89f444e83637007fe981a09)
passed [CI 34629704362](https://github.com/endless-net/client/actions/runs/34629704362).
All eight logs contain the same 17 common scenarios, each passing three times:
408/408 outcomes. The required automated comparer independently reports the
same count with no skips. All eight installation jobs, three platform
verification jobs and Linux dataplane scenarios also passed. Optional external
STUN did not execute.

| Native runner | Contract outcomes | Installation |
| --- | --- | --- |
| Ubuntu 22.04 amd64 | 51/51 PASS | PASS |
| Ubuntu 24.04 amd64 | 51/51 PASS | PASS |
| Ubuntu 22.04 arm64 | 51/51 PASS | PASS |
| Ubuntu 24.04 arm64 | 51/51 PASS | PASS |
| Windows 2022 amd64 | 51/51 PASS | PASS |
| Windows 2025 amd64 | 51/51 PASS | PASS |
| macOS 15 ARM | 51/51 PASS | PASS |
| macOS 15 Intel | 51/51 PASS | PASS |

Both ARM installation logs identify a native Go arm64 toolchain and the
`endlessnet-client_0.5.0_arm64.deb` package. Fresh IPC, disconnected-intent
preservation across service restart and uninstall pass. The package uses the
existing release version; this run does not establish an enrolled-client upgrade.
Native TCP/UDP and ICMP echo in both overlay families now have evidence on all
eight runners, with the same IPv4-underlay and policy/lifecycle limits above.
This remains partial HC coverage, not completion of the entire catalog.

## Enrolled same-artifact reinstall increment

The installed-client suite now includes `enrolled-reinstall` on every native
runner. It stops the installed service for a public CLI enrollment bootstrap
against the contract testserver, supplying only the fixture's public signing
trust and a token through stdin. No test reads or edits persisted Client state.
The service then runs under systemd, launchd or Windows SCM with its normal
installed configuration path. A reference WireGuard TCP peer validates actual
traffic with fresh nonce echoes; its address is never assigned to the host.

Repeating the existing package/service installer while connected must preserve
the public node identity, overlay address, credential/cache indicators and
restore TCP traffic without creating another enrollment. Repeating installation
while user-disconnected must preserve that intent and identity, deny traffic
and send no registration/refresh request. Explicit connect must subsequently
restore access through the same reference peer/key binding. Credential refresh
while connected is distinct from creating a new enrollment.

The installation workflow builds packetprobe natively for all eight runners.
The standalone contract suite reuses the extracted underlay-selection helper;
its assertions are unchanged. This is HC-003 same-artifact repetition, not a
version-upgrade, interrupted-installation or complete-state-removal result.
[Source 8866f54](https://github.com/endless-net/client/tree/8866f54df64ad6138d036740242c667a56128404)
passed local format/vet/lint/short checks. In
[CI 34631959688](https://github.com/endless-net/client/actions/runs/34631959688),
all four Linux and both Windows installation jobs passed. Both macOS installation
jobs failed waiting for public service state before the first tested reinstall;
the original timeout did not distinguish bootstrap from initial connect. This
increment is not qualified on macOS. Phase-specific IPC diagnostics now report
only public enums and presence/error flags, without dumping identities, keys,
credentials or private state. Diagnose that failure before claiming HC-003
coverage across the matrix; the common contract jobs are separate evidence.

The diagnostic source 7fb421a in
[CI 34633043288](https://github.com/endless-net/client/actions/runs/34633043288)
localized macOS ARM failure to bootstrap: enrollment, credentials, trust and
cached map were present, but public status reported a local-state error and
invalid cached map. Source inspection found macOS installation identity lookup
falling back to `os.UserConfigDir`, unlike the machine-level Linux/Windows paths.
The macOS root CLI and launchd daemon now resolve the same machine installation
directory independently of HOME. Non-root clients retain user-scoped identity.
The enrolled installer scenario is the native regression check.
[Source 5f8aadd](https://github.com/endless-net/client/tree/5f8aadd629be26cf9367bc0b3bcaa057423c80ce)
in [CI 34633422853](https://github.com/endless-net/client/actions/runs/34633422853)
passed all eight installation jobs, including macOS ARM and Intel. Their
`enrolled-reinstall` cases passed in 48.65s and 48.57s respectively; both tested
connected and disconnected reinstall, identity preservation, renewed TCP traffic
and subsequent uninstall. The common contract suite is still separate evidence. No private state was read, no
fingerprint validation was bypassed and no version was increased.

The completed common matrix for source 5f8aadd contains the same 17 scenarios
three times on each of eight platforms: **407 PASS, 1 FAIL, 0 SKIP**. The only
failure was the first `TestControlPlaneDNSWireRecovery` repetition on Windows
2022: its shared Client startup helper obtained no successful public IPC status
within 15 seconds, before any DNS-wire assertions. The other two repetitions
passed. Windows 2025 and both macOS variants each passed all 51 outcomes, as did
all four Linux variants. The required aggregate correctly failed; this source
has no fully successful qualification run. All eight installation jobs passed
independently. This is not evidence that DNS itself malfunctioned or that the
startup failure is fixed.

The harness now records fixed IPC failure categories, response/failure counts
and whether the agent exited (with exit code), without arbitrary command output.
Each status subprocess is also bounded by the existing overall 15-second wait
context. No deadline was increased, no retry of a failed test was introduced and
no assertion was removed. Re-observe the startup failure in subsequent CI before
attributing or closing its cause.

## Explicit ICMP-only policy increment

The two native TCP scenarios now additionally apply a signed ICMP-only grant
for the exact reference peer destination. Real OS ping must succeed while an
already established TCP stream and fresh TCP connections to both reference ports
are blocked. Changing only the granted destination to the adjacent host address
must deny ping; restoring the original destination must recover ping. Finally,
restoring unrestricted authorization must recover fresh TCP on both ports and
preserve ping. IPv4 and IPv6 use the same checks on all eight native runners.
These are new assertions within the existing 17-scenario inventory, not new
root test names. Local format/vet/lint/short checks pass; hosted evidence for
these new assertions is pending. This does not prove UDP denial under ICMP-only
policy, ICMP errors/PMTU, IPv6 underlay, Relay/NAT or all policy directions.

## UDP isolation under ICMP-only policy

The protocol-stack reference peer now answers real UDP nonce echoes on the same
two destination ports as TCP. Before applying an ICMP-only grant, native Client
UDP probes on both ports and a persistent UDP session must succeed. While ICMP
remains allowed, the established UDP session and new UDP exchanges on both ports
must be blocked; restoring unrestricted authorization must restore both fresh
UDP exchanges. These checks run for both overlay address families in the native
TCP scenarios, independently of the existing UDP-only reference scenarios.
Only public signed maps and externally observed payload delivery are used.
Local format/vet/lint/short checks pass; hosted qualification remains pending.

The Client Test workflow now finishes its active matrix when a newer commit is
pushed (`cancel-in-progress: false`). This preserves completed-source evidence
for analysis instead of interrupting the slowest platform. GitHub concurrency
still serializes runs in the same branch group and may replace an older pending
run with a newer pending run; it does not guarantee execution of every queued
commit. Jobs within each matrix remain parallel with fail-fast disabled. This
changes Client-owned workflow scheduling only, not managed runner infrastructure.

## Single-agent ownership increment

`TestControlPlaneSingleAgentOwnership` starts a real enrolled Client, then starts
another real agent for the same configuration with a different IPC endpoint.
The second process must exit with code 1 and the public configuration-ownership
error, excluding IPC address collision as a false positive. The original process
must continue answering IPC with its identity and intent intact, and while
connected must consume a newer signed map. The test repeats rejection while
user-disconnected, then stops and starts the owner after each case to prove that
ownership is released and enrollment/intent survive. The contract testserver
must observe exactly one registration. Tests never read lock files, persisted
identity or private runtime state. This does not yet cover path aliases,
simultaneous first launch or loss of power during writes.

The compiled common inventory increases to 18 root scenarios, with the existing
CI comparator requiring three successful repetitions of every scenario on all
eight platforms (432 outcomes). Local format/vet/lint/short checks passed;
platform evidence for this new scenario remains pending.

## ICMP destination fixture correction

In [CI 34634842532](https://github.com/endless-net/client/actions/runs/34634842532),
all four Linux runners rejected the new wrong-destination fixture in both native
TCP scenarios on all three repetitions: the published contract validator reports
`acl_grants[0] destination is outside peer allowed_ips`. This occurs while the
testserver prepares the signed map, not in Client packet enforcement. The earlier
ICMP-only positive and TCP-negative assertions ran before this failure, but the
full new lifecycle scenario is not qualified by this run.

The fixture now publishes both exact destination routes as owned by the same
peer throughout the ICMP-only phase, while the grant alternates between the
actual echo address and its adjacent address. This makes both maps valid while
preserving the intended destination-denial/recovery assertion. The actual echo
address, cryptographic peer and reference endpoint remain unchanged. Full
platform qualification of the corrected fixture remains pending.

## Completed ICMP fixture-failure evidence

[CI 34634842532](https://github.com/endless-net/client/actions/runs/34634842532)
for source `3c2f0e4d89617ca7d82efad869f1db5d10f6cac9` completed with **failure**.
The eight native reports contain identical inventories of 17 root scenarios,
three repetitions each: **360 PASS, 48 FAIL, 0 SKIP**. Every platform has 45 PASS
and six failures: three IPv4 and three IPv6 native TCP repetitions rejected the
out-of-route ICMP destination in testserver contract validation. No other root
scenario failed. All eight installed-client jobs and the separate Linux dataplane
job passed; the aggregate `verify` correctly failed.

`TestControlPlaneDNSWireRecovery` passed all three repetitions on Windows 2022
(7.69s, 2.70s, 2.75s) and Windows 2025 (8.69s, 3.50s, 3.73s). The earlier Windows
2022 IPC startup timeout did not recur here, but its cause remains unproven.
The new diagnostics are observability improvements, not proof of a runtime fix.

The corrected 18-scenario source
[`b9b7712`](https://github.com/endless-net/client/tree/b9b7712dddf059beba590329ec11e8a72c970242)
is now running in [CI 34635511042](https://github.com/endless-net/client/actions/runs/34635511042).
It includes valid ICMP destination routes, UDP exclusion/recovery and single-agent
ownership. These increments remain unqualified until that matrix completes.
Intermediate pending runs for `1e0a535` and `4a84a93` were superseded by newer
pending source; no successful execution is claimed for those source commits.

## Explicit local cleanup after unconfirmed logout

`TestControlPlaneLocalForgetAfterUnconfirmedLogout` drives the real CLI and IPC.
It first rejects `service local-forget` without explicit confirmation and checks
that enrollment remains. While the contract server is unavailable, `service
logout` must report that remote cleanup was not confirmed and retain the node
credential and cached map. Explicit local-forget must then return the public
`remote_cleanup_unconfirmed` outcome, clear node/network/credential/cache/session
state visible through IPC, retain signing trust and record disconnected intent.
After server recovery and an agent restart the cleared public state must persist;
no registration request may appear during the one-second observation interval,
and the testserver must never have confirmed deletion or logout. This bounded
observation is not a claim about arbitrary offline duration or all traffic paths.
The scenario does not claim removal of installation identity/private keys, a
full reset, profile support or proof of remote revocation.

The shared harness now exposes bounded `ServiceCommand` results for expected
CLI errors using the same 35-second outer bound as successful service operations.
Arbitrary output is never printed. Local format/vet/lint/short checks pass.
The common inventory grows to 19 scenarios, with three required repetitions
on eight platforms (456 outcomes); hosted qualification remains pending.

## macOS route-only update regression

The corrected source `b9b7712` in
[CI 34635511042](https://github.com/endless-net/client/actions/runs/34635511042)
passed all 54 root outcomes on each of four Linux runners (216 PASS). Both macOS
runners passed 48/54; their six failures were native IPv4/IPv6 TCP scenarios after
valid route changes, either missing ICMP echo under an ICMP-only grant or failing
to restore TCP after restoring unrestricted authorization. These failures are
distinct from the preceding contract-invalid fixture. Single-agent ownership
passed three times on all six completed platforms; Windows evidence is pending.

Source inspection found Darwin router reconfiguration unconditionally lowering
utun even for route-only changes. The correction reconciles only added/removed
routes when other router settings are unchanged, preserving the live interface
and addresses. Each successful mutation updates the router's tracked routes so
engine rollback can restore the prior map after a later command failure. A
Darwin-specific regression exercises successful add/remove and failure on the
second add, requiring rollback without any interface command. Other router
changes continue through the full configuration path.

Local format/vet/lint/short checks on Windows pass; the Darwin-specific test and
unchanged native packet assertions require hosted macOS qualification. The
interface-event race is the working explanation for the traffic failures, not
an independently captured OS event trace. No packet assertion was relaxed and
no recovery deadline was increased. No Infrastructure or version changes occur.

## Windows route-only update regression and completed 18-scenario evidence

The completed [CI 34635511042](https://github.com/endless-net/client/actions/runs/34635511042)
for source `b9b7712dddf059beba590329ec11e8a72c970242` failed: **408 PASS, 24 FAIL,
0 SKIP**, with the same 18 root scenarios repeated three times on every platform.
All four Linux platforms passed 54/54. Both macOS and both Windows platforms
passed 48/54; all failures were native IPv4/IPv6 TCP scenarios at the new ICMP-only
or subsequent recovery phase. Windows consistently failed the first permitted
ICMP echo after adding the second route. Single-agent ownership passed 3/3 on
all eight platforms (24 independent outcomes). Installation passed on all eight;
these scenario-level successes do not make the complete source CI successful.

Windows source inspection found route reconfiguration removing and recreating
all manual interface IP addresses, routes and DNS rules even when only routes
changed. The router now records its last successful configuration and applies
only route additions/removals when all other settings are unchanged. Retained
routes, addresses and DNS are untouched. On a failed route script the existing
cleanup path invalidates configured state so subsequent engine rollback performs
a full restoration instead of treating the previous desired state as applied.
Regression tests reject address/DNS changes during route updates, verify IPv4
and IPv6 additions/removals, and verify full restoration after a partial failure.

Windows local format/vet/lint/short checks pass, including these regression tests.
The unchanged native ICMP/TCP/UDP tests remain the cross-platform acceptance
criterion. Both this correction and the earlier Darwin route correction require
new hosted evidence before being called confirmed fixes. No scenario or platform
was removed, no timing assertion was relaxed and no version was increased.

## Local-forget status expectation correction

In [CI 34636389331](https://github.com/endless-net/client/actions/runs/34636389331),
Linux ARM and macOS ARM completed the local-forget command but the new test
waited for `StatusResponse.state=NeedsEnrollment`. The observed service status was
`Disconnected` / `disconnected` with revision zero. Source inspection and the
[local cleanup retention matrix](client-ownership-recovery.md#logout-and-local-forget)
show that explicit local cleanup records disconnected intent with reason
`local_logout`; the status projection gives that intent precedence. The command
response separately reports `NeedsEnrollment` and `remote_cleanup_unconfirmed`.

The test now requires the exact disconnected status/control state, the public
`local_logout` intent reason and zero revision alongside all original node,
credential, session, map and peer cleanup checks. The command response assertion
is unchanged. This corrects an expectation error, not a Client runtime defect.
Restart persistence and the bounded no-reenrollment observation still execute
only after these strict cleanup conditions hold. Hosted qualification is pending.

## Completed 19-scenario pre-correction evidence

[CI 34636389331](https://github.com/endless-net/client/actions/runs/34636389331)
for `55ecd930f6f80ec78f4959fc2143938976a19f05` completed with **409 PASS, 47 FAIL,
0 SKIP** across identical 19-scenario inventories repeated three times on eight
platforms. Each Linux runner passed 54/57, failing only the three local-forget
status expectations. macOS Intel passed 49/57; macOS ARM and both Windows runners
passed 48/57. All 24 local-forget failures were at the incorrect NeedsEnrollment
status expectation, and the remaining 23 failures were ICMP-only/recovery traffic
checks affected by route reconfiguration. No other root scenario failed. All
eight installation jobs passed; the required aggregate correctly failed.

[CI 34637951595](https://github.com/endless-net/client/actions/runs/34637951595)
is running source `7283d13d6f273a39a055bbdbc2db7316eee3e49f`, which includes both
platform route corrections and the corrected local-forget status expectation.
Its outcome is not inferred from the preceding run. Earlier queued intermediate
commits were superseded before execution; the currently running matrix is kept.

## Confirmed 19-scenario eight-platform qualification

[Source 7283d13](https://github.com/endless-net/client/tree/7283d13d6f273a39a055bbdbc2db7316eee3e49f)
completed [CI 34637951595](https://github.com/endless-net/client/actions/runs/34637951595)
with **success** on 2026-09-11. All required jobs passed: eight installed-client
jobs, eight native contract jobs, three platform verification jobs, the separate
Linux dataplane suite and aggregate `verify`. The optional external STUN job was
skipped and is not included in this qualification claim.

| Native platform | Common root outcomes | Job evidence |
| --- | --- | --- |
| Ubuntu 22.04 x64 | 57 PASS | [103390748039](https://github.com/endless-net/client/actions/runs/34637951595/job/103390748039) |
| Ubuntu 24.04 x64 | 57 PASS | [103390748040](https://github.com/endless-net/client/actions/runs/34637951595/job/103390748040) |
| Ubuntu 22.04 ARM64 | 57 PASS | [103390748261](https://github.com/endless-net/client/actions/runs/34637951595/job/103390748261) |
| Ubuntu 24.04 ARM64 | 57 PASS | [103390748312](https://github.com/endless-net/client/actions/runs/34637951595/job/103390748312) |
| macOS 15 Intel | 57 PASS | [103390748153](https://github.com/endless-net/client/actions/runs/34637951595/job/103390748153) |
| macOS 15 ARM64 | 57 PASS | [103390748411](https://github.com/endless-net/client/actions/runs/34637951595/job/103390748411) |
| Windows 2022 x64 | 57 PASS | [103390748109](https://github.com/endless-net/client/actions/runs/34637951595/job/103390748109) |
| Windows 2025 x64 | 57 PASS | [103390748539](https://github.com/endless-net/client/actions/runs/34637951595/job/103390748539) |

The [aggregate verifier](https://github.com/endless-net/client/actions/runs/34637951595/job/103395121526)
confirmed identical compiled inventories and source SHA, three successful runs
of each of 19 common scenarios on all eight platforms: **456 PASS, 0 FAIL,
0 SKIP**. This includes the new ICMP-only grant and exact-destination checks,
established/fresh TCP and UDP exclusion and recovery, single-agent ownership,
and explicit local cleanup followed by restart without automatic reenrollment.
The successful native traffic checks qualify the Windows/Darwin route-only
corrections for these tested transitions. No timing bound or packet assertion
was relaxed to obtain this result.

The installed-client jobs also reconfirm fresh installation, disconnect intent
across service restart, enrolled same-artifact reinstall in connected/disconnected
states with identity preservation and real TCP recovery, and uninstall. These
are not version-upgrade, interrupted-install or full-state-removal claims.

This qualifies the cited source and tested cases, not every HC-001-HC-065 variant
or a production release. IPv6 overlay still uses IPv4 underlay. Relay/NAT,
system resolver integration, ICMP errors/PMTU, full policy-direction variants
and other gaps in the matrix remain open. The isolated earlier Windows IPC
startup timeout did not recur but its cause remains unproven. Later documentation
commits require their own source CI for publication under the existing gate.

## Routed resource increment awaiting hosted qualification

`TestControlPlaneRoutedResource` adds a real native Client consuming a signed
IPv4/IPv6 resource prefix through a WireGuard reference routing peer. The
resource has its own network stack behind an explicit IP forwarding hop that
decrements TTL/Hop Limit and validates IPv4 checksums. Its address is never
assigned to the runner OS. TCP/UDP nonce exchanges and forwarding counters
prove delivery through that hop. The scenario checks absent-route denial,
route installation, established/fresh flow denial after withdrawal and recovery
after restoration. The common inventory becomes 20 roots repeated three times
on eight platforms (480 required outcomes); native qualification is pending.

This is Client consumer-route evidence, not production router implementation,
SNAT, HA, overlapping-route selection, IPv6 underlay or ICMP error/PMTU coverage.

The later documentation-source [CI 34638279169](https://github.com/endless-net/client/actions/runs/34638279169)
has a native IPv6 UDP failure on
[macOS Intel](https://github.com/endless-net/client/actions/runs/34638279169/job/103395234127):
the probe exited 1 before its result could be classified as reachable/unreachable.
This does not invalidate the cited successful run, but repeatability is not yet
established. Its cause is unproven. Probe diagnostics now report only allowlisted
fixed error messages, preserving failure semantics and keeping arbitrary process
output out of logs. No network assertion, retry count or timeout was relaxed.

## Trust confirmation rejection awaiting hosted qualification

`TestControlPlaneTrustConfirmation` invokes the real CLI to inspect announced
and trusted server identities and reject missing consent, a foreign control
origin and a wrong key ID. Each rejection must preserve trust and enrollment
and allow a later signed map revision. Agent restart must retain the same
trusted key and registered node, with only one registration observed through
the testserver contract. No persisted identity or configuration is read by the
test. This covers HC-021/HC-059 rejection behavior; successful key rotation,
durable interrupted recovery and independent signing scopes remain open.

The common native inventory now contains 21 roots, requiring three repetitions
on eight platforms (504 outcomes). Hosted evidence for the routed-resource and
trust-confirmation increments remains pending; the earlier 456-PASS qualification
continues to describe only its cited source.

HC-063 remains a product/interface gap for a full identity reset: the currently
exposed local-forget operation deliberately retains device and WireGuard keys,
installation fingerprint and local owner, as specified in
[the recovery retention matrix](client-ownership-recovery.md#logout-and-local-forget).
Its passing tests must not be reported as new-identity reset coverage.

## Repeat qualification of the 19-scenario source

[CI 34639597707](https://github.com/endless-net/client/actions/runs/34639597707)
succeeded for [source 3387f4c](https://github.com/endless-net/client/tree/3387f4c93552b2efa2f2202151c3624a216804f9).
All eight native jobs completed with 57 PASS each, without failures or skips.

| Native platform | Job evidence |
| --- | --- |
| Ubuntu 22.04 x64 | [103399230717](https://github.com/endless-net/client/actions/runs/34639597707/job/103399230717) |
| Ubuntu 24.04 x64 | [103399230775](https://github.com/endless-net/client/actions/runs/34639597707/job/103399230775) |
| Ubuntu 22.04 ARM64 | [103399230854](https://github.com/endless-net/client/actions/runs/34639597707/job/103399230854) |
| Ubuntu 24.04 ARM64 | [103399230784](https://github.com/endless-net/client/actions/runs/34639597707/job/103399230784) |
| macOS 15 Intel | [103399230821](https://github.com/endless-net/client/actions/runs/34639597707/job/103399230821) |
| macOS 15 ARM64 | [103399230903](https://github.com/endless-net/client/actions/runs/34639597707/job/103399230903) |
| Windows 2022 x64 | [103399230818](https://github.com/endless-net/client/actions/runs/34639597707/job/103399230818) |
| Windows 2025 x64 | [103399230764](https://github.com/endless-net/client/actions/runs/34639597707/job/103399230764) |

The [aggregate verifier](https://github.com/endless-net/client/actions/runs/34639597707/job/103403700599)
confirmed identical source/inventory and **19 scenarios x 3 repetitions x 8
platforms = 456 PASS, 0 FAIL, 0 SKIP**. All eight installation jobs, three OS
verification jobs and the separate Linux dataplane job also succeeded. Optional
external STUN compatibility was skipped and remains outside this claim.

For comparison, completed CI 34638279169 produced **455 PASS, 1 FAIL, 0 SKIP**;
its sole failure was one macOS Intel native IPv6 UDP repetition. The same three
repetitions passed in this later run (22.57s, 22.40s, 24.80s). This is evidence
of non-recurrence, not proof of a cause or a fix. No runtime change separated
these documentation-only source revisions. Keep the earlier failure visible.

The new routed-resource and trust-confirmation scenarios are absent from this
19-root source. Their [21-root source CI](https://github.com/endless-net/client/actions/runs/34641056248)
is running separately for `859e1021673c50876404cf2de3baed594d21b21a`; qualification
of its 504 expected outcomes remains pending. Full HC-001-HC-065 coverage,
production acceptance and all stated scope gaps remain unproven.

## Native route preference increment awaiting hosted qualification

`TestControlPlaneRoutedResource` now also exercises HC-019/HC-033 through the
real `sync --offline --route-table off|auto` CLI while the agent is stopped.
With the resource prefix still present in the signed map, `off` must deny
fresh IPv4/IPv6 TCP/UDP access and remain effective after another agent restart.
Restoring `auto` must restore nonce exchanges through the forwarding peer.
Public IPC must report the expected route table, same node and applied map;
testserver observations must show exactly one registration. The existing
server route-withdrawal checks, including established flows, remain intact.

This is durable local route-installation preference coverage, not live mutation
of a running agent, per-resource selection, numerical route tables, egress
selection or router advertisement. The common inventory remains 21 roots and
504 expected outcomes across eight platforms; this extension awaits its own
hosted source evidence. Local short tests, vet and lint passed, but do not
execute native routing scenarios.

## Confirmed 21-scenario eight-platform qualification

[CI 34641056248](https://github.com/endless-net/client/actions/runs/34641056248)
succeeded for [source 859e102](https://github.com/endless-net/client/tree/859e1021673c50876404cf2de3baed594d21b21a).
The native job reports contain 63 successful root outcomes per platform:

| Platform | Native job evidence | RoutedResource repetitions (seconds) | TrustConfirmation repetitions (seconds) |
| --- | --- | --- | --- |
| Ubuntu 22.04 x64 | [103403847639](https://github.com/endless-net/client/actions/runs/34641056248/job/103403847639) | 8.13, 8.16, 8.14 | 0.46, 0.46, 0.46 |
| Ubuntu 24.04 x64 | [103403848463](https://github.com/endless-net/client/actions/runs/34641056248/job/103403848463) | 8.05, 8.05, 8.07 | 0.43, 0.43, 0.43 |
| Ubuntu 22.04 ARM64 | [103403847649](https://github.com/endless-net/client/actions/runs/34641056248/job/103403847649) | 8.02, 7.98, 8.00 | 0.43, 0.44, 0.44 |
| Ubuntu 24.04 ARM64 | [103403847770](https://github.com/endless-net/client/actions/runs/34641056248/job/103403847770) | 8.00, 8.00, 7.97 | 0.42, 0.42, 0.42 |
| macOS 15 Intel | [103403847842](https://github.com/endless-net/client/actions/runs/34641056248/job/103403847842) | 7.39, 7.87, 7.79 | 0.72, 0.69, 0.69 |
| macOS 15 ARM64 | [103403847729](https://github.com/endless-net/client/actions/runs/34641056248/job/103403847729) | 7.21, 7.06, 7.11 | 0.52, 0.44, 0.47 |
| Windows 2022 x64 | [103403847620](https://github.com/endless-net/client/actions/runs/34641056248/job/103403847620) | 18.41, 17.21, 17.35 | 3.43, 3.38, 3.54 |
| Windows 2025 x64 | [103403847769](https://github.com/endless-net/client/actions/runs/34641056248/job/103403847769) | 17.15, 23.13, 18.93 | 4.04, 3.75, 2.53 |

The [aggregate verifier](https://github.com/endless-net/client/actions/runs/34641056248/job/103408442552)
confirmed identical compiled inventories and source SHA: **21 common scenarios
x 3 repetitions x 8 platforms = 504 PASS, 0 FAIL, 0 SKIP**. All eight installed
client jobs, three OS verification jobs and the separate Linux dataplane job
also passed. Optional external STUN compatibility was skipped and is excluded.

The new evidence proves consumer access to a resource behind a separate IP
forwarding hop, absent/withdrawn-route denial including established flows, and
TCP/UDP recovery over IPv4 and IPv6 overlays. It also proves CLI rejection of
missing consent, wrong origin and wrong key ID without changing pinned trust
or enrollment, with later signed-map acceptance and durable state after restart.

It does not prove Client acting as a production router, SNAT, route HA, IPv6
underlay, successful signing-key rotation or full HC-001-HC-065 coverage. The
earlier isolated macOS Intel IPv6 UDP failure again did not recur; its cause
remains unproven. The later local route-table off/auto extension is absent from
this source and is being checked separately by
[CI 34642493566](https://github.com/endless-net/client/actions/runs/34642493566).
Its result must not be inferred from this successful qualification.

## Relay contract participant foundation

`internal/testrelay` provides a Client-owned, fixed-peer TLS participant using
only the pinned published `relay/protocol/v1` messages. It accepts the credential
issued for the test Client, checks frame peer/network scope, forwards opaque
datagrams to one reference WireGuard UDP endpoint and wraps responses in public
server frames. It exposes only public certificate material and aggregate counts;
private keys remain in memory. Explicit unavailability closes existing sessions
and denies new ones while allowing recovery at the same listener.

Its short component test verifies TLS trust, denied authentication, foreign
scope rejection, bidirectional opaque payload preservation and reconnection.
These are fixture checks, not production Relay tests or evidence of native
Client Relay access. HC-028 still requires a real Client consuming a signed map
with Relay credentials, traffic through its native interface, forced absence of
a direct path, observable Relay selection and outage/recovery assertions. The
common native inventory is unchanged by this foundation.

## Native forced Relay increment awaiting qualification

`TestControlPlaneNativeRelayTraffic` runs a real native Client with a signed
peer map that publishes no direct peer endpoint. The published Relay endpoint,
credential and explicitly supplied public TLS CA connect the Client to the
fixed-peer contract participant. Public IPC must report the selected Relay and
the peer's `relay` path. Fresh TCP/UDP nonce exchanges over IPv4 and IPv6 overlays
must also increment both directions of the participant's frame counters.

Closing all Relay sessions and rejecting new authentication must deny both
established and fresh TCP/UDP exchanges while retaining Client enrollment.
Restoring the same Relay listener must restore fresh application access; agent
restart must again select Relay and restore traffic with the same node and one
registration. No Client runtime internals or production Relay server are imported
by the scenario. The WireGuard reference peer has a separate application stack
and its address is never installed on the runner OS.

The common inventory becomes 22 root scenarios, each repeated three times on
eight platforms (528 required outcomes). Local short tests, vet and lint passed;
native qualification remains pending. This is not evidence of NAT traversal,
direct-path upgrades, multi-Relay failover, IPv6 underlay, production Relay
authorization, or recovery of every established application session. HC-028 and
HC-030 retain those unverified variants.

## Confirmed durable route preference qualification

[CI 34642493566](https://github.com/endless-net/client/actions/runs/34642493566)
succeeded for [source 9805f0e](https://github.com/endless-net/client/tree/9805f0ed4520b3a7f12d939f7807f87c89fdb45e).
Each native job produced 63 PASS outcomes, including the expanded IPv4/IPv6
`TestControlPlaneRoutedResource` with public offline `off`/`auto` preference
changes, persistence across agent restart and TCP/UDP denial/restoration.

| Platform | Native job evidence | RoutedResource repetitions (seconds) |
| --- | --- | --- |
| Ubuntu 22.04 x64 | [103408597962](https://github.com/endless-net/client/actions/runs/34642493566/job/103408597962) | 23.20, 23.28, 23.11 |
| Ubuntu 24.04 x64 | [103408597975](https://github.com/endless-net/client/actions/runs/34642493566/job/103408597975) | 23.15, 23.12, 23.00 |
| Ubuntu 22.04 ARM64 | [103408598040](https://github.com/endless-net/client/actions/runs/34642493566/job/103408598040) | 22.75, 22.89, 22.85 |
| Ubuntu 24.04 ARM64 | [103408598010](https://github.com/endless-net/client/actions/runs/34642493566/job/103408598010) | 22.75, 23.02, 22.84 |
| macOS 15 Intel | [103408598074](https://github.com/endless-net/client/actions/runs/34642493566/job/103408598074) | 23.78, 23.85, 23.02 |
| macOS 15 ARM64 | [103408598007](https://github.com/endless-net/client/actions/runs/34642493566/job/103408598007) | 22.34, 22.34, 22.27 |
| Windows 2022 x64 | [103408597919](https://github.com/endless-net/client/actions/runs/34642493566/job/103408597919) | 42.87, 42.24, 42.35 |
| Windows 2025 x64 | [103408598110](https://github.com/endless-net/client/actions/runs/34642493566/job/103408598110) | 43.82, 43.61, 43.76 |

The [aggregate verifier](https://github.com/endless-net/client/actions/runs/34642493566/job/103413261540)
confirmed **21 scenarios x 3 repetitions x 8 platforms = 504 PASS, 0 FAIL,
0 SKIP** with identical compiled inventory and source SHA. Eight installation
jobs, three OS verification jobs and separate Linux dataplane checks also passed.
Optional external STUN compatibility was skipped and remains outside the claim.

This qualifies durable local route installation control, not live preference
mutation, per-resource selection, numerical tables, egress selection or router
advertisement. The source does not contain the new native Relay scenario;
[CI 34644300216](https://github.com/endless-net/client/actions/runs/34644300216)
checks that increment separately. Windows 2025 root durations sum to 868.68s,
close to the current 900s suite limit. Further coverage growth should distribute
repetitions across isolated runner jobs while preserving all three required
outcomes, inventories and exact source verification.

## Isolated CI repetitions awaiting hosted qualification

The native matrix now runs one complete `TestControlPlane*` suite on each of
three separate GitHub-hosted runners per platform, instead of three sequential
iterations in one runner process. This addresses the observed 868.68s Windows
suite duration as functional coverage grows. Existing per-operation assertions,
the 15-minute test-process deadline and 20-minute job deadline are retained.
Historical evidence with three same-process iterations keeps its original scope;
new reports demonstrate three fresh-runner executions, not same-process reuse
across repetitions. Hosted concurrency and queue capacity still govern actual
start times.

All 24 platform/repetition jobs are mandatory. The aggregate verifier requires
every named artifact, matching `shard.txt`, the exact source SHA, the same compiled
test inventory across all reports, and exactly one complete successful execution
of every root in each report. It rejects missing repetitions, renamed reports
with another repetition identity, extra executions, failed/skipped children and
incomplete package completion. Three executions in one report cannot substitute
for three isolated reports. The exact-source publication gate now requires all
24 native job names in addition to its existing installation and verification
requirements (37 required jobs total).

The common 22-root inventory still requires **528 PASS outcomes**. Short tests,
vet and lint passed, including report rejection and publication-gate regressions.
Hosted validation of this CI change and the new Relay scenario remains pending.
No service/provider or infrastructure execution changes are included.

## Concurrent fixture validation

The existing mandatory Linux control-plane CI job now runs the race detector
against `internal/testcontrol`, `internal/testrelay` and `internal/testwireguard`.
This checks concurrent fixture behavior (including TLS session shutdown and
packet forwarding) in addition to short component tests. It is fixture evidence,
not another native Client or production service coverage claim. Native tests and
the 24-report publication requirement remain unchanged. Hosted race validation
of the expanded participant set is pending.

## Initial native Relay failure and diagnostic follow-up

In [CI 34644300216](https://github.com/endless-net/client/actions/runs/34644300216),
both Linux ARM jobs failed all three native Relay repetitions in both IPv4 and
IPv6: [Ubuntu 22.04 ARM](https://github.com/endless-net/client/actions/runs/34644300216/job/103413387229)
and [Ubuntu 24.04 ARM](https://github.com/endless-net/client/actions/runs/34644300216/job/103413387112).
The completed Linux x64 jobs also report failure. The observed stage is initial
application exchange after public IPC already selected Relay; outage/recovery
assertions have not yet been reached. This does not establish whether the fault
is in Client packet handling or the new contract participant.

The diagnostic follow-up probes TCP and UDP independently and reports only
booleans and counters: Relay authentication and frame directions, reference
WireGuard initiations/responses, reference application requests/echoes, and
public Client handshake, loopback endpoint and byte counters. Arbitrary process
output, keys and credentials remain withheld. The 15-second exchange bound and
required payload checks are unchanged. HC-028 remains unqualified pending
localization, correction and native reruns.

## Completed initial Relay run: failure, not qualification

[CI 34644300216](https://github.com/endless-net/client/actions/runs/34644300216)
completed unsuccessfully for source `b3548fd35cfe985806c271d084ac48310f5bd96a`.
Its root reports contain **479 PASS, 24 FAIL, 0 SKIP**, with **25 expected root
executions incomplete or absent** from the 528-outcome requirement. All 24
completed root failures are `TestControlPlaneNativeRelayTraffic` (three per
platform). Each of the four Linux and two macOS jobs completed the other 63
outcomes successfully. This supports preserving the previous qualification,
not accepting the new Relay feature.

[Windows 2022](https://github.com/endless-net/client/actions/runs/34644300216/job/103413387483)
recorded 52 PASS and 3 FAIL, then hit the overall 15-minute test-process limit
while `TestControlPlaneMalformedErrorsPreserveEnrollment` had run for 4 seconds.
[Windows 2025](https://github.com/endless-net/client/actions/runs/34644300216/job/103413387148)
recorded 49 PASS and 3 FAIL, then reached the same overall limit during
`TestControlPlaneRoutedResource` (40 seconds into that scenario). These incomplete
runs are not additional successful coverage or evidence that the active scenario
itself exceeded its own operation deadline.

The already-implemented separate-runner repetition matrix addresses suite
capacity while retaining all expected outcomes. Its first diagnostic source
[CI 34646086686](https://github.com/endless-net/client/actions/runs/34646086686)
has instantiated 24 native jobs for `0981f09add71d1e6f6209b8b4d2be17bdc586aea`.
Relay packet loss still requires localization and correction; CI repartitioning
alone cannot fix or qualify it.

## Relay return-path correction and concurrent ownership increment

The diagnostic source `0981f09add71d1e6f6209b8b4d2be17bdc586aea` reached the
reference WireGuard peer: the Ubuntu 24.04 first repetition recorded two Relay
authentications, two forwarded frames and two handshake initiations, but no
handshake response or application request. This localizes the initial failure
before application traffic; it is not evidence of a Client runtime defect.

Source `798755b84cce93c86ee33bf0b876152b3fcafcca` configures the reference peer's
return endpoint for each authenticated Relay connection, before readiness and
forwarding. The pinned reference engine requires an explicit endpoint, as in
the existing direct-traffic fixture. The fixture recovery test now withholds
UDP replies unless the configured return endpoint matches the sending socket.
Local vet, lint and short tests pass; native Relay qualification still requires
the complete hosted matrix.

`TestControlPlaneSingleAgentOwnership` additionally launches three contenders
concurrently against an already-running real agent, using the original path,
a directory-dot path and a directory-parent path to the same configuration.
Each contender has a separate IPC endpoint and must report the ownership error.
The original agent must retain identity and connected/disconnected intent,
consume a signed map while connected, and restart without another registration.
This increment passed all 24 native repetitions in CI 34647302646, documented
below. It does not qualify symbolic links,
hard links, simultaneous startup without an existing owner, or all crash cases.

## Completed first matrix with separate repetitions

[Diagnostic CI 34646086686](https://github.com/endless-net/client/actions/runs/34646086686)
finished with **503 PASS, 25 FAIL, 0 SKIP** across all 528 expected root
executions for source `0981f09add71d1e6f6209b8b4d2be17bdc586aea`.
All 24 shards completed their 22 root scenarios; splitting repetitions onto
separate runners removed the incomplete Windows outcomes seen in the initial
run. It did not qualify the suite: all 24 Relay roots failed before the
return-path correction.

The additional failure was
[Windows 2025 repetition 3](https://github.com/endless-net/client/actions/runs/34646086686/job/103418092163):
`TestControlPlaneDNSWireRecovery` received zero IPC responses and 16 failures
during its initial 15-second agent-start wait, while the process remained alive.
No DNS wire assertion had run. The last IPC error was unclassified, so its
cause is not established. Preserve this failure separately from the Relay
fixture defect; a later pass alone does not explain it.

The harness now preserves counts of every fixed IPC error category and labels
expiration of its own context separately, without printing arbitrary CLI
output. This diagnostic change retains the original deadlines and assertions.
[CI 34647302646](https://github.com/endless-net/client/actions/runs/34647302646)
has instantiated the next matrix for `01ac1e486c5496bbae55b55b1fc12d069d02ff56`,
including the Relay return-path correction and concurrent ownership checks.
Its results are pending; it predates the IPC category-count diagnostic.

## Network-scoped selection boundary

`TestControlPlaneNetworkSelectionBoundary` drives the real `service networks`
and `service select-network` CLI operations. A second network exists at the
contract testserver, but the enrolled Client must list only its own network.
Selection of that network by ID or case-insensitive name must preserve its node
identity. Requests for a foreign network ID/name or an absent network must
require a new network-scoped enrollment, both while connected and disconnected.
Rejection and agent restart must retain the original selected network, identity
and desired connection state, without an extra registration.

The common native inventory now contains 23 roots: 23 x 3 x 8 = 552 required
outcomes for this source. CI discovers the compiled inventory automatically.
This scenario does not establish multiple-profile support, authorized migration
to a new network, traffic isolation after such migration, or same-network
selection semantics while disconnected. HC-020 remains incomplete.

## Completed return-path correction run: partial evidence

[CI 34647302646](https://github.com/endless-net/client/actions/runs/34647302646)
completed with failure for source `01ac1e486c5496bbae55b55b1fc12d069d02ff56`.
All 528 expected roots completed: **503 PASS, 25 FAIL, 0 SKIP**. The expanded
`TestControlPlaneSingleAgentOwnership` passed all 24 repetitions across eight
platforms. This qualifies the tested lexical aliases and concurrent contenders
against an existing owner, not every HC-005 variant or the source as a whole.

All 24 Relay roots failed. The
[Ubuntu 24.04 repetition 3](https://github.com/endless-net/client/actions/runs/34647302646/job/103421527555),
[macOS Intel repetition 1](https://github.com/endless-net/client/actions/runs/34647302646/job/103421527641)
and [macOS ARM repetition 3](https://github.com/endless-net/client/actions/runs/34647302646/job/103421527699)
logs locate both IP-family failures at the public path-status wait after Relay
availability was restored. Initial TCP/UDP exchange, established sessions and
outage denial assertions had passed. Because status was checked before traffic
at recovery, these logs do not establish whether recovered traffic worked.

[Windows 2025 repetition 1](https://github.com/endless-net/client/actions/runs/34647302646/job/103421527552)
instead failed at the initial exchange in both families: 2 authentications,
4 frames to the peer, 2 frames back, 2 reference handshake initiations and
2 responses, but zero application requests/echoes. This must be investigated
separately from the Linux/macOS recovery-status observation.

The additional root failure was
[Windows 2025 repetition 2](https://github.com/endless-net/client/actions/runs/34647302646/job/103421527778),
again during initial IPC readiness in `TestControlPlaneDNSWireRecovery`:
zero responses, 16 failed attempts and a live agent. The DNS wire assertions
had not started. This repeats the startup symptom from CI 34646086686; its
cause remains unresolved.

[Diagnostic CI 34648109886](https://github.com/endless-net/client/actions/runs/34648109886)
has instantiated the matrix for `a6b4edb4724b9a23be8575e29e94109bc9592f7f`.
It includes the 23-root inventory, IPC category counts, and mandatory Relay
traffic checks before mandatory path-status checks. Per-check deadlines are
unchanged; safe status booleans and participant counters distinguish failure
stages. Runtime recovery is not yet qualified.

## Replayed map events: Client runtime correction awaiting native evidence

Source `b30f4538651e4704967a9b7476522b60f2b459e5` corrects handling of the
published `ErrMapStreamEventAlreadyApplied` sentinel. The agent previously
converted the accompanying result into a runtime map even though no effective
map was returned. A regression applying the same signed snapshot twice failed
on the second application before this correction. Passing that empty map into
WireGuard can remove peers and stop the Relay bridge.

A replayed full snapshot is now independently validated from an empty base and
returned with its transient Relay credential. A repeated delta returns the
effective cached projection; when that projection has Relays, the online agent
fetches a full signed projection before configuring the transport. Credentials
remain excluded from the disk cache. Tampered repeated snapshots remain denied.

The added repeated-delta agent test observes exactly one delta request and one
full-snapshot request through an HTTP contract participant, checks the resulting
peer and runtime Relay credential, and checks that the cache excludes that
credential. Short tests, vet and lint passed for the correction. Native
cross-platform recovery and the previously observed Windows startup symptom
still require hosted evidence; this is not a qualification of HC-028/HC-030.

## Completed 23-root diagnostic matrix

[CI 34648109886](https://github.com/endless-net/client/actions/runs/34648109886)
completed for source `a6b4edb4724b9a23be8575e29e94109bc9592f7f` with
**528 PASS, 24 FAIL, 0 SKIP** across all 552 expected root executions.
Every platform/repetition completed 22 passing roots and one failed
`TestControlPlaneNativeRelayTraffic`. The new network-selection boundary and
the expanded ownership scenario passed in every shard. This is evidence of
their stated cases, not full HC-020/HC-005 coverage or source qualification.

The DNS/IPC startup failure observed in the preceding two runs did not recur
in this matrix. Its absence does not establish a cause or a fix.

In [Ubuntu ARM64 repetition 3](https://github.com/endless-net/client/actions/runs/34648109886/job/103424679886),
initial Relay TCP/UDP succeeded; after the outage, both application probes
failed with only one recorded Relay authentication. This establishes failure
of real recovery traffic, independently of snapshot freshness. The replayed
map correction `b30f4538651e4704967a9b7476522b60f2b459e5` was made after this
source and is still awaiting native qualification.

## Initial IPC readiness deadline alignment

[Windows 2025 repetition 2 in CI 34649232328](https://github.com/endless-net/client/actions/runs/34649232328/job/103427695628)
again failed before DNS assertions. The new diagnostic reported 14
`context deadline exceeded` results and one expiration of the harness context,
with no successful IPC response and a live agent. This identifies bounded
status-request timeouts, but does not identify the runtime initialization stage.

The real CLI's default service IPC timeout is 30 seconds and the installed
service startup test also allows 30 seconds. The contract driver now uses that
same bound for initial IPC readiness instead of its generic 15-second
state-transition wait, and logs readiness duration. Subsequent state waits and
native application probes retain their existing bounds. This corrects a test
precondition mismatch; it is not a claim that the underlying startup latency
has been fixed or that a 30-second bound has passed on all runners.

## Native Relay qualification after replay correction

[CI 34649824498](https://github.com/endless-net/client/actions/runs/34649824498)
completed for source `ff8e6a6dabbfc70df7483ba24a86e048adab6222` with
**551 PASS, 1 FAIL, 0 SKIP** across all 552 expected root executions.
`TestControlPlaneNativeRelayTraffic` passed every repetition on all eight
platforms. The table reports the total duration of its IPv4 and IPv6 subtests
per repetition; these are scenario durations, not recovery latency measurements.

| Platform | Relay repetitions (seconds) |
| --- | --- |
| Ubuntu 22.04 x64 | 10.72, 10.76, 10.73 |
| Ubuntu 24.04 x64 | 10.63, 10.65, 11.60 |
| Ubuntu 22.04 ARM64 | 10.51, 10.55, 10.52 |
| Ubuntu 24.04 ARM64 | 10.52, 10.76, 10.59 |
| Windows 2022 x64 | 31.21, 29.62, 28.72 |
| Windows 2025 x64 | 33.88, 33.06, 34.93 |
| macOS 15 Intel | 13.71, 13.57, 13.94 |
| macOS 15 ARM64 | 10.99, 11.37, 12.07 |

This qualifies a real native Client with a signed map containing no direct
peer endpoint, using a single TLS Relay contract participant and a reference
WireGuard peer: fresh TCP/UDP and established-session exchange, denial while
the Relay is unavailable, fresh-traffic recovery, and recovery after agent
restart without a new registration. IPv4 and IPv6 overlays use IPv4 underlay.
It does not qualify multi-Relay HA, NAT transitions, IPv6 underlay, production
Relay authorization, or recovery of every established application session.

The sole failed root was initial IPC readiness in
[Windows 2025 repetition 1](https://github.com/endless-net/client/actions/runs/34649824498/job/103430860489),
before DNS assertions: 14 request deadlines and one harness deadline at 15
seconds, with a live agent. That same job passed Relay. All eight installation
jobs, three OS verification jobs and the separate Linux dataplane job passed;
the required aggregate correctly rejected the source because of the failed
root. Optional external STUN was skipped and is outside these root counts.

[CI 34650528186](https://github.com/endless-net/client/actions/runs/34650528186)
completed with failure for `28aa2939cc944ebb9aad061c3bb3144a28544cd1`, which
aligns initial IPC readiness with the existing 30-second service timeout.
Twenty-three native jobs passed; Ubuntu 22.04 x64 repetition 1 crashed during
reference-peer teardown, as detailed below. This is not full source
qualification. HC-001-HC-065 coverage remains incomplete, with the other
variants and product gaps listed above.

## Two-Relay failover increment

`TestControlPlaneNativeRelayFailover` extends the same native IPv4/IPv6
TCP/UDP contract scenario with two independent TLS endpoints, a preferred
primary and a lower-priority backup. Both accept the same issued Client
credential. No direct peer endpoint is published. After initial primary
traffic succeeds, primary loss must lead to fresh traffic through the backup,
confirmed by its forwarding counters and public selected-path status. Losing
both endpoints must deny established and fresh traffic. Restoring only the
primary must restore fresh traffic and survive agent restart without another
registration.

The shared fixture's short test also checks that a second endpoint accepts the
original credential and preserves the datagram's sender scope while the first
endpoint is unavailable. This tests the Client's contract participants, not
production Relay mesh behavior or authorization implementation.

This increment introduced 24 roots and required 24 x 3 x 8 = 576 outcomes,
qualified by the completed run below. This does not prove
seamless preservation of pre-failure TCP sessions, latency-based selection,
automatic failback while a healthy backup remains available, NAT transitions,
or IPv6 underlay.

## Two-Relay native qualification: 576 PASS

[CI 34651631287](https://github.com/endless-net/client/actions/runs/34651631287)
completed successfully for source `64f9537537cd17478d6e006213303f47b3f0a913`.
All 24 native reports contain the same 24 root scenarios: **576 PASS, 0 FAIL,
0 SKIP**. The mandatory aggregate also verified complete source-bound reports.
All eight installation jobs, three OS verification jobs and the separate
Linux dataplane job passed. Optional external STUN was skipped and is outside
these required root outcomes.

The two-Relay scenario passed in every repetition (elapsed time shown):

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| ubuntu-22.04 | [13.24s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199021) | [14.82s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198885) | [13.26s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198936) |
| ubuntu-24.04 | [13.08s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199040) | [13.03s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198882) | [14.61s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198963) |
| ubuntu-22.04-arm | [12.95s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198856) | [13.01s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198951) | [13.08s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199194) |
| ubuntu-24.04-arm | [12.91s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199078) | [13.02s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198954) | [13.05s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198969) |
| windows-2022 | [33.08s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199090) | [34.61s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199054) | [33.40s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199426) |
| windows-2025 | [34.42s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199153) | [40.21s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199082) | [39.29s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198901) |
| macos-15-intel | [15.58s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199001) | [15.27s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198987) | [14.11s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436198939) |
| macos-15 | [12.73s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199130) | [12.87s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199039) | [15.62s](https://github.com/endless-net/client/actions/runs/34651631287/job/103436199109) |

This qualifies fresh IPv4/IPv6 TCP/UDP traffic moving from an unavailable
primary to the backup, denial when both endpoints are unavailable, restoration
when only the primary returns, and agent restart with the original identity.
Both overlay families use IPv4 underlay. Assertions use actual application
traffic, Relay forwarding counters and public Client selected-path status.
This is Client consumer evidence, not production Relay mesh qualification.
It does not establish seamless existing-session failover, healthy-backup
failback, latency selection, NAT changes or IPv6 underlay.

This source includes the 30-second initial IPC readiness bound; no readiness
failure occurred in these 24 jobs. It predates the reference-stack teardown
fix, corrected network-selection response and interrupted-enrollment scenario.
Their qualification belongs to the subsequent 25-root source, not this green
run. The previously captured peer teardown panic remains valid evidence even
though it did not recur here. HC-001-HC-065 completion remains unproven.

## Reference TCP peer teardown increment

[Ubuntu 22.04 repetition 1](https://github.com/endless-net/client/actions/runs/34650528186/job/103433655084)
terminated with `panic: send on closed channel` in the reference WireGuard
netstack adapter's `WriteNotify`, called by a gVisor TCP reset worker. The
required aggregate rejected the run. The interrupted process cannot supply
complete root outcomes, even though the other 23 native jobs passed.

The test peer now owns a small TUN adapter around the already-pinned gVisor
stack. It reads the link queue directly, prevents new packet injection during
close, closes the synchronized link queue, and destroys the protocol stack,
joining its workers. It does not use the extra notification channel implicated
by the panic. This uses the public lifecycle APIs of
[gVisor's channel endpoint](https://github.com/google/gvisor/blob/cbd86285d259/pkg/tcpip/link/channel/channel.go)
and [stack](https://github.com/google/gvisor/blob/cbd86285d259/pkg/tcpip/stack/stack.go).
Neither dependency versions nor Client runtime behavior change.

`TestReferenceStackCloseWithActiveTCP` establishes IPv4 and IPv6 TCP sessions,
checks payload delivery, then removes the return path and tears down with
unacknowledged data. It requires termination of both packet readers and the
stack, and rejects injection after close. This fixture regression belongs to
the short suite and the existing hosted participant race check; it does not
add an HC root or prove Client behavior by itself. The complete 25-root hosted
qualification below covers native Client traffic with this lifecycle fix.

## Current-network selection preserves connection intent increment

The existing HC-020 boundary scenario now selects the enrolled network by ID,
name and case-insensitive name while connected and disconnected, before and
after agent restart. It compares the response's desired state with public
status and requires disconnected state when the user has disconnected. Status
must preserve identity, network, intent and cached-map validity; the existing
foreign-network rejection and single-registration assertions remain in place.

A short handler regression reproduced an incorrect `Connected/connected`
response after persisted disconnect. Selection had not reconnected the client:
the response was hard-coded. The handler now derives state and desired state
from the same status calculation as the public status endpoint, including
connection intent. Local short tests, vet and lint passed after the fix.
The expanded native assertions passed in the 25-root qualification below;
earlier HC-020 evidence did not check this response while disconnected. This does not
introduce saved profiles or switching to another enrollment.

## Interrupted browser enrollment increment

`TestControlPlaneBrowserEnrollmentInterrupted` starts the real CLI and waits
for at least two public poll requests before terminating that CLI process.
The pending operation must not register a node or invoke completion. After
approval of that same request, an explicit new CLI invocation must resume it,
create exactly one node and start an agent reporting that identity with a
valid map. A second enrollment operation is forbidden. Assertions use CLI/IPC
and contract events, without reading persisted enrollment or credential files.

This covers the process-interruption variant of HC-008/HC-010. It does not
cover interactive Ctrl-C handling, server-side cancellation, rejection recovery,
image cloning or foreign poll-token authorization. `RunContext` supplies a
bounded process-termination control to the test driver on each runner OS.

This increment introduced 25 roots, requiring 25 x 3 x 8 = 600 root outcomes
from 24 independent reports. Local vet, lint and the short suite passed;
the CI-only native test is qualified by the run below. Historical 24-root
evidence retains its original scope.

## Interrupted enrollment and client fixes qualified: 600 PASS

[CI 34652700516](https://github.com/endless-net/client/actions/runs/34652700516)
completed successfully for `d4eb13d40afe629204e80df05e2f54425095b169`.
All 24 native reports contain exactly the same 25 roots, each once:
**600 PASS, 0 FAIL, 0 SKIP**. The mandatory aggregate verified complete reports
for that source. Eight installation jobs, three OS verification jobs and the
separate Linux dataplane job passed. Optional external STUN was skipped and
is excluded from the required root totals.

Interrupted-enrollment elapsed times and full per-runner reports:

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| ubuntu-22.04 | [1.14s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254382) | [1.10s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254340) | [1.08s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254486) |
| ubuntu-24.04 | [1.95s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254273) | [1.09s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254375) | [1.09s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254347) |
| ubuntu-22.04-arm | [1.09s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254523) | [1.13s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254384) | [1.09s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254398) |
| ubuntu-24.04-arm | [1.21s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254592) | [1.09s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254414) | [1.09s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254448) |
| windows-2022 | [2.24s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254159) | [2.40s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254456) | [2.37s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254082) |
| windows-2025 | [2.76s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254451) | [2.97s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254118) | [2.76s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254088) |
| macos-15-intel | [1.22s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254166) | [1.48s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254220) | [1.24s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254174) |
| macos-15 | [1.13s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254158) | [1.11s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254127) | [1.16s](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254124) |

This qualifies HC-008 process interruption while approval is pending, explicit
CLI restart completing the same approved request, and exactly one resulting
node observed through agent IPC. It does not qualify interactive cancellation,
foreign poll tokens or rejection during active polling.

Every report also passed the expanded HC-020 assertions: selecting the current
network while disconnected preserves and accurately reports that intent before
and after restart, with no new registration. Multiple saved profiles and
switching to another enrollment remain outside this evidence.

The source includes the reference-stack lifecycle fix. All native direct,
resource and Relay cases completed without the prior teardown panic. The
[Linux participant race check](https://github.com/endless-net/client/actions/runs/34652700516/job/103439254164)
also passed `internal/testwireguard`, including the active-TCP teardown
regression. This is bounded regression evidence, not proof that every possible
concurrency interleaving has been exercised.

The subsequent 27-root source adds rejection during polling and diagnostic
export/status assertions. Those additions and the diagnostic runtime fix
still require their own hosted qualification. HC-001-HC-065 coverage remains
incomplete; this green source does not close unrelated variants or product gaps.

## Rejection during active enrollment polling increment

`TestControlPlaneBrowserEnrollmentRejectedDuringPolling` exercises HC-008 and
HC-013 by rejecting a request after the running CLI has issued at least two
polls. The CLI must report rejection without completing enrollment or obtaining
a node credential. Resuming the saved rejected operation must report its denial
and must not silently create a replacement. A subsequent explicit attempt must
create exactly one distinct request; only approval of that new request may
produce a node. The running agent must report that same identity and a valid
map, with exactly one registration across the scenario.

The test shares polling and recovery assertions with the existing expiry case,
while preserving their different public retry behavior. No Client runtime or
provider behavior was changed for this increment. It does not prove foreign
poll-token isolation, account binding or a server-side cancellation API.

The native inventory now contains 26 roots: 26 x 3 x 8 = 624 required outcomes.
Local vet, lint and the short suite passed. This native case is qualified by
the later 27-root run recorded below, whose full source still failed because
of diagnostic export. Earlier 24- and 25-root evidence cannot qualify this case.

## Diagnostic status and export increment

`TestControlPlaneDiagnosticsExport` adds HC-053/HC-056/HC-057 consumer checks:
unconfigured export must return its public error; configured diagnostics must
agree with public identity and connection intent while connected, disconnected
and after agent restart. The generated JSON artifact must remain inside the
configured output directory, match the published diagnostic schema and size,
and have valid creation/expiry metadata. An immediate repeated export must
identify the same reused artifact. Reconnecting must restore connected intent
in fresh diagnostics. The test reads only the exported artifact, never private
configuration, agent snapshots or credential files.

A short handler regression first reproduced diagnostic status losing persisted
disconnect (`desired=connected`, `user_disconnected=false`). Diagnostics now
uses the same connection-intent attachment as public status, including its
error handling. This fixes the exported status as well. Local short tests, vet
and lint passed. Native qualification required the timestamp correction below
and is recorded in the completed 28-root run.

Strict JSON decoding checks schema boundaries, not every possible secret value
inside allowed free-text fields. Comprehensive redaction fault injection,
export retention/expiry, flow-log consent and control/path/DNS/application fault
classification remain separate work. Cached exports are historical snapshots;
fresh status is checked through the diagnostics endpoint after reconnect.

The current native inventory has 27 roots and requires 27 x 3 x 8 = 648 outcomes.
Earlier 24-, 25- and 26-root sources do not qualify this added scenario.

## Native control-plane TLS trust: awaiting runner evidence

HC-057 flow reporting uses HTTPS-only protobuf RPC through the default HTTP
transport. Existing native scenarios use loopback HTTP; successful Relay TLS
with an explicitly configured CA does not establish control-plane TLS trust.
The next prerequisite is therefore a real Client HTTPS enrollment/map scenario.

`TestControlPlaneTLSTrustBoundary` (HC-021) requires enrollment rejection for an
unknown ephemeral HTTPS certificate even when map-signing trust was supplied.
The fixture must receive no registration. After explicitly trusting that CA,
enrollment and agent startup/restart must retain one node identity and valid map.
This does not test hostname mismatch, certificate expiry or trust rotation.

`TrustControlTLS` is restricted to GitHub Actions. Linux uses process-scoped
`SSL_CERT_FILE`; Windows installs the public CA in the current user's Root store;
macOS uses the runner's administrative trust store and system keychain. Cleanup
targets the exact certificate fingerprint and removes its trust entry on macOS
using the [Apple security tool commands](https://github.com/apple-oss-distributions/Security/blob/main/SecurityTool/macOS/security.c).
Only ephemeral public certificates are written; their private keys remain in
the fixture process. No TLS verification bypass or Client runtime change is
introduced. These operations run only on the disposable native CI jobs.

The current inventory has 28 roots, requiring 28 x 3 x 8 = 672 outcomes. Local
vet, lint and the short suite passed; native HTTPS trust remains unqualified.
HC-057 consent, wire reports, retries and revocation still need their own real
traffic scenario after this prerequisite; component flow tests are insufficient.

## Diagnostic export timestamp correction: awaiting native evidence

The 27-root run exposed a diagnostic-export metadata mismatch on
[macOS ARM](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254671)
and [Ubuntu 22.04](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254757):
an immediate repeated export did not preserve the first response's lifecycle
metadata. The run remains unqualified. The short store regression reproduced
the mismatch after adding timestamp and size comparisons to its reuse check.

The first response used the pre-write clock for creation and expiry, whereas
reuse and retention used the persisted file modification time. The first
response now uses that same persisted timestamp, including filesystem precision.
The existing native assertion remains unchanged. This is a Client runtime fix;
its full hosted evidence is pending, and the 28-root inventory is unchanged.

## Native flow consent lifecycle: awaiting hosted evidence

`TestControlPlaneNativeFlowConsent` (HC-057) uses the real OS UDP path and
reference WireGuard peer over IPv4, with an HTTPS control-plane fixture. Before
consent it requires no report RPC attempts while real traffic succeeds for
12 seconds, exceeding the documented ten-second collection window. After a
policy grants collection, an accepted protobuf report must match the Client's
source IP, peer destination IP, UDP port, protocol, observed decision, positive
packet/byte counters and consent interval. Live revocation must stop report
attempts for another 12 seconds while traffic continues. New consent must
produce new accepted wire evidence; an old report cannot satisfy recovery.

The fixture captures authenticated RPC message bodies independently of their
acceptance and returns copies. Request events additionally detect report
attempts with failed authorization. Credentials/headers and client-private state
are not captured. Fixture short tests verify immutable capture across repeated
requests and mutation of returned observations, and the revoked policy response.

Local short tests, vet and lint passed. Native execution awaits the same hosted
TLS setup as HC-021. The common inventory now has 29 roots and requires
29 x 3 x 8 = 696 outcomes. This does not qualify IPv6/TCP/denied-flow metadata,
expiration without a policy refresh, report retry/idempotency, crash-spool
recovery or production flow collection. Those remain Client-owned follow-up.

## Active enrollment rejection qualified; export blocks source acceptance

[CI 34653546587](https://github.com/endless-net/client/actions/runs/34653546587)
completed with failure for `774169be0f2a1837e1b32383bcf431a729909218`.
All 24 reports contain the same 27 roots: **624 PASS, 24 FAIL, 0 SKIP**.
Every failure is `TestControlPlaneDiagnosticsExport` at the immediate-reuse
metadata assertion. The source is not qualified; the timestamp correction
already described above belongs to a later commit and needs its own CI.

`TestControlPlaneBrowserEnrollmentRejectedDuringPolling` passed in all reports:

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| ubuntu-22.04 | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254915) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254757) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254799) |
| ubuntu-24.04 | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254896) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254813) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254796) |
| ubuntu-22.04-arm | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254789) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254853) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254793) |
| ubuntu-24.04-arm | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254950) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254806) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254833) |
| windows-2022 | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254883) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254865) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254937) |
| windows-2025 | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254925) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254800) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254920) |
| macos-15-intel | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442255057) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254802) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254924) |
| macos-15 | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254907) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254918) | [PASS](https://github.com/endless-net/client/actions/runs/34653546587/job/103442254671) |

This is bounded HC-008/HC-013 evidence: rejection while the CLI is polling,
no node registration/completion for the rejected request, rejection when the
saved operation is resumed, then a distinct explicitly approved attempt with
one resulting identity. It does not prove foreign poll-token isolation,
account binding, server-side cancellation or completion of these HC rows.

The diagnostic failure occurred after the checks for disconnected status,
restart, export path, schema and first-export identity. It prevented the final
reconnect assertion from running, so the scenario as a whole remains failed.
The newer TLS and flow-consent scenarios are absent from this source.

## Native trust-store import investigation

The 28-root run failed during test-CA installation on all three macOS Intel
repetitions, before the trusted Client enrollment step:
[repeat 1](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831035),
[repeat 2](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831056),
[repeat 3](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831025).
The original helper discarded OS-command output, so those logs do not prove
whether certificate profile or another import constraint caused the failure.

The HTTPS fixture now uses an ECDSA P-256 CA certificate with a nonempty subject
instead of the previous Ed25519 certificate with an empty subject. Map signing
continues to use its existing independent key and contract. Import/cleanup
failures now expose only exit code, deadline state and allowlisted error
categories, keeping raw command output private. The existing short TLS test
still verifies rejection without trust and success with explicit trust; local
short tests, vet and lint passed. OS-store compatibility remains to be proven
by native runners. No Client TLS verification bypass was added.

In the nine Linux reports inspected so far, all 28 roots passed, including the
diagnostic export correction and HTTPS boundary. This partial result does not
qualify the complete source or the new flow-consent scenario.

## Diagnostic export qualified; OS trust import blocks source acceptance

[CI 34654555171](https://github.com/endless-net/client/actions/runs/34654555171)
completed with failure for `1a63192e20e114d14a37965bf6aa56f1a41e4c53`.
All 24 native reports contain the same 28 roots: **660 PASS, 12 FAIL, 0 SKIP**.
All twelve Linux jobs passed. The twelve Windows/macOS jobs failed only
`TestControlPlaneTLSTrustBoundary`, before trusted enrollment, at the OS
certificate-import boundary. The full source remains unqualified.

The corrected diagnostic-export scenario passed on every native repetition:

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| ubuntu-22.04 | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830952) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831007) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830937) |
| ubuntu-24.04 | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830963) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830947) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830982) |
| ubuntu-22.04-arm | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830962) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830968) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831009) |
| ubuntu-24.04-arm | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831024) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830966) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830904) |
| windows-2022 | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831016) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830998) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830987) |
| windows-2025 | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830918) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831046) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831004) |
| macos-15-intel | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831035) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831056) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831025) |
| macos-15 | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830978) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444831001) | [PASS](https://github.com/endless-net/client/actions/runs/34654555171/job/103444830976) |

This qualifies diagnostic identity/intent while connected and disconnected,
persistence across agent restart, unconfigured-export rejection, public JSON
schema/path/size/lifetime metadata, stable metadata on immediate artifact reuse,
and fresh connected intent after reconnect. It does not prove every secret
redaction, retention expiry, tamper resistance or fault classification variant.

TLS trust rejection followed by explicitly trusted enrollment and restart passed
all Linux variants. Windows/macOS evidence stops at import failure and cannot
prove those later steps. The subsequent named P-256 CA profile and flow-consent
scenario are not part of this source and still require their own hosted run.
The latest goal still requires remaining HC-001-HC-065 cases and a complete
successful required Client matrix.

## Accepted flow window acknowledgement failure: awaiting native evidence

The existing native flow-consent root now additionally injects a retryable
protobuf error after the fixture accepts the first consented window. The real
Client must submit the same window again: node scope, consent revision, window
ID and complete protobuf body must remain identical. The comparison is against
captured wire messages, without reading the encrypted client queue. New consent
after revocation still requires fresh reporting evidence.

The fixture records acceptance before the injected unavailable response and
loses exactly one acknowledgement. A short RPC test verifies one acceptance,
one injected failure and a successful retry acknowledgement for the original ID.
Local short tests, vet and lint passed. This is Client retry evidence pending
native execution, not a production producer or durable-storage acceptance test.
It does not cover process crash after acceptance, retry lease expiry or
credential/consent changes while an unacknowledged window is pending.

The common inventory remains 29 roots / 696 expected native outcomes. Earlier
flow-consent sources lack this additional assertion and cannot qualify it.

## 2026-09-12: short flow consent qualified on Linux

[Run 34656410642](https://github.com/endless-net/client/actions/runs/34656410642)
at source `0328d13a1275c6dabecfca517fb3a3a95c126fff` completed with failure.
All 24 native reports were inspected with identical 29-root inventories:
**671 PASS, 25 FAIL, 0 SKIP**. Every Linux report passed all 29 roots
(348 outcomes). All eight installation jobs, three platform verification jobs
and the separate Linux control-plane job passed.

The flow root qualifies default-off, real IPv4 UDP metadata under a short grant,
immutable retransmission after accepted-window acknowledgement loss, live
revocation and renewed consent on these Linux runners:

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| ubuntu-22.04 | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020624) | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020532) | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020524) |
| ubuntu-24.04 | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020458) | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020482) | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020575) |
| ubuntu-22.04-arm | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020513) | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020540) | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020515) |
| ubuntu-24.04-arm | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020508) | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020473) | [PASS](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020577) |

Windows flow and TLS roots still stop at current-user certificate import.
On all six macOS jobs the flow test reaches its cleanup but fails to remove
trust; the later TLS root then times out during import. These failed roots are
not native qualification, even though the macOS flow assertions reached cleanup.
This source predates the Windows machine-store and scoped macOS authorization
corrections, natural-expiry extension and IPC-negotiation root.

One additional failure occurred in
[Windows 2025 repeat 1](https://github.com/endless-net/client/actions/runs/34656410642/job/103450020637):
`DiagnosticsExport` obtained its initial IPC response after 39 ms, then the
shared enrollment setup received no status responses for 15 seconds (16 failed
attempts, agent still alive). It never reached the export assertions. The
previous 24-platform export qualification remains historical evidence, but this
source is unqualified. Status synchronously calls WireGuard inspection under
the engine lock; contention during interface setup is an investigation lead,
not a proven cause. No status timeout or runtime behavior was changed for it.

The other 26 roots passed in every native job. Full coverage still requires the
30-root matrix and remaining HC variants; Linux flow evidence does not establish
expiry, crash-spool replay, IPv6/TCP/denied-flow reporting or producer conformance.

## 2026-09-12: flow expiry and TLS qualified on Linux and Windows

[Run 34657192800](https://github.com/endless-net/client/actions/runs/34657192800)
at source `b1d3b91021e25a0b6d610e04b94a936bce41e3dd` completed with failure.
All 24 native reports were read: identical 30-root inventories,
**701 PASS, 19 FAIL, 0 SKIP**. All twelve Linux repetitions passed every root.
All eight installation jobs, three platform verification jobs and the separate
Linux control-plane job passed. The complete source remains unqualified.

The TLS trust boundary and IPv4 UDP flow root both passed every repetition
below. Flow includes immutable acknowledgement-loss retry, live revocation,
unchanged-policy natural expiry, reporting silence after expiry and renewed
consent. TLS includes rejection before explicit trust, then enrollment and
identity-preserving restart with the trusted fixture.

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| ubuntu-22.04 | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688451) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688423) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688517) |
| ubuntu-24.04 | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688421) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688414) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688234) |
| ubuntu-22.04-arm | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688441) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688530) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688496) |
| ubuntu-24.04-arm | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688498) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688467) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688546) |
| windows-2022 | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688439) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688450) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688674) |
| windows-2025 | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688367) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688643) | [PASS](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688548) |

All six macOS jobs failed flow and TLS during the authorization-rule preparation;
that failed approach is superseded by the later disposable-VM trust lifecycle.
IPC negotiation passed all twelve Linux and six macOS jobs; all six Windows
jobs failed the immediate post-restart status assertion. Those results predate
both nonblocking status inspection and the runtime-readiness wait correction.

One additional failure in
[Windows 2025 repeat 2](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688643)
affected `NativeTCPTraffic`: the UDP baseline before the ICMP-only policy step
returned an unclassified probe exit 1. This is neither a confirmed policy denial
nor a known payload mismatch, and remains an investigation item. No raw probe
output was retained by the existing diagnostic filter. The other 26 roots passed
on every native job. The subsequent diagnostic-retention extension is not part
of this source and requires new evidence.

## 2026-09-12: 23 native reports passed; one build missing

[Run 34658370761](https://github.com/endless-net/client/actions/runs/34658370761)
at source `1af02785d498f624281cf385d6516065b83ac812` completed with failure.
All 23 available native reports were read and contained identical 30-root
inventories: **690 PASS, 0 FAIL, 0 SKIP**. The remaining macOS Intel repetition
failed to download pinned Go modules after a DNS failure, before compiling or
executing tests. Its 30 missing outcomes are not passes or skips. The required
[`verify` job](https://github.com/endless-net/client/actions/runs/34658370761/job/103458307510)
failed on the unsuccessful matrix dependency. All eight installation jobs,
three OS verification jobs and the separate Linux control-plane job passed.

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| ubuntu-22.04 | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542817) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542575) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542927) |
| ubuntu-24.04 | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542616) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542746) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542604) |
| ubuntu-22.04-arm | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542567) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542684) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542606) |
| ubuntu-24.04-arm | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542580) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542857) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542899) |
| windows-2022 | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542685) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542652) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542626) |
| windows-2025 | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542759) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542595) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542545) |
| macos-15-intel | [NO EXECUTION: DNS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542603) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542662) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542590) |
| macos-15 | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542654) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542599) | [PASS](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542651) |

Every executed root passed, including native TLS trust, IPv4 UDP flow consent,
immutable retry, revocation, expiry and renewal; IPC negotiation and restart
recovery; diagnostic export reuse, timestamp-based retention, preservation of
an unrelated operator file and fresh connected export. These are 23-repetition
results across all eight runner variants, not complete 24-repetition qualification.
The macOS fixture removes the exact CA certificate from the keychain; its
hash-bound trust entry lasts until disposable VM destruction. No successful
explicit removal of that final trust entry is claimed.

The source includes nonblocking status/diagnostic engine inspection. Component
regressions prove the blocked-interface-creation case; these native results
show no recurrence of the earlier status failure under the exercised scenarios,
not a bound on every OS operation. The old unknown UDP-probe failure did not
recur, but its cause remains unidentified. This run predates the probe deadline
category, dependency-download retry, TLS hostname mismatch and IPC request
validation additions. The current inventory is 31 roots / 744 expected outcomes;
full-source qualification and remaining HC variants are still open.

## Complete 31-root platform evidence: hostname fixture failure

Run [34659063792](https://github.com/endless-net/client/actions/runs/34659063792)
completed with failure at source `a9a1a218f32f999a2ac3fbbe10401f8c71e86b39`.
All 24 native reports were inspected: identical 31-root inventories, **720 PASS,
24 FAIL, 0 SKIP**. Every failed root is `TestControlPlaneTLSTrustBoundary`, and
every job contains the hostname-mismatch certificate-error assertion failure.
The other 30 roots passed in every repetition. Job links below identify repeats
1, 2 and 3 respectively; each job has 30 passes and one TLS failure.

| Platform | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| Ubuntu 22.04 x64 | [103458324775](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324775) | [103458324609](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324609) | [103458324710](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324710) |
| Ubuntu 24.04 x64 | [103458324660](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324660) | [103458324620](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324620) | [103458324725](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324725) |
| Ubuntu 22.04 ARM64 | [103458324729](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324729) | [103458324900](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324900) | [103458324662](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324662) |
| Ubuntu 24.04 ARM64 | [103458324756](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324756) | [103458324690](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324690) | [103458324675](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324675) |
| Windows 2022 | [103458324784](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324784) | [103458324751](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324751) | [103458324831](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324831) |
| Windows 2025 | [103458324768](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324768) | [103458324726](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324726) | [103458324781](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324781) |
| macOS 15 Intel | [103458324754](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324754) | [103458324709](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324709) | [103458324825](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324825) |
| macOS 15 ARM64 | [103458324679](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324679) | [103458324617](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324617) | [103458324796](https://github.com/endless-net/client/actions/runs/34659063792/job/103458324796) |

This supplies all 24 repetitions for diagnostic export retention, native IPC
negotiation/restart and IPv4 UDP flow consent/retry/expiry, including the macOS
Intel repetition missing from the preceding run. The initial IPC request
validation root also passes all 24: wrong method, unknown route, malformed JSON,
unknown fields and trailing JSON, unchanged intent, then valid disconnect/connect.
It predates the non-object rejection fix, event subscriptions, isolated TLS
hostname fixture and request-size boundary extension. Those changes need their
own hosted evidence.

All eight install/smoke jobs, three OS verification jobs and the separate Linux
control-plane job passed. Required aggregate `verify` failed; optional external
STUN was skipped. The source is not qualified for publication. The TLS fixture
was corrected in `1d841847390288d7ab7804624dd777374a0abbf4`: isolate the mismatch
profile and explicitly restrict its origin list, preventing failover to the
valid IP origin retained after the first enrollment attempt. This is a fixture
correction, not evidence of bypassed Client certificate validation. Complete
HC-001–HC-065 coverage and the remaining platform variants are still open.

## Complete 32-root execution evidence; artifact upload failure

Run [34660106943, first attempt](https://github.com/endless-net/client/actions/runs/34660106943/attempts/1)
completed with failure at source `1d841847390288d7ab7804624dd777374a0abbf4`.
All 24 job logs contain identical 32-root inventories: **768 PASS, 0 FAIL,
0 SKIP**. The following links identify the original repetitions, including the
failed artifact-upload job; each log contains all 32 passing root outcomes.

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| macos-15 | [103461308446](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308446) | [103461308511](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308511) | [103461308500](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308500) |
| macos-15-intel | [103461308534](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308534) | [103461308525](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308525) | [103461308498](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308498) |
| ubuntu-22.04 | [103461308411](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308411) | [103461308575](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308575) | [103461308491](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308491) |
| ubuntu-22.04-arm | [103461308439](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308439) | [103461308541](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308541) | [103461308419](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308419) |
| ubuntu-24.04 | [103461308475](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308475) | [103461308552](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308552) | [103461308529](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308529) |
| ubuntu-24.04-arm | [103461308453](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308453) | [103461308469](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308469) | [103461308509](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308509) |
| windows-2022 | [103461308471](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308471) | [103461308457](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308457) | [103461308442](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308442) |
| windows-2025 | [103461308538](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308538) | [103461308473](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308473) | [103461308423](https://github.com/endless-net/client/actions/runs/34660106943/job/103461308423) |

The macOS Intel repeat-2 job completed compilation, native tests and JSON report
production successfully, then failed `Preserve platform contract reports` with
`Failed to CreateArtifact: Unable to make request: ENOTFOUND`. Aggregate
`verify` failed its dependency check and did not run the complete-artifact
validator. Thus all native executions passed, but this attempt does **not**
qualify the source for publication: the required evidence artifact is missing.
After confirming that terminal failure, only that job and its dependent gate
were requested again on the same source. Attempt 2 was cancelled while pending
as newer source runs entered the shared concurrency queue; it produced no new
execution evidence. The original artifact failure remains unresolved for this
source. No failing test was retried or hidden; the latest full source matrix
must qualify its own complete artifact set.

This source qualifies the executed TLS hostname rejection and explicit CA trust
cases on all eight runners, the original native IPC event stream lifecycle,
and rejection of null/array/scalar mutation bodies without changing intent.
It predates request-size boundaries, simultaneous subscription isolation,
AAAA DNS records, trust-confirmation intent repair and map-signing rotation.
Those additions require their own hosted evidence. The current working inventory
is 33 roots / 792 outcomes; neither the 32-root execution evidence nor a future
successful gate alone establishes full HC-001–HC-065 coverage.

## Qualified 33-root eight-platform source

Run [34661280214](https://github.com/endless-net/client/actions/runs/34661280214)
completed successfully at source `88f335e5b4a2fee0fe8c96a13856718398b0f58c`.
All 24 native job logs were inspected: identical 33-root inventories, **792 PASS,
0 FAIL, 0 SKIP**. All eight installation jobs, three OS verification jobs and the
separate Linux control-plane suite also passed. Optional external STUN was skipped
and is not included in the native count or this qualification.

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| macos-15 | [103464356392](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356392) | [103464356641](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356641) | [103464356523](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356523) |
| macos-15-intel | [103464356484](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356484) | [103464356583](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356583) | [103464356598](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356598) |
| ubuntu-22.04 | [103464356356](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356356) | [103464356363](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356363) | [103464356418](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356418) |
| ubuntu-22.04-arm | [103464356434](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356434) | [103464356379](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356379) | [103464356402](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356402) |
| ubuntu-24.04 | [103464356408](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356408) | [103464356582](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356582) | [103464356376](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356376) |
| ubuntu-24.04-arm | [103464356432](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356432) | [103464356417](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356417) | [103464356439](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356439) |
| windows-2022 | [103464356461](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356461) | [103464356612](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356612) | [103464356472](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356472) |
| windows-2025 | [103464356520](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356520) | [103464356618](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356618) | [103464356445](https://github.com/endless-net/client/actions/runs/34661280214/job/103464356445) |

The [required aggregate gate](https://github.com/endless-net/client/actions/runs/34661280214/job/103467144576)
passed both dependency verification and complete artifact validation. Its report
confirmed 33 common scenarios across three repetitions and eight platforms with
792 passing outcomes and no skips. Unlike the preceding source's missing upload,
this run supplied the complete evidence set and qualifies the cited source under
the current Client source-publication gate.

This increment confirms on all eight runners:

- Existing and boundary-size IPC mutation requests, including the non-object
  rejection fix, plus independent event subscriptions, cancellation and restart.
- AAAA DNS record lookup, withdrawal and restoration over UDP/TCP, retaining
  private-domain upstream isolation; this is not OS resolver installation.
- The trust-confirmation intent fix: repeating confirmation of the current key
  while disconnected preserves intent and native IPv4/IPv6 TCP/UDP/ICMP denial.
- The initial map-signing rotation case: separate credential trust, stale-key
  rejection, explicit new-key confirmation while disconnected, same-node renewal,
  durable trust/intent after restart and a fresh signed map after explicit connect.

This source predates the connected/interrupted rotation subcases, explicit retry
for disconnected pending recovery, and real traffic checks after changed-key
rotation. Those extensions await their own complete source matrix. HC-059 remains
a separate independent node-endorsement product decision; server identity tests
belong to HC-021. Installation privilege failures, interrupted installation, actual
OS reboot and other open HC variants are not qualified by this green run. Full
HC-001–HC-065 completion remains unproven.

## Interrupted rotation matrix: complete failure evidence

Run [34661999142](https://github.com/endless-net/client/actions/runs/34661999142)
tested source `efcf82bf4792571fea6c5da638932b11621b40a9` and completed with failure.
All 24 native reports contain the same 33 root scenarios: **768 PASS, 24 FAIL,
zero SKIP**. Every report failed only `TestControlPlaneMapSigningRotation`.
Its connected and disconnected ordinary rotation subcases passed in all reports;
both interrupted subcases failed in all reports (48 passing and 48 failing child
outcomes, counted separately from the root totals).

All eight installation jobs, the three OS verification jobs and the separate
Linux control-plane job passed. The required aggregate
[verify job](https://github.com/endless-net/client/actions/runs/34661999142/job/103470373394)
failed. Optional external STUN compatibility was skipped and contributes no
qualification evidence. This source is not qualified for publication.

The interruption fixture returned inconsistent public-error body/header request
IDs, as described below. Fix `a53850f3d83b5df67cc8d556d21e9d055c8cb027` makes
those responses contract-valid; it still requires a successful hosted run.
The four direct-traffic roots passed on every runner, including their added
post-rotation IPv4/IPv6 TCP/UDP/ICMP checks. This bounded execution evidence does
not override the failed source gate or establish interrupted recovery.

This run predates the configuration symlink ownership/storage changes and Unix
SIGTERM shutdown assertion. Their results must come from a later source matrix.

## Interrupted trust recovery and configuration ownership qualified

Run [34663088786](https://github.com/endless-net/client/actions/runs/34663088786)
qualified source `4f117a0fcf8831d26eb59948774aaa9e0f799f36`. All 24 native
reports contain the same 33 roots: **792 PASS, zero FAIL, zero SKIP**. All four
map-signing rotation subcases passed in each report (96 child passes, counted
separately from the root totals).

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| macos-15 | [103470474401](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474401) | [103470474378](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474378) | [103470474440](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474440) |
| macos-15-intel | [103470474441](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474441) | [103470474385](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474385) | [103470474201](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474201) |
| ubuntu-22.04 | [103470474398](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474398) | [103470474319](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474319) | [103470474435](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474435) |
| ubuntu-22.04-arm | [103470474370](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474370) | [103470474371](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474371) | [103470474436](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474436) |
| ubuntu-24.04 | [103470474410](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474410) | [103470474363](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474363) | [103470474423](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474423) |
| ubuntu-24.04-arm | [103470474438](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474438) | [103470474337](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474337) | [103470474397](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474397) |
| windows-2022 | [103470474407](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474407) | [103470474418](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474418) | [103470474505](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474505) |
| windows-2025 | [103470474453](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474453) | [103470474366](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474366) | [103470474519](https://github.com/endless-net/client/actions/runs/34663088786/job/103470474519) |

The [aggregate gate](https://github.com/endless-net/client/actions/runs/34663088786/job/103473494660)
passed both platform dependencies and complete same-source artifact validation.
All eight installation jobs, three OS verification jobs and the separate Linux
control-plane job passed. Optional external STUN compatibility was skipped and
is not counted as coverage.

This source confirms ordinary and interrupted map-signing rotation while
connected and disconnected, pending-operation survival across agent restart,
explicit retry for disconnected recovery, same-node renewal, durable trust and
intent, and restored IPv4/IPv6 TCP/UDP/ICMP traffic after changed-key rotation.
The request-ID correction makes the interruption fixture contract-valid; this
qualification supersedes the preceding failed interruption run.

Configuration file symlink ownership, duplicate rejection and successor startup
through the alias passed on all eight platforms. Unix roots also require clean
SIGTERM exit before successor startup. Windows uses abrupt process termination
in this root; this is not Unix signal evidence or a substitute for SCM tests.
Hard-link aliases, concurrent first startup and real OS reboot remain separate.

This source predates the stopped-service CLI assertions, CLI event timeout and
missing-hello correction, and split-DNS SERVFAIL recovery extension. Those changes
await their own matrix. Full HC-001–HC-065 coverage remains incomplete.

## Installed CLI absence checks: eight-platform execution evidence

Source `38050bc60c608d69eeed399f9e86172d270bbd0c` in run
[34664269724](https://github.com/endless-net/client/actions/runs/34664269724)
passed all eight installation jobs. Each report contains PASS for the root and
its fresh-install, disconnect-survives-service-restart, enrolled-reinstall and
uninstall subcases, with no failed or skipped installation cases.

| Runner | Installation report |
| --- | --- |
| macos-15-intel | [103473575709](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575709) |
| macos-15 | [103473575697](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575697) |
| ubuntu-22.04-arm | [103473575732](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575732) |
| ubuntu-22.04 | [103473575773](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575773) |
| ubuntu-24.04-arm | [103473575699](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575699) |
| ubuntu-24.04 | [103473575799](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575799) |
| windows-2022 | [103473575736](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575736) |
| windows-2025 | [103473575809](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575809) |

The enrolled-reinstall subcase at this source includes stopped-service checks
for status, networks, diagnostics, connect, disconnect and events, in both
connection states. Every invocation must fail with code 1, stderr and no stdout
payload. Restart retains the original identity and intent, restores connected
TCP traffic and preserves disconnected traffic denial without registration.
These observations qualify that bounded HC-004/052/053 installed-service variant.
They do not test an unauthorized local user or a stalled but reachable IPC peer.

The common 24-report contract matrix and aggregate gate had not completed when
this installation evidence was recorded; it does not independently qualify the
entire source for publication. Later IPv6 DNS upstream and TCP-retry extensions
are absent from this source.

## Next work

The DNS wire root now independently varies the Client listener address family
and upstream address family, with complete/truncated UDP replies: eight variants
per runner. Every listener must report exactly the requested loopback address,
then accept both UDP and TCP queries. The full private/global/split lifecycle
runs through each combination. This adds native IPv6 proxy-listener acceptance
without assuming it from IPv6 records or upstreams; hosted evidence is pending.

HC-025/026 also runs both upstream address families with truncated UDP answers.
The fixture binds TCP on the same endpoint, supplies no UDP answer records and
requires the Client to repeat the DNS question over length-framed TCP. Domain
observations and doubled query counts cover successful resolution and split
SERVFAIL/recovery without leaking to the global resolver. This is normal DNS
transport retry, not a legacy protocol fallback. Native qualification is pending.

The native HC-025/026 DNS scenario now runs separately with UDP upstreams bound
to IPv4 and IPv6 loopback. Each variant repeats A/AAAA local-map projection,
withdrawal/restoration, global/split selection, blocked-domain denial and split
SERVFAIL/recovery with exact wire-query isolation checks. Failure to bind the
required upstream family fails the scenario rather than skipping it. Requests
to the Client proxy use independent IPv4/IPv6 loopback listeners over UDP/TCP,
as described above. TCP retry is covered by the separate truncated-answer
variants above. Hosted qualification is pending.

HC-025/026 DNS wire recovery now injects SERVFAIL from the selected split
resolver while keeping the global resolver healthy. UDP and TCP requests must
preserve private-domain isolation, public and local-map resolution, and recover
the split answer in the same proxy process when its upstream resumes success.
Exact observed upstream question counts and domains detect fallback leakage.
This is DNS response-error recovery, not packet loss, transport timeout, OS
resolver setup or live map reload; eight-platform qualification remains pending.

A short CLI regression reproduced false success when an event subscription
timed out or reached EOF before receiving hello. The command now treats those
unopened streams as errors while preserving successful listening-timeout exit
after a received hello. Both negative cases failed before the correction; the
positive timeout case guards normal CLI behavior. This component regression
does not itself qualify stalled native IPC transports on hosted platforms.

The HC-052/053 event scenario now also executes real `service events --timeout
2s` commands while connected and after restarting a disconnected agent. It
requires successful timeout termination (with an outer ten-second observation
allowance for process startup), one complete JSON event per line, hello first,
increasing stream-local sequences, negotiated metadata and the current enrolled
status. Public status after completion must retain the connection intent.
This supplements SDK subscriptions; hosted evidence for this CLI path is pending.

HC-004/052/053 now have installed-service absence assertions in both enrolled
connection states: six public CLI read/mutation/subscription commands must fail with exit 1,
stderr and no stdout payload within a bounded harness deadline. Restart must
retain identity and intent, restore connected TCP access and preserve disconnected
traffic denial without registration. This runs inside the existing eight-platform
installation matrix; short compilation alone does not qualify the new assertions.

HC-005 foreground shutdown now has an explicit Unix SIGTERM path: the agent's
signal context handles SIGTERM as well as Interrupt. The native ownership root
requires a zero exit code within the helper's existing five-second shutdown
budget before starting the successor with retained identity/intent. A fallback
kill is cleanup only and fails this assertion. Windows continues to exercise
abrupt process termination here; SCM shutdown belongs to installed-service tests.
This closes a missing test boundary rather than treating the old permissive
`Stop` helper as proof of graceful signal handling. Unix hosted qualification is
recorded at source `4f117a0` above.

The first interrupted-rotation run exposed an invalid fixture response: its body
used `request_id=rotation-renewal-unavailable`, while `SetResponseFault` emitted
`X-Request-ID: test-request`. The Client correctly classified this mismatch as a
protocol failure rather than a retryable outage. The fixture now provides a
typed `SetPublicError` helper with matching body/header IDs, and the rotation test
uses it for stable renewal unavailability. A public HTTP regression validates
both repeated temporary and authorization errors with the producer's decoder
and HTTP-response validator. Client error classification is unchanged; corrected
interrupted-recovery evidence passed the complete `4f117a0` native matrix above.

The native ownership test now also starts the successor agent through the file
symlink after releasing the original owner. The same identity and connection
intent must survive, subsequent duplicate contenders must still be rejected,
and filesystem metadata must show that startup/mutations did not replace the
symlink. The alias lives beside the original configuration, keeping the helper's
public state-output directory unchanged. This checks legitimate alias use as well
as duplicate rejection; all-platform execution passed at `4f117a0` above.

Configuration storage and agent locking now resolve the same file location,
including file/directory symlinks and missing profile directories beneath an
existing aliased ancestor. Atomic writes therefore target the original file,
and a file alias cannot select a different lock by its basename. Dangling links
and loops fail; the unsupported Linux state location remains rejected after
resolution. Linux/macOS component checks exercise shared store/lock ownership,
alias preservation after write, missing profiles and dangling links. They do not
run on the local Windows workstation; the strict eight-platform native alias
probe remains the required evidence. Hard links and path replacement races are
not covered by this change. Hosted qualification is recorded at `4f117a0` above.

The native single-agent ownership root adds a file symlink naming its existing
configuration as a fourth simultaneous contender, with a distinct IPC endpoint.
It must receive the same ownership error as the direct and lexical paths while
the original process retains identity, intent and map updates. Creating the alias
uses filesystem metadata only, with no private Client state read; inability to
create it fails the hosted test rather than skipping a platform. At introduction,
path resolution did not explicitly resolve symlinks. The subsequent shared-path
fix and native qualification are recorded above. Hard links and simultaneous
startup without an existing owner remain separate gaps.

The four native direct-traffic roots now rotate the map signer while connected,
reject confirmation of the old key, explicitly confirm the new key and require
real application traffic plus ICMP echo to recover. They restart the agent and
repeat the traffic checks before the existing terminal credential-retirement
phase. The reference peer keeps its keys and overlay address; only its return
endpoint follows the public Client listening port. Existing identity and single
enrollment assertions still apply. This extends changed-key recovery to native
IPv4/IPv6 TCP/UDP/ICMP dataplane evidence, qualified at `4f117a0`; it does
not establish uninterrupted existing sessions during rotation. The inventory
remains 33 roots / 792 native root outcomes.

The interrupted rotation test distinguishes recovery scheduling from tunnel
intent: a connected agent retries automatically after the renewal fault is
removed, while an explicitly disconnected agent suppresses background control
work. In the disconnected subcase the operator repeats the same trust command
after fault removal; it must resume the existing operation ID and complete
without connecting. Pending recovery is asserted through `recovery.state`, since
the top-level status can also reflect disconnected intent or an invalid old map.
The previous statement that fault removal completes both
subcases implicitly is superseded by this explicit retry boundary. No Client
runtime behavior is changed by this test correction.

The map-signing rotation root also covers a stable public
`temporarily_unavailable` response on credential renewal for both initial intents.
The Client must expose a recovering operation, retain its operation ID and node
credential across agent restart, and return `already_applied` for the same
confirmation while renewal is still unavailable. Removing the wire fault must
complete recovery with the original identity and desired connection state.
The test observes public recovery status/operation IDs, not a persisted recovery
file. This adds interrupted-recovery subcases within the 33-root inventory;
hosted qualification is pending and abrupt OS crash remains separate.

The map-signing rotation root now runs both initial connection intents as separate
native subcases. Each must preserve its intent through recovery, repeat the
completed confirmation as `already_applied`, and preserve intent/new trust after
restart. The connected case also requires a healthy public WireGuard inspection;
only the disconnected case issues an explicit connect before the final fresh-map
check. This remains control/IPC recovery evidence, not independent traffic proof
for changed-key rotation. Both subcases await hosted qualification within the
same 33-root inventory / 792 native root outcomes.

Requirement mapping correction: the architecture catalog defines HC-059 as node
admission endorsed independently of the control plane (the B20 / Tailnet Lock
comparison), whereas HC-021 explicitly includes control endpoint, TLS and server
identity errors. The server trust-confirmation and map-signing rotation tests
therefore cover HC-021 variants; they do **not** qualify HC-059. Earlier mentions
of HC-059 alongside those tests in this ledger are superseded by this correction,
without changing their execution evidence. The [business analysis on main](https://github.com/endless-net/architecture/blob/main/docs/ru/headless-client-business-analysis.md)
keeps independent endorsement under BR-05 / Q-05 as a separate product decision.
The [HC catalog on main](https://github.com/endless-net/architecture/blob/main/docs/ru/headless-client-use-cases.md)
requires unsigned-node denial and recovery after loss of a signing device for
that capability. Neither ordinary login nor server-key rotation proves those
requirements. Client work remains scoped to its published contracts; no Signing
or Coordinator implementation work is authorized by this test increment.

`TestControlPlaneMapSigningRotation` changes the testserver's map-signing key
while retaining node-credential and relay trust. A public-wire fixture check
verifies the old credential still authorizes map streaming, the old map trust
rejects the new signature, and the new announced map trust verifies it. The
native Client test inspects the changed identity, rejects stale confirmation,
explicitly confirms the new key while disconnected, recovers the same enrollment,
restarts with durable disconnected intent/new trust, then explicitly reconnects
and consumes a newly signed map. It checks published CLI/IPC and server events
without reading private Client state. The inventory is now 33 roots / 792 native
outcomes. This is pending hosted evidence; interrupted rotation, traffic during
rotation and additional signing-scope combinations remain separate variants.

The four native direct-traffic roots now reaffirm the current signing key twice
after disconnect, then require IPv4/IPv6 TCP/UDP and ICMP echo denial to remain
effective. The reference peer stays available; if public IPC exposes a newly
opened Client port, its return endpoint is refreshed so stale fixture routing
cannot manufacture denial. Existing disconnected restart and explicit reconnect
checks follow with the same identity. This adds dataplane evidence for the trust
intent fix beyond status-only assertions; hosted qualification remains pending.

A component regression reproduced unchanged trust confirmation attempting tunnel
configuration despite explicit disconnected intent (even `already_applied` took
the connect path). Trust recovery now reads the durable connection intent before
reconnecting; disconnected intent is retained and an unreadable intent fails
closed. Reaffirming the current key twice succeeds without an engine in the
regression. The native trust root repeats the same confirmation through public
CLI, checks the response and unchanged identity/cached-map/intent, restarts the
agent and requires disconnected intent to survive, then explicitly reconnects.
The regression establishes the unconditional-connect defect; native evidence
and successful changed-key rotation remain pending. No protocol version changes.

The DNS wire root now also queries AAAA records over both UDP and TCP for a
dual-stack peer, withdraws that peer and requires NXDOMAIN, then restores its
IPv6 answer. Absent private AAAA names must also return NXDOMAIN. Existing
upstream transcript checks remain exact, so these additional private queries
must not leak to global or split upstreams. Answer owner, type, class and address
are checked against the question and published map. This is IPv6 DNS record
coverage over a loopback IPv4 DNS transport, not IPv6 underlay, system resolver
installation or live reload: each CLI proxy still loads a fresh map. Hosted
qualification is pending; the inventory remains 32 roots / 768 outcomes.

The native IPC event root now opens two simultaneous subscriptions. Each must
receive its own hello, current snapshot and the subsequent disconnect event,
with independently increasing sequence numbers. After explicit cancellation
of the second subscription, the first must still report reconnect and another
disconnect before the existing agent-restart recovery phase. The test uses only
the published native IPC transport and observable events. This adds subscription
isolation to the pending event-stream evidence; it does not test slow consumers
or claim resumable event history. The inventory stays at 32 roots / 768 outcomes.

The IPC schema now documents the existing 65536-byte JSON mutation body limit,
including whitespace, and HTTP 413 / `request_too_large` before action dispatch.
The native request-validation root sends a valid object one byte above that
limit and verifies unchanged identity/intent, then sends a valid object exactly
at the limit and requires successful disconnect followed by normal reconnect.
This publishes and tests existing behavior without a protocol version increase.
The boundary extension awaits hosted evidence; unauthorized local-user access
remains a separate variant. The inventory remains 32 roots / 768 outcomes.

The first hostname-mismatch run exposed fixture contamination: failed initial
enrollment can retain the valid IP control origin, and a later `--server`
prepends an origin rather than replacing the failover list. The mismatch probe
now uses an isolated profile and an explicit single `--coordinator` origin.
It must still return a certificate error and leave the HTTP transcript unchanged;
the positive enrollment uses the original valid IP profile. Client origin
failover behavior is unchanged. The corrected hostname assertion awaits CI.

`TestControlPlaneIPCEvents` exercises the published NDJSON stream through the
real native socket/pipe: hello, current snapshot, increasing stream-local
sequence, timestamps, disconnect notification, subscription termination when
the agent stops, a new hello/snapshot after restart, reconnect notification and
explicit cancellation. It checks the same enrolled identity and retained intent
throughout, without persisted-state reads. The sequence is not used as a resume
cursor. The common inventory is now 32 roots / 768 expected native outcomes;
this extension awaits hosted evidence. Slow-consumer and malformed-stream
variants remain separate.

The [published IPC schema](client-ipc-v2.openapi.yaml) declares mutation request
bodies as objects. A component regression reproduced `null` (including padded
`null`) reaching the disconnect action through Go's struct JSON decoder. The
reader now rejects non-object bodies with `invalid_json` before dispatch;
an absent optional body and `{}` still each execute exactly once. The native
request-validation root now sends null, arrays, booleans, numbers and strings
and checks rejection without changing client intent. Protocol/schema versions
are unchanged. The regression proves the local decoder defect; complete native
qualification of the extension remains pending at 31 roots / 744 outcomes.

`TestControlPlaneIPCRequestValidation` adds native Unix-socket/Windows-pipe
checks for wrong method, unknown route, malformed JSON, unknown fields and
trailing JSON. Each request must return its published typed error and protocol
metadata without changing connected intent, identity or the cached map. Valid
CLI disconnect/connect must still work afterward. The test uses the public IPC
transport and DTOs, with no runtime-internal imports or private-state reads.
The common inventory is now 31 roots / 744 native outcomes. Hosted evidence is
pending; request-size boundaries and hostile local-user authorization remain
separate variants.

The native TLS root now also calls the trusted fixture through `localhost`,
while its certificate names only `127.0.0.1`. The test first verifies the OS
resolves that alias to the listener, requires a certificate error and no request
at the control HTTP handler, then enrolls successfully through the certificate's
IP address. This checks hostname verification independently of adding CA trust.
The root inventory stays at 30 / 720 outcomes. Previous TLS evidence predates
this assertion; certificate expiry and rotation remain separate cases.

[macOS Intel repeat 1 of run 34658370761](https://github.com/endless-net/client/actions/runs/34658370761/job/103455542603)
failed before compiling its contract runner: DNS lookup of `proxy.golang.org`
timed out and then returned no host while downloading pinned modules. It has no
native scenario outcomes and cannot count as platform evidence. The Client
workflow now downloads pinned modules before compilation, with at most three
attempts under a three-minute step deadline, then verifies `go.mod`/`go.sum`
remain unchanged. It does not retry tests, change dependency versions or switch
registries. Continued download failure still fails the required job and source.

The packet probe now emits the fixed category `application deadline setup failed`
when a socket cannot accept its deadline. A closed-pipe component regression
failed before the change and verifies that this remains an internal probe
failure (exit 1), never an accepted network-denial outcome (exit 2). The native
harness recognizes that fixed message without exposing arbitrary socket errors.
This improves future diagnosis; it does not identify the cause of the historical
unclassified Windows probe failure, whose raw output was not retained.

The native diagnostic-export root now ages its own public JSON artifact using
the retention duration advertised by the CLI. The next export must delete the
aged artifact, create a different file in the configured directory, preserve an
unrelated operator note and capture the current connected identity/intent.
Only the exported file's OS timestamps are changed; no private config, queue,
snapshot or credential is read or altered, and no agent clock is injected.
This is file-retention evidence pending all native repetitions, not a seven-day
wall-clock soak or comprehensive tamper/size-quota qualification. The inventory
remains 30 roots / 720 outcomes; previous export evidence lacks this extension.

The IPC-negotiation root in the first three completed Windows 2022 repetitions
of [run 34657192800](https://github.com/endless-net/client/actions/runs/34657192800)
failed its immediate status assertion after agent restart; its incompatible
read/mutation probes and overlapping negotiation had already completed.
The recovery phase now uses the existing bounded runtime-readiness wait after
IPC endpoint startup. Immediate identity/intent assertions after each rejected
request remain unchanged; transport errors are diagnosed separately from a
state mismatch. No transition deadline or negotiated version is increased.
These three jobs passed their native TLS and flow consent/expiry/retry roots;
complete Windows and cross-platform qualification still requires all reports.

A short component regression now holds platform TUN creation pending while
requesting engine inspection. The previous blocking inspection path failed the
bounded observation; the new `TryInspection` reports unavailability without
waiting for the engine mutex, then becomes available after configuration
recovers. Agent status and diagnostic snapshots use this path and explicitly
report `inspection unavailable` while retaining identity, cached-map and intent
fields. Their deadlines and wire versions are unchanged. This isolates engine
lock contention; it does not prove every OS inspection call is bounded or that
the earlier Windows timeout had this exact cause. Native qualification is pending.

The first macOS ARM repetition in
[run 34657192800](https://github.com/endless-net/client/actions/runs/34657192800/job/103452688613)
rejects both the scoped authorization-rule write and restoration with exit 255.
The root-only authorization preparation is therefore not a confirmed fix;
macOS TLS/flow qualification remains open. The same job completes its IPC
negotiation restart sequence, but full-matrix evidence must be assessed separately.

`TestControlPlaneIPCNegotiation` extends HC-053/HC-058 through the published
IPC package and the real agent's Unix socket or Windows named pipe. Missing,
wrong-protocol, malformed, nonpositive, reversed, disjoint and duplicate version
headers must return the specified HTTP status, typed error and server metadata
for both status and disconnect requests. Rejected mutations must preserve the
connected intent, cached map and enrolled identity. An overlapping range must
negotiate the current version; agent restart and subsequent valid CLI
disconnect/connect must still work with one enrollment. Unsupported numbers
are malformed-input probes, not a version increase or compatibility support.
This does not prove binary artifact/build identity or unauthorized local-user
access. The common inventory is now 30 roots / 720 expected native outcomes;
qualification awaits the corresponding complete hosted source matrix.

The attempted scoped macOS authorization-rule override was rejected by hosted
runners and has been removed. The fixture imports its ephemeral public CA,
then deletes that exact certificate from System.keychain by fingerprint. The
certificate-hash Admin Trust Settings entry remains until GitHub destroys the
disposable VM. The corresponding signing key exists only in fixture memory and
is never persisted. Cleanup failure for the keychain certificate remains fatal.
The helper requires both `GITHUB_ACTIONS=true` and
`RUNNER_ENVIRONMENT=github-hosted`; it is not a managed-host cleanup procedure.
No authorization rule, runner image or Client TLS verification is modified.
The negative untrusted enrollment and positive explicitly trusted enrollment
assertions are unchanged. Exact trust-entry removal is not claimed by this
fixture; platform qualification of its revised lifecycle is still pending.

The preparation follows Apple's
[trust settings implementation](https://github.com/apple-oss-distributions/Security/blob/main/OSX/libsecurity_keychain/lib/TrustSettings.cpp),
whose root shortcut requires nonempty settings, and the
[authorization rules](https://github.com/apple-oss-distributions/Security/blob/main/OSX/authd/authorization.plist),
which otherwise require entitlement or interactive administrator authentication.
This explains why deleting the last trust entry can take a different path from
adding it. The revised fixture uses VM disposal for that final trust entry
instead of requiring interactive authorization or weakening host policy.

The native flow root now also leaves its last granted policy unchanged through
natural expiry while UDP traffic continues. After draining the five-second RPC
deadline, twelve seconds of fresh traffic must produce no report attempts.
Captured window timestamps may not exceed the consent boundary. A new explicit
grant must restore reporting, without restarting or re-enrolling the Client.
This covers expiry with the policy endpoint still reachable; loss of policy
connectivity and abrupt-crash spool recovery remain separate. The inventory is
still 29 roots / 696 outcomes; this extension awaits its own hosted evidence.

Run [34655479384](https://github.com/endless-net/client/actions/runs/34655479384)
completed with failure at source `add60a385d4a17a92d1f3ddb0d34f52cd0ae6d16`.
All 24 native job reports were inspected: identical 29-root inventories,
660 PASS, 36 FAIL, 0 SKIP. The flow-consent root failed in all 24 jobs;
TLS trust failed in all 12 Windows/macOS jobs and passed in all 12 Linux jobs.
The other 27 roots passed on every runner/repetition. All eight installation
jobs, three platform verification jobs and the separate Linux control-plane job
passed. This source remains unqualified; it predates the short-grant and
Windows machine-store corrections and cannot attest either correction.

The named P-256 CA run still times out at Windows current-user root import.
The fixture now uses the elevated disposable runner's machine root store for
both installation and exact-thumbprint removal. This follows the
[Windows machine-store model](https://learn.microsoft.com/en-us/windows-hardware/drivers/install/local-machine-and-current-user-certificate-stores).
macOS reaches native traffic in the flow case, but cleanup and subsequent TLS
setup time out; cleanup errors now identify the failing operation number.
Neither timeout is evidence of a Client TLS verification defect. Windows and
macOS trust lifecycle qualification remains pending.

The initial 29-root run [34655479384](https://github.com/endless-net/client/actions/runs/34655479384)
exposed a flow fixture mismatch: its two-minute grant was rejected by the
Client's one-minute future-expiry guard. The native scenario now uses a
50-second grant, covering the policy refresh and reporting/retry observation
budgets. This changes the fixture only; it does not remove the privacy guard or
qualify longer grants. The published protobuf does not state that maximum, so
long-grant behavior remains an explicit Client contract gap. The corrected
scenario still requires native evidence on every runner and repetition.

Reconcile the HC matrix with Client-owned consumer/OS coverage, then implement
missing contract-only testserver scenarios and client regressions. Historical
producer failures are constraints, not tasks to fix outside Client. Keep
contract gaps and platform decisions explicit; do not replace unresolved client
scenarios with generic smoke tests or infer completion from historical P/R runs.

Continue installed-client lifecycle coverage with interruption and artifact
replacement, explicit reset and complete-state removal. Same-artifact reinstall is HC-003 evidence; it must not be
reported as a completed version-upgrade or full-removal scenario for HC-060/HC-064.

Extend native platform coverage beyond direct IPv4/IPv6 TCP/UDP and ICMP echo over IPv4 underlay:
Explicit ICMP grants and errors/PMTU, IPv6 underlay, Relay/NAT, policy direction/destination variants and automatic OS
resolver behavior still require explicit client tests and runner evidence.
Do not infer those outcomes from component ACL tests or successful peer UDP
echoes. The real Client must continue to use its native OS interface and CLI/IPC.
