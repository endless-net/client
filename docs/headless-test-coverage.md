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
| HC-005 | C SingleAgentOwnership including lexical/file-symlink aliases, concurrent duplicate rejection and successor startup passed all 24 repetitions at `4f117a0`; concurrent startup without an existing owner, winner sync and cleanup passed all 24 repetitions at `1f8df62`; Unix SIGTERM requires clean exit | Hard-link aliases and remaining termination variants |
| HC-006 | L service restart without interactive login; connected cached-map TCP startup without control and later control recovery passed all eight installation runners at `0cf6d73` | Actual machine reboot and other late-network availability variants |
| HC-007 | C BrowserEnrollment | Client account binding and completion/error variants against the contract testserver |
| HC-008 | C BrowserEnrollment; expiry recovery on six runners; pending CLI interruption/resume and active rejection/reapproval passed all 24 repetitions on eight native runners | Interactive/server-side cancellation and foreign poll authorization |
| HC-009 | C enrollment; P wrong/expired join authorization; R two real CLI registrations | Full authorized/denied attribute and platform variants |
| HC-010 | C RegistrationResponseLoss; D ResponseLossPreservesOperation; historical R distinct node IDs | Client image-cloning and batch variants; historical producer replay failure is an external constraint |
| HC-011 | C ephemeral public IPC projection, native TCP access, abrupt process loss, terminal provider cleanup outcome and distinct replacement enrollment passed all 24 repetitions at `92983ae`; CLI join-token --ephemeral forwarding has a component test | Provider absence detection and asynchronous cleanup timing remain outside the Client boundary |
| HC-012 | C real Client rejects invalid and preserves corrected hostname, endpoint, advertised-prefix and requested-tag registration inputs through browser approval; all four recovery leaves passed every native repeat at `90f8a28`; D request/proof tests | Effective tag/route authorization and additional platform attributes against published responses |
| HC-013 | C BrowserEnrollment; R registered-node rejection and reapproval restore real traffic | Remaining pending/denial and browser completion variants |
| HC-014 | C user-session rotation, user-RPC denial, node-credential independence, reauthentication and same-node traffic/restart recovery passed all 24 repetitions at `92983ae`; U recovery matrix | Browser/OIDC refresh and other session-expiry timing variants |
| HC-015 | C join-token rotation denies a new real Client, preserves the existing node credential, signed-map advancement, native traffic and cached restart during a control outage, then restores control and permits replacement-token enrollment; passed all 24 repetitions at `92983ae`; historical R revoked join key | Token expiry and expired-cache variants; node revocation remains separate |
| HC-016 | C initial cached-map status and native direct IPv4/IPv6 TCP/UDP and ICMP echo on all eight runners | Other paths/protocols and denied-access variants |
| HC-017 | C native direct IPv4/IPv6 TCP/UDP and ICMP echo blocked/restored by disconnect/connect and agent restart on all eight runners with original identity; historical R direct TCP/UDP | Remaining established-flow, other paths and operation-failure variants |
| HC-018 | C connected/disconnected intent survives process restart with native IPv4/IPv6 TCP/UDP and ICMP echo checks on all eight runners; historical R traffic checks | Host reboot, crash during intent write and other platform/network variants |
| HC-019 | C durable route-table off/auto passed three times on all eight native platforms; MTU CLI validation/persistence, IPC/OS agreement and IPv4/IPv6 TCP/UDP checks passed all 24 repeats at `169b09c`; U configuration tests | Other public preferences and live mutation variants |
| HC-020 | C NetworkSelectionBoundary passed all 24 native repetitions: enrolled network listing/selection, disconnected response before/after restart and foreign-network rejection | Product decision for multiple saved profiles and switching; network-scoped selection is not profile support |
| HC-021 | C TLS trust/hostname, unchanged-key intent/traffic, connected/disconnected ordinary and interrupted map-signing rotation, and changed-key native traffic passed all 24 repetitions at qualified source `4f117a0`; expired/not-yet-valid TLS rejection before enrollment and after enrolled restart, followed by recovery, passed all 24 repeats at `90f8a28` | Remaining origin variants and existing-session certificate lifetime semantics |
| HC-022 | C Lifecycle logout; LocalForgetAfterUnconfirmedLogout passed three times on all eight runners | Remaining revocation retry, traffic-retirement and profile semantics |
| HC-023 | C Lifecycle peer projection; R approval changes applied by running agent and WireGuard | Remaining authorization and peer absence variants |
| HC-024 | C two-Client Linux direct traffic; native real Client IPv4/IPv6 TCP/UDP and ICMP echo on all eight runners (increments below); historical R two agents with real Coordinator | ICMP errors/PMTU, IPv6 underlay, Relay/NAT and full policy variants |
| HC-025 | C DNS CLI/proxy, wire lookup and application access by FQDN; UDP/TCP A/AAAA projection, withdrawal/restoration, split isolation, SERVFAIL recovery, IPv4/IPv6 listener/upstream and truncated-UDP TCP retry are executable; all eight wire variants, native system-resolver selection/recovery and signed DNS map updates passed all 24 repeats at `90f8a28` | Later DNS diagnostic-projection assertions await qualification; remaining DNS variants |
| HC-026 | C explicit default/split upstream selection and denied-domain isolation; U DNS/router configuration | System DNS control and IP-access preservation |
| HC-027 | C delta/resync and TCP grant withdrawal with retained UDP; native IPv4/IPv6 TCP/UDP port withdrawal/restoration, ICMP denial under TCP-only grants, ICMP-only TCP/UDP isolation and exact destination denial/recovery on all eight runners; historical R node/port withdrawal | Remaining Client direction/destination correlation, ICMP errors/PMTU and other policy/transport variants |
| HC-028 | C native single-Relay and two-Relay failover IPv4/IPv6 TCP/UDP, outage denial, recovery and agent restart passed all 24 native repetitions | NAT, direct/Relay transitions, existing-session failover, healthy-backup failback and remaining Relay variants |
| HC-029 | C native signed endpoint rotation requires stale-endpoint denial followed by IPv4/IPv6 TCP/UDP recovery; all four traffic roots passed every repeat at `90f8a28`; U endpoint/reconnect tests | External network change and additional stale response ordering variants |
| HC-030 | C typed/malformed errors; native cached-map signature expiry/outage/restart recovery for IPv4/IPv6 passed all 24 repeats at `90f8a28`; L connected cached-map TCP startup without control, later map advancement and disconnected no-auto-connect passed all eight installation runners at `0cf6d73`; historical R short dependency/process outages | Other lease expiry, edge/transport/storage outage and remaining variants |
| HC-031 | Central ACL tests are not evidence of a local inbound preference | No local inbound toggle found in current CLI/IPC/config; product and contract gap |
| HC-032 | C RoutedResource passed three times on all eight native platforms: IPv4/IPv6 TCP/UDP through an IP forwarding peer, route withdrawal and recovery | Full Client router roles, SNAT, HA and remaining resource variants |
| HC-033 | C durable route installation disable/restore passed three times on all eight native platforms; U configuration tests | Per-resource selection and remaining route-selection semantics |
| HC-034 | C route advertisement and platform boundary; Linux two-Client subnet forwarding with SNAT and source-preserving operator-configured forwarding, approval, withdrawal, router outage and recovery; common root passed all 24 repetitions at `92983ae`; U hook rendering | Non-Linux SNAT mode is explicitly unsupported; IPv6, independent policy and other router variants |
| HC-035 | No C/R evidence audited | Site-to-site scope and reverse-path tests |
| HC-036 | C native IPv4/IPv6 default-route selection, same-host reference egress hop, withdrawal and recovery passed all 24 repeats at `169b09c` | Remote exit-peer underlay and remote control connectivity while default routes are active are not proved; production public-address and DNS observation remain release acceptance |
| HC-037 | U exit-LAN rules | LAN allowed/denied with real exit traffic |
| HC-038 | C Linux Client IPv4 exit provider for another real Client: default-route advertisement/approval, TCP/UDP forwarding, SNAT-dependent return traffic, withdrawal and restart recovery; common root passed all 24 repetitions at `92983ae`, with explicit unsupported results outside Linux | IPv6 provider, no-SNAT, HA and production public-address observation |
| HC-039 | No C/R evidence audited | Role-specific HA semantics and failure recovery |
| HC-040 | C native signed application route, DNS, connector traffic, target isolation, withdrawal, expiry and recovery are executable; U application discovery/runtime | Qualify the native root on every runner; connector discovery report, DNS-address change, CNAME, multiple-IP and connector-outage variants |
| HC-041 | No C/R evidence audited | Overlap product decision and unambiguous target tests |
| HC-042 | No C/R evidence audited | SSH feature scope, authority and commands contract |
| HC-043 | No C/R evidence audited | Forwarding scope and allowed/denied listener tests |
| HC-044 | No C/R evidence audited | File-transfer scope, integrity and interrupted transfer |
| HC-045 | No C/R evidence audited | Personal-device transfer decision and recipient restriction |
| HC-046 | No C/R evidence audited | Private publication contract and external denial |
| HC-047 | No C/R evidence audited | Public exposure contract, authorization and external reachability |
| HC-048 | No C/R evidence audited | Audience restrictions, lifetime, shutdown and crash expiry |
| HC-049 | No C/R evidence audited | Certificate/publication scope and frontend/backend TLS |
| HC-050 | C signed cross-network machine grant with exact TCP scope, expiry, renewal, live TCP/UDP rights replacement and withdrawal passed all 24 repetitions at `90f8a28`; U two-engine direct/Relay direction and key-rotation tests | New recipient identity and real source-Client variants |
| HC-051 | C signed two-host service DNS, actual host traffic, target isolation, host-set removal, approval loss and recovery passed all 24 repetitions at `92983ae`; U service discovery/runtime | Health, load distribution and connection-draining semantics require product decisions |
| HC-052 | L/C bounded IPC waits; independent event subscriptions/cancellation/restart and real CLI listening-timeout exit passed all 24 repetitions at qualified `38050bc`; installed absent-service failure passed all eight runners | Remaining public readiness conditions, slow consumers and stalled native IPC |
| HC-053 | L/C structured IPC; request validation, non-object rejection, body-size boundaries, subscribers and real CLI NDJSON events passed all 24 repetitions at qualified `38050bc`; D RPC authorization | Remaining machine output/errors and local-user authorization variants |
| HC-054 | L/C Ubuntu container persistent state restart with native IPv4/IPv6 TCP/UDP and ephemeral retirement/recreation with IPv4 TCP passed at `169b09c` | Host/sidecar split, reduced capabilities, container network modes and orchestration lifecycle |
| HC-055 | No C/R evidence audited | Userspace/no-TUN product scope and application proxy behavior |
| HC-056 | C public diagnostics export with control-outage isolation and recovery passed all 24 repetitions at `92983ae`; U path diagnostics | Distinguish peer path, DNS and application failures through public commands |
| HC-057 | C export/reuse, agent-crash recovery, retention and IPv4 UDP flow consent/retry/expiry; D FlowConsentAndIdempotency; U flow tests | Qualify expanded diagnostics root; comprehensive redaction and remaining retention/flow variants |
| HC-058 | L version and runtime platform; native IPC negotiation/restart passed all 24 repetitions at `a9a1a21` | Artifact mismatch and exact release identity |
| HC-059 | No qualified independent client-endorsed node-admission evidence; server trust/recovery tests belong to HC-021 | BR-05 / Q-05 product decision: independently signed node admission, unsigned-node denial, signing-device loss and recovery |
| HC-060 | L/C two-version native package/service upgrade preserves the enrolled identity, connected intent and real traffic; passed all eight installation runners at `92983ae` | Incompatible-state rollback and insufficient space/privileges |
| HC-061 | Publication gate, fixture tests and real GitHub API check | Supported update channels, artifact acceptance and update failures |
| HC-062 | L missing-executable repair preserves identity and connected/disconnected traffic intent on all eight installation runners at `0cf6d73` | Other installation damage and interrupted-repair variants; full identity reset is separate |
| HC-063 | L/C explicit native state removal, fresh `NeedsEnrollment`, and reenrollment with a different node identity passed all eight installation runners at `92983ae`; local forget deliberately retains identity | Standalone reset without uninstall and provider-side removal remain separate |
| HC-064 | L native uninstall checks service and TUN removal, ordinary enrolled-state retention, and explicit state removal; passed all eight installation runners at `92983ae` | Windows/macOS distribution-owned binary removal remains outside the core service uninstaller |
| HC-065 | C terminal revoke, native direct IPv4/IPv6 TCP/UDP retirement and ICMP echo denial across agent restart on all eight runners; Linux direct TCP/UDP; historical R deleted Client sync denied and peer withdrawn | Offline leases and remaining path/platform variants, separately verified local removal |

## Windows evidence and complete-suite budget — 2026-09-12

At source `f71587a`, [Windows 2025 repeat 2](https://github.com/endless-net/client/actions/runs/34710382160/job/103600590944)
fails the initial IPv4 application-map wait. Public stage logs show TUN creation
and device activation completed, followed by `routes begin` without completion
at inspection. This locates the delay in route configuration; it does not yet
identify the slow PowerShell/OS operation. The 15-second readiness limit remains.
The same report has an IPv6 TCP flow-consent probe failure from 2.857 seconds
before consent expiry until 1.844 seconds before expiry, after 1290 successful
probes. Reference traffic counters do not advance for that exchange. Public
inspection reports a 99.297-second handshake age, valid cache, healthy WireGuard
and available physical IPv4 interfaces before cleanup. Cause remains open.

Failed flow probes now also report reference-peer wire-counter deltas over
that individual exchange: received initiations, attempted/sent/failed responses,
and other received datagrams. These are observations at the independent peer's
UDP boundary, with no Client private state or payload logging. An increase in
other datagrams alone does not establish successful decryption or application
delivery. This diagnostic addition preserves the traffic assertion and all
probe deadlines; its native execution still requires CI evidence.

[Windows 2025 repeat 3](https://github.com/endless-net/client/actions/runs/34710382160/job/103600590934)
also fails the DNS diagnostic assertion and reaches the overall 30-minute Go
test deadline. Its 52 completed roots total 1794.25 seconds; the final
`TrustConfirmation` root has run for only six seconds at cancellation. This
report does not establish a failure in trust confirmation and is incomplete.
The complete 53-root suite now has a 40-minute Go deadline, a 41-minute execution
step limit and a 55-minute job limit for setup and report preservation. These
are suite execution budgets; CLI, IPC, traffic, readiness and lease deadlines
are unchanged, as are all required tests and the eight-platform/three-repeat
matrix. The revised budget still requires CI validation.

## DNS diagnostic revision synchronization — 2026-09-12

In the first 16 downloaded reports of [run 34710382160](https://github.com/endless-net/client/actions/runs/34710382160),
source `f71587a8583b0d5eff1a005338c5e54809bc05e0`, fourteen roots sets pass
all 53 tests. Windows 2022 repeat 1 and macOS ARM repeat 3 each fail only the
new `NativeDNSMapUpdates` diagnostic assertion (52 other roots pass). The
combined assertion did not report which field differed, so these failures
alone do not establish the cause.

Source inspection found two synchronization flaws in the test: waiting for any
revision newer than the previous observation could accept an intervening
endpoint update, and requiring identical revisions from consecutive status and
diagnostics requests could reject a legitimate newer map. The test now waits
for at least the signed snapshot revision containing the DNS change, and
diagnostics must describe that revision or a later one. Exact peer DNS names,
addresses, withdrawal, record counts and real wire-query assertions remain.
Failure output now reports revision numbers and presence/match flags only.
This correction needs its own native qualification; neither failure is marked
resolved by source inspection or local checks alone.

## Complete corrected map-expiry matrix — 2026-09-12

[Run 34708283604](https://github.com/endless-net/client/actions/runs/34708283604)
completed with a failed aggregate gate at source
`90f8a2860b55b08466842a51bbe3fdcf207ab521`. All 24 native reports are present
for the eight platform variants with three repeats. Each identifies this source,
contains all 52 expected root results and a terminal package result. The native
total is **1199 PASS, 49 FAIL, 0 SKIP**; these are repeated test-root outcomes,
not a completion percentage for the 65 HC scenarios.

`NativeCachedMapExpiry` passes all 24 times, with 48 passing IPv4/IPv6 leaves.
This qualifies the signed-map expiry packet-filter fix for the tested outage,
restart, traffic-denial and fresh-map recovery sequence on all eight native
variants. Other credential lifetimes and route-retirement requirements remain
separate. Native flow consent and exit-route roots also pass all 24 repeats;
the earlier intermittent flow failures have not been explained by this result.

There are 24 failures each in `JoinTokenRotation` and `SessionExpiryRecovery`
from the wrong reference counter, corrected later in `6cff19b`. Their expanded
lifecycle coverage still needs its own successful native run. The remaining
failure is `NativeApplicationRoute/ipv4` on Windows 2022 repeat 1, detailed
below. Forty-nine of the 52 roots pass every repeat; the application root passes
23. Neither the counter correction nor the later startup-stage diagnostics is
qualified by this source.

All eight installation jobs, the container lifecycle job, control-plane job and
Linux/macOS/Windows verification jobs succeeded. External STUN compatibility
was skipped and is not claimed as verified. The aggregate `verify` job failed
([job 103600518503](https://github.com/endless-net/client/actions/runs/34708283604/job/103600518503)).

## First corrected map-expiry native evidence — 2026-09-12

The first six downloaded native reports from [run 34708283604](https://github.com/endless-net/client/actions/runs/34708283604)
identify source `90f8a2860b55b08466842a51bbe3fdcf207ab521`: Ubuntu 24.04
amd64 and Ubuntu 22.04 arm64, each with repetitions 1–3. Every report has
50 passing and two failing root results. `NativeCachedMapExpiry` passes in
both IPv4 and IPv6 in all six reports. This is the first native confirmation
of the packet-filter expiry fix, limited to these two platform variants;
the remaining platform reports and the complete matrix are still pending.

The two failing roots are `JoinTokenRotation` and `SessionExpiryRecovery`.
Both TCP and UDP probes succeeded before the new counter assertion failed.
The tests incorrectly queried `ForwardedPacketCounts` on `NewTCP`, which has
no router/resource link and therefore returns zero from that accessor.
They now use the reference echo endpoint's `PacketCounts`, independently
requiring fresh received and echoed counts for each successful TCP/UDP probe.
The lifecycle assertions and native traffic requirements remain in place.
This test correction still requires native qualification on its own source.

### Windows startup recurrence in the same run

All three Windows 2022 reports from source `90f8a28` pass both native
cached-map expiry families. Repetitions 2 and 3 fail only the two counter
assertions described above. Repetition 1 also fails
`TestControlPlaneNativeApplicationRoute/ipv4` in the initial map-application
wait ([job 103593386182](https://github.com/endless-net/client/actions/runs/34708283604/job/103593386182)).
The agent remains alive and IPC answers 599 times without transport errors
during the 15-second wait. Every WireGuard inspection is classified as
`operation-in-progress`; the last public state has revision 1, zero peers,
valid cache and credentials, and no agent snapshot. The IPv6 subtest passes.

This repeats the earlier Windows startup symptom, now with evidence that
inspection was unavailable because an operation held the WireGuard lock.
It does not identify which native startup operation was delayed or why.
The timeout has not been relaxed; Client startup diagnosis remains open.

The next diagnostic increment emits fixed begin/complete messages around TUN
creation, device activation and route configuration. On a failed readiness
wait, the harness uses the published `service logs-recent` command with a
separate bounded request and includes only those exact messages in the report.
Unknown messages, including a known message with added context, are withheld.
No network parameters or credentials are included in the stage messages. This
adds evidence for locating a future recurrence; it is not a startup fix and
does not extend the readiness deadline. Native qualification is pending.

## Diagnostic export storage failure increment — 2026-09-12

`TestControlPlaneDiagnosticsExport` now compares the public exported bytes before
and after crash reuse. It also temporarily replaces only its configured public
export directory with an operator-owned regular file, requires a CLI failure
diagnostic, checks that public diagnostics still report the original connected
identity and valid cache, and verifies that the obstruction remains unchanged.
After restoring the directory, the same running agent must successfully reuse
the retained artifact with identical bytes. This adds HC-056/HC-057 export-path
failure isolation and recovery without reading private Client state.

The existing mandatory native root runs in the eight-platform, three-repeat
Client CI matrix. This extension has passed local short tests, vet and lint;
native execution is pending. It does not establish disk-full behavior, arbitrary
filesystem failures, or application traffic continuity during export failure.

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

## CLI event deadlines and split-DNS recovery qualified

Run [34664269724](https://github.com/endless-net/client/actions/runs/34664269724)
completed successfully for source `38050bc60c608d69eeed399f9e86172d270bbd0c`.
All 24 native reports contain the same 33 scenarios: **792 PASS, zero FAIL,
zero SKIP**. The complete platform/repetition evidence is:

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| macos-15 | [103473575844](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575844) | [103473575884](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575884) | [103473575970](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575970) |
| macos-15-intel | [103473575948](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575948) | [103473575857](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575857) | [103473575911](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575911) |
| ubuntu-22.04 | [103473575790](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575790) | [103473575768](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575768) | [103473575766](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575766) |
| ubuntu-22.04-arm | [103473575838](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575838) | [103473575843](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575843) | [103473575810](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575810) |
| ubuntu-24.04 | [103473575759](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575759) | [103473575772](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575772) | [103473575791](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575791) |
| ubuntu-24.04-arm | [103473575822](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575822) | [103473575848](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575848) | [103473575871](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575871) |
| windows-2022 | [103473575819](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575819) | [103473575874](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575874) | [103473575937](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575937) |
| windows-2025 | [103473575845](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575845) | [103473575902](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575902) | [103473575833](https://github.com/endless-net/client/actions/runs/34664269724/job/103473575833) |

The [aggregate gate](https://github.com/endless-net/client/actions/runs/34664269724/job/103476199341)
passed dependency and same-source artifact validation, explicitly reporting
33 common scenarios across three repetitions and eight platforms. All eight
installation jobs, three OS verification jobs and the separate Linux control-plane
job passed. Optional external STUN compatibility was skipped and is not counted.
This completes the source gate missing from the preceding installation snapshot.

The increment confirms real CLI event subscriptions while connected and after
a disconnected restart: complete NDJSON records, hello/current status, monotonic
sequence, negotiated metadata, successful listening-timeout exit and retained
intent. It also confirms split-upstream SERVFAIL propagation, isolation from the
healthy global resolver, preserved local/public DNS and same-process recovery
on all eight platforms. The CLI missing-hello fix has its short regression and
installed absent-service coverage; stalled native transports remain a separate
boundary, not an implication of healthy-stream timeout success.

This source predates IPv6 DNS upstream/listener combinations, truncated UDP to
TCP retry and nobody-account Unix permissions tests. Those remain pending their
own source matrix. Full HC-001–HC-065 coverage is still incomplete.

## Unix IPC permissions qualified; native UDP mismatch under investigation

Source `c36c0db57b4d814053ac261f466c36e2bfcbe73b` in run
[34665159849](https://github.com/endless-net/client/actions/runs/34665159849)
passed all six Linux/macOS installation reports. Each contains PASS for the root
and all four installation subcases, without failure or skip. The enrolled-reinstall
subcase executes the nobody-account checks in both connection states.

| Runner | Installation report |
| --- | --- |
| macos-15-intel | [103476274292](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274292) |
| macos-15 | [103476274381](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274381) |
| ubuntu-22.04-arm | [103476274207](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274207) |
| ubuntu-22.04 | [103476274277](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274277) |
| ubuntu-24.04-arm | [103476274185](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274185) |
| ubuntu-24.04 | [103476274265](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274265) |

The CLI executes with a verified non-root UID, then status/connect/disconnect
and confirmed local-forget fail with Unix IPC permission denial. Privileged
status and real TCP probes verify retained identity and connected/disconnected
behavior afterward. This qualifies protected Unix transport denial, not
observer/owner/admin roles after IPC access. Windows restricted-token tests
were added after this source and have no evidence in this run.

The [Windows 2025 third repetition](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274315)
failed only `TestControlPlaneNativeUDPTraffic`: after reconnecting with retained
identity, a fresh application probe reported `application response mismatch`.
Its other 32 roots and all eight DNS leaf variants passed. The mismatch is a
received UDP payload that did not match a known outstanding probe nonce;
packet corruption versus delayed unrelated traffic is not established. This
failure is not classified as permitted traffic, denied traffic or an expected
skip. No speculative runtime fix or relaxed probe assertion is justified by
this log alone. The remaining matrix was still running when this evidence was
recorded; this source has no successful publication qualification.

## Complete DNS-family matrix: two Windows failures

Run [34665159849](https://github.com/endless-net/client/actions/runs/34665159849)
completed with failure for source `c36c0db57b4d814053ac261f466c36e2bfcbe73b`.
All 24 reports contain the same 33 roots: **790 PASS, 2 FAIL, zero SKIP**.

| Runner | Repeat 1 | Repeat 2 | Repeat 3 |
| --- | --- | --- | --- |
| macos-15 | [103476274434](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274434) | [103476274296](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274296) | [103476274285](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274285) |
| macos-15-intel | [103476274293](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274293) | [103476274324](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274324) | [103476274333](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274333) |
| ubuntu-22.04 | [103476274288](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274288) | [103476274245](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274245) | [103476274279](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274279) |
| ubuntu-22.04-arm | [103476274271](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274271) | [103476274274](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274274) | [103476274343](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274343) |
| ubuntu-24.04 | [103476274312](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274312) | [103476274326](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274326) | [103476274241](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274241) |
| ubuntu-24.04-arm | [103476274295](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274295) | [103476274273](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274273) | [103476274357](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274357) |
| windows-2022 | [103476274395](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274395) | [103476274268](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274268) | [103476274287](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274287) |
| windows-2025 | [103476274338](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274338) — FAIL | [103476274398](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274398) | [103476274315](https://github.com/endless-net/client/actions/runs/34665159849/job/103476274315) — FAIL |

Windows 2025 repeat 1 failed DiagnosticsExport because the real CLI disconnect
exceeded its default 30-second timeout (30.018 seconds observed). Repeat 3 failed
NativeUDPTraffic after reconnect with an application response mismatch. These
are distinct unresolved failures. The existing log does not identify whether
disconnect waited on control-plane notification, engine locking, or OS teardown.
Neither increasing its timeout nor relaxing nonce comparison is established as
a correction. Later bounded nonce diagnostics do not retroactively identify
this run's cause.

All **192 DNS leaf outcomes passed**: eight listener/upstream/truncation variants
on each of 24 runners. This confirms IPv4/IPv6 UDP/TCP listener access, independent
IPv4/IPv6 upstreams, TCP retry after truncated UDP, local A/AAAA map changes,
split isolation and upstream SERVFAIL recovery for this source's DNS scenario.
It does not override the two other failures. All eight installation jobs and
three OS verification jobs passed, including the six Unix permission checks
recorded above. The [aggregate gate](https://github.com/endless-net/client/actions/runs/34665159849/job/103478839514)
failed; the source is not qualified for publication. Optional external STUN was
skipped and supplies no evidence. Windows restricted-token tests and bounded
probe-mismatch counters were introduced after this source.

## Windows restricted-token installation evidence — 2026-09-12

Source `08d5186596527f6ce8a0f0064b75431a07126f69` in
[run 34666246762](https://github.com/endless-net/client/actions/runs/34666246762)
passed both Windows installation jobs:

| Platform | Job | Installed root | Administrator denials |
| --- | --- | --- | --- |
| Windows 2022 x64 | [103478854514](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854514) | PASS, 47.39s | Explicit IPC authorization, connected and disconnected |
| Windows 2025 x64 | [103478854524](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854524) | PASS, 53.00s | Explicit IPC authorization, connected and disconnected |

Each job passed fresh installation, disconnected service restart, enrolled
reinstallation and uninstall. The restricted CLI executed `version` successfully
and administrator-only `local-forget` was rejected by IPC authorization in both
connection states. These were application authorization denials, not named-pipe
transport denials. Subsequent assertions retained the original identity and
connection intent and verified the corresponding real TCP behavior.

This proves the tested restricted-token administrative boundary on these two
hosted Windows images. It does not cover another account SID, observer/owner
roles, reboot, or upgrade between different artifacts. At this snapshot the
contract matrix is incomplete; these installation results do not qualify the
whole source or resolve the previous Windows UDP mismatch and disconnect timeout.
The later post-failure public status observation is not part of this source.

## Complete matrix with restricted Windows tokens — 2026-09-12

[Run 34666246762](https://github.com/endless-net/client/actions/runs/34666246762)
completed with failure for source `08d5186596527f6ce8a0f0064b75431a07126f69`.
All 24 contract reports contain the same 33 root scenarios, exactly once each:
**790 PASS, 2 FAIL, 0 SKIP**.

| Platform | Repetition 1 | Repetition 2 | Repetition 3 |
| --- | --- | --- | --- |
| macos-15 | [103478854546](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854546) | [103478854627](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854627) | [103478854679](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854679) |
| macos-15-intel | [103478854621](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854621) | [103478854554](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854554) | [103478854543](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854543) |
| ubuntu-22.04 | [103478854563](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854563) | [103478854500](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854500) | [103478854590](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854590) |
| ubuntu-22.04-arm | [103478854556](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854556) | [103478854613](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854613) | [103478854609](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854609) |
| ubuntu-24.04 | [103478854611](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854611) | [103478854447](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854447) | [103478854596](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854596) |
| ubuntu-24.04-arm | [103478854603](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854603) | [103478854549](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854549) | [103478854568](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854568) |
| windows-2022 | [103478854527](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854527) | [103478854599](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854599) | [103478854571](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854571) |
| windows-2025 | [103478854598](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854598) | [103478854612](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854612) | [103478854644](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854644) |

Windows 2022 repetition 1 failed `TestControlPlaneIPCEvents` before the first
SDK subscription's hello; the underlying Stream error was not retained by that
source. Windows 2025 repetition 1 failed `TestControlPlaneDNSWireRecovery`:
the udp4/::1/truncated-udp and udp6/::1/truncated-udp variants could not bind the
TCP fixture on the UDP-selected port. The DNS leaf totals are 190 PASS and 2 FAIL.
Both setup failures remain failed evidence, not Client DNS success or skips.

All eight installation jobs, three OS verification jobs and the separate
control-plane job passed. The [aggregate gate](https://github.com/endless-net/client/actions/runs/34666246762/job/103481499793)
failed; this source is not qualified for publication. The optional external STUN
job was skipped and supplies no evidence.

The earlier native UDP mismatch and disconnect timeout did not recur in these
24 reports. Their absence does not establish a cause or a fix. This source
predates the TLS-lifetime variants, TCP-first DNS port allocation, IPC termination
categories, eight simultaneous subscriptions, OS-thread pinning and post-failure
public status observation. Those increments require their own exact-source CI.

## Qualified TLS lifetime, DNS families and IPC concurrency — 2026-09-12

[Run 34667224598](https://github.com/endless-net/client/actions/runs/34667224598)
completed successfully for source `ef19a9fb1e516b2ddb96b250888006291dd6c0af`.
All 24 reports contain the same 33 root scenarios, exactly once each:
**792 PASS, 0 FAIL, 0 SKIP**. The
[aggregate gate](https://github.com/endless-net/client/actions/runs/34667224598/job/103484180731)
verified the complete source-bound report set.

| Platform | Repetition 1 | Repetition 2 | Repetition 3 |
| --- | --- | --- | --- |
| macos-15 | [103481518360](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518360) | [103481518346](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518346) | [103481518381](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518381) |
| macos-15-intel | [103481518369](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518369) | [103481518330](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518330) | [103481518374](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518374) |
| ubuntu-22.04 | [103481518320](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518320) | [103481518583](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518583) | [103481518463](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518463) |
| ubuntu-22.04-arm | [103481518525](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518525) | [103481518547](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518547) | [103481518387](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518387) |
| ubuntu-24.04 | [103481518546](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518546) | [103481518584](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518584) | [103481518313](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518313) |
| ubuntu-24.04-arm | [103481518575](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518575) | [103481518266](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518266) | [103481518617](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518617) |
| windows-2022 | [103481518332](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518332) | [103481518328](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518328) | [103481518358](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518358) |
| windows-2025 | [103481518305](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518305) | [103481518314](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518314) | [103481518353](https://github.com/endless-net/client/actions/runs/34667224598/job/103481518353) |

All 48 TLS lifetime leaves passed: expired and not-yet-valid certificates under
the trusted CA were rejected before HTTP, and valid-certificate recovery and
identity-preserving restart passed in every parent scenario. All 192 DNS leaves
passed, covering independent IPv4/IPv6 upstream and listener families, UDP/TCP
queries, truncated-UDP TCP retry, split isolation and SERVFAIL recovery.

The IPC-events root passed all 24 repetitions with eight simultaneous native
subscriptions, required hello/status, individual cancellation, CLI event output,
intent mutations and restart. This source includes Windows OS-thread pinning
during peer impersonation and TCP-first allocation for DNS fixtures. The earlier
IPC failure did not recur; this is qualification of the corrected source and its
tests, not proof that thread migration caused that historical incident.

All eight installation jobs, three OS verification jobs and the separate
control-plane job passed. Optional external STUN was skipped and supplies no
evidence. The earlier UDP nonce mismatch and disconnect timeout also did not
recur; their precise causes remain unresolved.

This run predates the native CLI fault/recovery root, CLI first-event ordering
fix, logout retry and native logout/local-forget traffic roots. Current source
has 36 roots and still requires its own 864-outcome matrix. This successful
33-root run does not complete HC-001–HC-065 or qualify those later increments.

## Native matrix evidence: cleanup and CLI fault boundaries

[Run 34668100780](https://github.com/endless-net/client/actions/runs/34668100780)
completed against source `8a0d75cf3294db832828ebdd4c7da0c02509a032`.
All 24 job logs contain the same 36 root scenarios: **863 PASS, 1 FAIL, 0 SKIP**.
The four CLI IPC fault/recovery leaves passed in every job (96 PASS).
Native logout/local-forget leaves produced 191 PASS and 1 FAIL across the
192 family/transport/cleanup variants. The failure is the initial fresh UDP
traffic baseline on Ubuntu 22.04 ARM repetition 2, before cleanup; details below.
The [aggregate gate](https://github.com/endless-net/client/actions/runs/34668100780/job/103487094061)
failed. All eight installation jobs, three OS verification jobs and the separate
Client control-plane job passed. Optional external STUN was skipped and supplies
no compatibility evidence.

| Runner and repetition | Job | Root outcomes |
| --- | --- | --- |
| macos-15-intel, repeat 1 | [103484236357](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236357) | 36 PASS |
| macos-15-intel, repeat 2 | [103484236551](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236551) | 36 PASS |
| macos-15-intel, repeat 3 | [103484236549](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236549) | 36 PASS |
| macos-15, repeat 1 | [103484236522](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236522) | 36 PASS |
| macos-15, repeat 2 | [103484236592](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236592) | 36 PASS |
| macos-15, repeat 3 | [103484236572](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236572) | 36 PASS |
| ubuntu-22.04-arm, repeat 1 | [103484236358](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236358) | 36 PASS |
| ubuntu-22.04-arm, repeat 2 | [103484236542](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236542) | 35 PASS, 1 FAIL |
| ubuntu-22.04-arm, repeat 3 | [103484236559](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236559) | 36 PASS |
| ubuntu-22.04, repeat 1 | [103484236450](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236450) | 36 PASS |
| ubuntu-22.04, repeat 2 | [103484236382](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236382) | 36 PASS |
| ubuntu-22.04, repeat 3 | [103484236577](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236577) | 36 PASS |
| ubuntu-24.04-arm, repeat 1 | [103484236378](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236378) | 36 PASS |
| ubuntu-24.04-arm, repeat 2 | [103484236537](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236537) | 36 PASS |
| ubuntu-24.04-arm, repeat 3 | [103484236629](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236629) | 36 PASS |
| ubuntu-24.04, repeat 1 | [103484236397](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236397) | 36 PASS |
| ubuntu-24.04, repeat 2 | [103484236394](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236394) | 36 PASS |
| ubuntu-24.04, repeat 3 | [103484236474](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236474) | 36 PASS |
| windows-2022, repeat 1 | [103484236346](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236346) | 36 PASS |
| windows-2022, repeat 2 | [103484236439](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236439) | 36 PASS |
| windows-2022, repeat 3 | [103484236591](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236591) | 36 PASS |
| windows-2025, repeat 1 | [103484236319](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236319) | 36 PASS |
| windows-2025, repeat 2 | [103484236540](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236540) | 36 PASS |
| windows-2025, repeat 3 | [103484236530](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236530) | 36 PASS |

This run does not qualify the later 37th interrupted-disconnect root, four-way
flow-consent expansion, direct IPC cleanup confirmation, exact build-commit
assertions, stricter child-outcome verifier, or UDP completed-reply history.
Those changes require evidence from their own source revision. Full HC-001–HC-065
coverage remains incomplete; this matrix does not close missing scenarios.

## Complete native evidence: 37-root revision

[Run 34668955037](https://github.com/endless-net/client/actions/runs/34668955037)
completed on 2026-09-12 for source `4a8011f90d839e6771d05999933a7738bc9a315f`.
All 24 logs contain the same 37-root inventory: **886 PASS, 2 FAIL, 0 SKIP**.
The four flow-consent variants total **94 PASS, 2 FAIL**. Both failures are
IPv6 UDP application unavailability after consent on Ubuntu 24.04 repetition 3
and Ubuntu 22.04 ARM repetition 3; their stage, timestamps and root-cause limits
are recorded below. No other root or child outcome failed or skipped.

The [aggregate gate](https://github.com/endless-net/client/actions/runs/34668955037/job/103491756047)
failed. All eight installation jobs, three OS verification jobs and the separate
Client control-plane job passed. Optional external STUN was skipped and is not
compatibility evidence. The table is the final result; earlier partial snapshots
below retain their historical observation limits.

| Runner and repetition | Job | Root outcomes |
| --- | --- | --- |
| macos-15-intel, repeat 1 | [103487109024](https://github.com/endless-net/client/actions/runs/34668955037/job/103487109024) | 37 PASS |
| macos-15-intel, repeat 2 | [103487109003](https://github.com/endless-net/client/actions/runs/34668955037/job/103487109003) | 37 PASS |
| macos-15-intel, repeat 3 | [103487109054](https://github.com/endless-net/client/actions/runs/34668955037/job/103487109054) | 37 PASS |
| macos-15, repeat 1 | [103487108968](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108968) | 37 PASS |
| macos-15, repeat 2 | [103487108921](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108921) | 37 PASS |
| macos-15, repeat 3 | [103487108993](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108993) | 37 PASS |
| ubuntu-22.04-arm, repeat 1 | [103487108913](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108913) | 37 PASS |
| ubuntu-22.04-arm, repeat 2 | [103487108981](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108981) | 37 PASS |
| ubuntu-22.04-arm, repeat 3 | [103487108946](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108946) | 36 PASS, 1 FAIL |
| ubuntu-22.04, repeat 1 | [103487108920](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108920) | 37 PASS |
| ubuntu-22.04, repeat 2 | [103487108911](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108911) | 37 PASS |
| ubuntu-22.04, repeat 3 | [103487108973](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108973) | 37 PASS |
| ubuntu-24.04-arm, repeat 1 | [103487108942](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108942) | 37 PASS |
| ubuntu-24.04-arm, repeat 2 | [103487108935](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108935) | 37 PASS |
| ubuntu-24.04-arm, repeat 3 | [103487108937](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108937) | 37 PASS |
| ubuntu-24.04, repeat 1 | [103487108959](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108959) | 37 PASS |
| ubuntu-24.04, repeat 2 | [103487108983](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108983) | 37 PASS |
| ubuntu-24.04, repeat 3 | [103487108890](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108890) | 36 PASS, 1 FAIL |
| windows-2022, repeat 1 | [103487108994](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108994) | 37 PASS |
| windows-2022, repeat 2 | [103487109098](https://github.com/endless-net/client/actions/runs/34668955037/job/103487109098) | 37 PASS |
| windows-2022, repeat 3 | [103487109002](https://github.com/endless-net/client/actions/runs/34668955037/job/103487109002) | 37 PASS |
| windows-2025, repeat 1 | [103487108918](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108918) | 37 PASS |
| windows-2025, repeat 2 | [103487108991](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108991) | 37 PASS |
| windows-2025, repeat 3 | [103487109015](https://github.com/endless-net/client/actions/runs/34668955037/job/103487109015) | 37 PASS |

Interrupted disconnect followed by restart, direct IPC cleanup-confirmation
validation, and CLI/IPC assertions of the exact build commit passed in all
24 repetitions across all eight runners. The four-way flow-consent expansion
remains unqualified because of the two failures. No claim of complete
HC-001–HC-065 coverage follows from the other successful roots.

This source predates the completed-UDP-reply history, forced-termination ownership
variants, retained Relay sessions, native control-outage traffic, strict denial
oracle, DNS response binding, route advertisement and early registration-input
validation. Those changes require a separate source-specific matrix.

## Partial native evidence: 37-root revision

Snapshot collected on 2026-09-12 for run `34668955037`, source
`4a8011f90d839e6771d05999933a7738bc9a315f`: fourteen completed contract
job logs have an identical 37-root inventory, **516 PASS, 2 FAIL, 0 SKIP**.
Ten remaining contract jobs were still running at this snapshot; there is no
complete eight-platform qualification for this source.

The four flow-consent leaves total **54 PASS and 2 FAIL**. Both failures are
IPv6 UDP application unavailability after consent; root-cause limits are below.
All fourteen completed jobs passed interrupted disconnect/restart and IPC
negotiation with exact source-commit assertions. This supplies partial Linux
and macOS evidence, not Windows evidence for those additions. The snapshot
predates the later forced-termination ownership, retained Relay sessions,
control-outage traffic, strict probe-denial oracle and DNS response-binding work.

| Runner and repetition | Job | Root outcomes |
| --- | --- | --- |
| macos-15-intel, repeat 1 | [103487109024](https://github.com/endless-net/client/actions/runs/34668955037/job/103487109024) | 37 PASS |
| macos-15, repeat 1 | [103487108968](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108968) | 37 PASS |
| macos-15, repeat 2 | [103487108921](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108921) | 37 PASS |
| ubuntu-22.04-arm, repeat 1 | [103487108913](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108913) | 37 PASS |
| ubuntu-22.04-arm, repeat 2 | [103487108981](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108981) | 37 PASS |
| ubuntu-22.04-arm, repeat 3 | [103487108946](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108946) | 36 PASS, 1 FAIL |
| ubuntu-22.04, repeat 1 | [103487108920](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108920) | 37 PASS |
| ubuntu-22.04, repeat 2 | [103487108911](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108911) | 37 PASS |
| ubuntu-22.04, repeat 3 | [103487108973](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108973) | 37 PASS |
| ubuntu-24.04-arm, repeat 1 | [103487108942](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108942) | 37 PASS |
| ubuntu-24.04-arm, repeat 2 | [103487108935](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108935) | 37 PASS |
| ubuntu-24.04-arm, repeat 3 | [103487108937](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108937) | 37 PASS |
| ubuntu-24.04, repeat 1 | [103487108959](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108959) | 37 PASS |
| ubuntu-24.04, repeat 3 | [103487108890](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108890) | 36 PASS, 1 FAIL |

## Partial 39-scenario matrix: Relay session recovery on Linux

Run `34670764847`, source `985763edf13e74fc479abb293b26420cc653e6f3`,
was still running at this snapshot on 2026-09-12. Eleven completed contract jobs
reported the same 39 root scenarios: **409 PASS, 20 FAIL, 0 SKIP**.
Thirteen contract jobs remained unfinished; this is not a final matrix result.

| Platform / repetition | Job | Root outcomes |
| --- | --- | --- |
| macos-15-intel, repeat 3 | [103491773697](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773697) | 39 PASS |
| ubuntu-24.04-arm, repeat 3 | [103491773761](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773761) | 37 PASS, 2 FAIL |
| ubuntu-22.04-arm, repeat 3 | [103491773787](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773787) | 37 PASS, 2 FAIL |
| ubuntu-22.04, repeat 3 | [103491773808](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773808) | 37 PASS, 2 FAIL |
| ubuntu-24.04, repeat 3 | [103491773818](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773818) | 37 PASS, 2 FAIL |
| ubuntu-24.04, repeat 1 | [103491773822](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773822) | 37 PASS, 2 FAIL |
| ubuntu-24.04-arm, repeat 1 | [103491773837](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773837) | 37 PASS, 2 FAIL |
| ubuntu-24.04, repeat 2 | [103491773854](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773854) | 37 PASS, 2 FAIL |
| ubuntu-22.04, repeat 2 | [103491773875](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773875) | 37 PASS, 2 FAIL |
| ubuntu-22.04, repeat 1 | [103491773880](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773880) | 37 PASS, 2 FAIL |
| ubuntu-24.04-arm, repeat 2 | [103491774023](https://github.com/endless-net/client/actions/runs/34670764847/job/103491774023) | 37 PASS, 2 FAIL |

All ten completed Linux jobs failed both `NativeRelayTraffic` and
`NativeRelayFailover`, with IPv4 and IPv6 children failing. No other root or
child failed or skipped in these eleven reports. macOS Intel repeat 3 passed
all 39 roots, including the six DNS upstream response-binding variants and
route-advertisement input recovery. This single macOS result does not qualify
those additions across the matrix.

In [Ubuntu 24.04 repeat 1](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773822),
all four Relay failures occur at `control_plane_relay_test.go:185`: after the
only remaining Relay path is restored, fresh TCP/UDP probes succeed, but the
existing TCP session reports `blocked` when `ok` is required. Failure times
are 03:52:47, 03:52:53, 03:53:01 and 03:53:08 UTC. The failing session exchange
has a one-second socket deadline; the parent permits three seconds for its
response. The same socket is retained throughout the outage. UDP session
recovery after this point is not reached in those children.

The observation establishes a failure of the current immediate retained-session
assertion, not permanent loss of the TCP connection. TCP retransmission timing,
Client transport state and the reference peer remain competing explanations.
No retransmission trace or longer retained-session observation was collected,
so the cause is unresolved. Do not infer that fresh-connection recovery proves
existing-session recovery, or relax the assertion without establishing the
intended recovery bound and collecting evidence. These native failures are
separate from the Windows component queue access-denied failure below.

## Final 39-scenario matrix at 985763e

Run [34670764847](https://github.com/endless-net/client/actions/runs/34670764847),
source `985763edf13e74fc479abb293b26420cc653e6f3`, completed with failure on
2026-09-12. All 24 reports contain the same 39 root names: **906 PASS, 30 FAIL,
0 SKIP** (936 outcomes). Nine contract jobs passed and fifteen failed.
The earlier partial snapshots above remain historical observations.

| Platform / repetition | Job | Root outcomes |
| --- | --- | --- |
| windows-2022, repeat 3 | [103491773657](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773657) | 39 PASS |
| windows-2022, repeat 2 | [103491773669](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773669) | 39 PASS |
| windows-2025, repeat 1 | [103491773671](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773671) | 38 PASS, 1 FAIL |
| windows-2025, repeat 2 | [103491773682](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773682) | 38 PASS, 1 FAIL |
| windows-2025, repeat 3 | [103491773684](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773684) | 36 PASS, 3 FAIL |
| ubuntu-22.04-arm, repeat 2 | [103491773689](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773689) | 36 PASS, 3 FAIL |
| macos-15, repeat 2 | [103491773696](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773696) | 39 PASS |
| macos-15-intel, repeat 3 | [103491773697](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773697) | 39 PASS |
| macos-15-intel, repeat 2 | [103491773698](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773698) | 39 PASS |
| macos-15-intel, repeat 1 | [103491773701](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773701) | 39 PASS |
| macos-15, repeat 3 | [103491773708](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773708) | 39 PASS |
| macos-15, repeat 1 | [103491773717](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773717) | 39 PASS |
| windows-2022, repeat 1 | [103491773721](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773721) | 39 PASS |
| ubuntu-24.04-arm, repeat 3 | [103491773761](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773761) | 37 PASS, 2 FAIL |
| ubuntu-22.04-arm, repeat 3 | [103491773787](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773787) | 37 PASS, 2 FAIL |
| ubuntu-22.04, repeat 3 | [103491773808](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773808) | 37 PASS, 2 FAIL |
| ubuntu-22.04-arm, repeat 1 | [103491773814](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773814) | 37 PASS, 2 FAIL |
| ubuntu-24.04, repeat 3 | [103491773818](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773818) | 37 PASS, 2 FAIL |
| ubuntu-24.04, repeat 1 | [103491773822](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773822) | 37 PASS, 2 FAIL |
| ubuntu-24.04-arm, repeat 1 | [103491773837](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773837) | 37 PASS, 2 FAIL |
| ubuntu-24.04, repeat 2 | [103491773854](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773854) | 37 PASS, 2 FAIL |
| ubuntu-22.04, repeat 2 | [103491773875](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773875) | 37 PASS, 2 FAIL |
| ubuntu-22.04, repeat 1 | [103491773880](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773880) | 37 PASS, 2 FAIL |
| ubuntu-24.04-arm, repeat 2 | [103491774023](https://github.com/endless-net/client/actions/runs/34670764847/job/103491774023) | 37 PASS, 2 FAIL |

All twelve Linux jobs fail both Relay roots. Windows 2025 repeat 1 and repeat 2
fail Relay failover; repeat 3 fails both Relay roots. These account for 28 failed
roots. The remaining failures are IPv6 UDP flow consent on Ubuntu 22.04 ARM
repeat 2 and the local-forget IPv4 UDP child of NativeLogoutTraffic on Windows
2025 repeat 3. All macOS and Windows 2022 repetitions pass. No other root or
child failure or skip appears in these reports.

The Windows 2025 repeat 3 cleanup failure occurs at 04:11:34 UTC in
`control_plane_native_udp_test.go:149`, during initial application reachability
before local-forget. The probe exits 1 with an unclassified failure; it does
not satisfy the strict explicit-denial oracle. This does not establish a
local-forget defect, response mismatch, network denial or shared cause with
the separate IPv6 flow failure. The saved diagnostic does not distinguish
process timeout from other unclassified failures. Its three Relay child failures
occur at the already-recorded post-outage retained-TCP assertion.

All eight installation jobs pass the root and four installation children
(40 PASS records, no FAIL/SKIP). Linux and macOS Verify and the separate Client
control-plane job pass; Windows Verify fails with the queue access-denied
component failure recorded below. Optional external STUN compatibility is skipped
and supplies no evidence. The required aggregate
[verify gate 103496452447](https://github.com/endless-net/client/actions/runs/34670764847/job/103496452447)
fails, so this source is not qualified for publication.

The DNS upstream response-binding and route-advertisement roots pass in every
report, as do all other roots outside the four named above. Those bounded
results do not qualify the full source or all HC requirements. This run predates
the flow queue mutex fix, retained-TCP observation, per-probe flow counters,
missing-executable repair, installed startup without control and browser input
correction/completion additions. None of those changes is validated by this run.

## Completed 39-root matrix at d0fc32a

Run [34672491102](https://github.com/endless-net/client/actions/runs/34672491102),
source `d0fc32a9f7c9d0d97623953fbbab3f452790089a`, has completed all 24
native contract jobs. Every report contains the same 39 root test names:
**907 PASS, 29 FAIL, 0 SKIP** across 936 executions. Nine jobs pass and fifteen
fail. These counts describe repeated test executions, not HC coverage percentages.

| Runner | Three repetitions: PASS / FAIL / SKIP |
| --- | --- |
| ubuntu-22.04 | 111 / 6 / 0 |
| ubuntu-24.04 | 111 / 6 / 0 |
| ubuntu-22.04-arm | 111 / 6 / 0 |
| ubuntu-24.04-arm | 111 / 6 / 0 |
| windows-2022 | 117 / 0 / 0 |
| windows-2025 | 112 / 5 / 0 |
| macos-15-intel | 117 / 0 / 0 |
| macos-15 | 117 / 0 / 0 |

The final [macOS ARM repeat 3 report](https://github.com/endless-net/client/actions/runs/34672491102/job/103496467994)
passes all 39 roots. Failures are limited to `NativeRelayTraffic` (15 root
executions), `NativeRelayFailover` (13), and `NativeFlowConsent` (one Windows
2025 repeat 3 execution, with IPv4 TCP and IPv6 UDP child failures). The Relay
observations and unresolved flow diagnostics below remain applicable; this run
predates the bounded retained-TCP recovery assertion. All 72 browser invalid-input
correction/completion leaves pass (three variants in each of 24 jobs), but the
later signed endpoint-projection assertion is not part of this source.

All three platform Verify jobs and the separate Client control-plane job pass.
All eight installation jobs fail: Linux reaches the stopped-service repair
expectation described below; Windows and macOS reach the cached-startup check.
The final [macOS ARM installation report](https://github.com/endless-net/client/actions/runs/34672491102/job/103496467972)
also times out in `cached traffic after service startup without control`, before
the traffic probe, with public cache validity and connected intent present.
It passes fresh installation and disconnected service restart; enrolled reinstall
fails and subsequent uninstall evidence is absent. Do not interpret this as
measured packet loss or successful installation qualification.

The aggregate [verify gate](https://github.com/endless-net/client/actions/runs/34672491102/job/103500740557)
fails; optional external STUN is skipped. This source is not qualified for
publication. Cached bootstrap, explicit Linux repaired-service startup, bounded
TCP recovery, signed endpoint propagation and deferred fatal-probe counters
remain awaiting evidence from a source containing those changes. Follow-up
implementation and native validation remain owned by `endless-net/client` on
`main`; no provider or infrastructure validation is claimed.

## Cached service startup and executable repair qualified on eight runners

Run [34673867164](https://github.com/endless-net/client/actions/runs/34673867164),
source `0cf6d735193f1ffc0980b68cf19bb3b602a54423`, passes all eight installation
jobs. Each report has `TestInstalledClient` and its four children (fresh install,
disconnected service restart, enrolled reinstall, uninstall): **40 PASS records,
0 FAIL, 0 SKIP**. Parent and child records are counted together here, not as 40
independent scenarios.

| Runner | Installation evidence |
| --- | --- |
| ubuntu-22.04 | [103500753829](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753829) |
| ubuntu-24.04 | [103500753841](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753841) |
| ubuntu-22.04-arm | [103500753790](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753790) |
| ubuntu-24.04-arm | [103500753825](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753825) |
| windows-2022 | [103500753819](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753819) |
| windows-2025 | [103500753820](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753820) |
| macos-15 | [103500753886](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753886) |
| macos-15-intel | [103500753911](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753911) |

The enrolled-reinstall sequence now demonstrates missing-executable repair in
both connected and disconnected states. It stops the service, removes only the
known installed executable on the disposable runner, verifies launch fails with
executable absence, repairs it, and checks public identity and traffic behavior.
Linux repair explicitly starts the restored service because package installation
preserves a previously stopped service.

After a connected service restart with control unavailable, public status remains
degraded with the original identity and valid cached map, and actual TCP traffic
passes through the configured peer. Restoring control advances the map revision,
clears degraded state, and preserves traffic. The disconnected variant rejects
traffic after repair and startup without control, makes no registration or
endpoint-refresh HTTP attempts, remains disconnected when control returns, and
resumes traffic only after explicit connect. Assertions use public CLI/IPC,
contract-participant transcripts and observed traffic.

This qualifies the bounded HC-006/HC-030 service restart and HC-062 repair
increments at this source. It does not prove physical machine reboot, cross-version
upgrade, every corruption mode, or the complete HC scenarios. Native contract
repetitions and the aggregate source gate are still outstanding at this snapshot;
the full source is not yet qualified. Further coverage and validation remain
owned by `endless-net/client` on `main`.

## Next work

### Windows DNS participant port allocation: fixture correction pending CI

[Windows 2025 repeat 2](https://github.com/endless-net/client/actions/runs/34678240275/job/103513677174)
at source `6f0bdf3bb4d801804572f7d234314487c1f6843b` fails only
`DNSWireRecovery/udp6/::1/truncated-udp`. At 2026-09-12 06:46:15.681 UTC the
upstream fixture cannot bind UDP to its TCP-selected ephemeral port. This is
participant setup failure, not a failed Client DNS answer. Repetitions 1 and 3
pass all roots, including the new TLS lifetime and DNS rename/address phases.

The fixture now allocates a complete UDP/TCP pair before returning its endpoint,
alternating which transport chooses the ephemeral port, with at most 16 attempts.
Every unsuccessful partial pair is closed. Failure to allocate the first socket
or exhaustively allocate a pair remains fatal. This is bounded fixture setup,
not a Client exchange retry, timeout change or successful skip. Native Windows
validation of the correction remains pending; ownership stays in Client tests.

### Computed-zero IPv6 UDP checksum: Client fix awaiting native validation

[Ubuntu 24.04 repeat 1](https://github.com/endless-net/client/actions/runs/34678240275/job/103513677025)
at source `6f0bdf3bb4d801804572f7d234314487c1f6843b` fails
`NativeFlowConsent/ipv6/udp` at 2026-09-12 06:52:52.804 UTC in the consented
traffic loop. Received/echoed deltas are 1/0. Rejected packet index 501 matches
the final received count: 80-byte IPv6, next header UDP, both expected addresses,
destination port 24001, UDP length 40, **checksum zero**. Earlier rejected packets
are ICMPv6 at indices 7–11. Public inspection is present, WireGuard OK, one peer,
handshake present, no inspection/agent error and valid connected cache. This
locates the echo rejection; the packet payload itself was not logged.

The pinned [WireGuard TUN checksum completion](https://github.com/tailscale/wireguard-go/blob/ae172d45f0f7/tun/offload.go)
can emit a computed zero without mapping it to FFFF. A deterministic component
test constructs this arithmetic edge, runs the pinned public `GSOSplit` API,
then the Client TUN wrapper at a nonzero buffer offset. It failed before the
Client fix. [RFC 8200 section 8.1](https://www.rfc-editor.org/rfc/rfc8200.html#section-8.1)
requires FFFF when the computed IPv6 UDP checksum is zero.

Client now normalizes that value after native TUN read only for complete
fixed-header IPv6 UDP packets whose full pseudoheader/datagram sum confirms the
computed-zero case. Invalid checksums, nonzero fields, mismatched lengths,
extension headers, IPv4 and truncated packets remain untouched and have component
checks. No dependency/version change or reference-echo relaxation is involved.
This proves the arithmetic defect and Client correction at component level;
the observed CI packet's full checksum sum was not recorded, so confirming the
native failure mechanism and recovery still requires the next platform matrix.
Other TCP and unexpected-reply failures are not explained by this correction.

### Running agent DNS map updates: native validation pending

New root `TestControlPlaneNativeDNSMapUpdates` extends HC-025 through the real
agent with native routing and dual-stack map projection. A signed public
`Network.DNSConfig` enables peer DNS with `OverrideLocalDNS=false`. Without
restarting the agent, five maps introduce a peer, replace its IPv4/IPv6 addresses,
rename it, withdraw it and restore its original name/addresses. Public IPC must
advance the map revision with WireGuard OK and no degraded state. UDP/TCP queries
to the agent's `127.0.0.1:53` listener then require exact current A/AAAA answers
and NXDOMAIN without answers for inactive names.

The same test also disconnects through public IPC, requires the agent's TCP DNS
listener to become unavailable, then reconnects without restarting the process.
The original node identity and connected intent must return with WireGuard OK;
UDP/TCP A/AAAA answers must recover, and the withdrawn hostname must stay absent.
This adds bounded HC-017/HC-025 recovery. UDP listener retirement and OS resolver
restoration are not established by the TCP unavailability check.

This exercises the agent-managed DNS path, whose router reapplies the proxy on
configuration changes. The standalone `dns serve` command still loads a single
map at startup. The test does not inspect persisted Client state or call runtime
internals. It does not prove OS resolver selection, continuous availability
during the apply interval, or application traffic by name. Short tests, vet and
lint pass; the existing CI discovers the new root, bringing the native inventory
to 40 roots, and will run it on eight runners with three repetitions. Native
validation remains pending. Further DNS and platform coverage stays in Client.

### Completed matrix at 28cf961: one IPv6 UDP failure remains

Run [34676671634](https://github.com/endless-net/client/actions/runs/34676671634)
completed on 2026-09-12 at exact source
`28cf961b8b796aa1dbe0b1ff8b9464c9f981defd`. All 24 reports contain identical
inventories of 39 root tests: **935 PASS, 1 FAIL, 0 SKIP** across 936 executions.

| Runner | Three repetitions: PASS / FAIL / SKIP |
| --- | --- |
| ubuntu-22.04 | 116 / 1 / 0 |
| ubuntu-24.04 | 117 / 0 / 0 |
| ubuntu-22.04-arm | 117 / 0 / 0 |
| ubuntu-24.04-arm | 117 / 0 / 0 |
| windows-2022 | 117 / 0 / 0 |
| windows-2025 | 117 / 0 / 0 |
| macos-15-intel | 117 / 0 / 0 |
| macos-15 | 117 / 0 / 0 |

The final [macOS Intel repeat 3 report](https://github.com/endless-net/client/actions/runs/34676671634/job/103509328691)
passes all 39 roots. Previous Windows TCP and macOS underlay/retained-port failures
do not recur here; this does not establish their cause or resolution. All 96 Relay
family leaves pass. Flow family/protocol leaves contain 95 PASS and the single
Ubuntu 22.04 repeat 3 IPv6 UDP failure detailed below.

All eight installation jobs pass, with 40 PASS parent/child records (one root and
four children per runner, not 40 independent scenarios). All three platform Verify
jobs and the separate Client control-plane job pass. Optional external STUN is
skipped. The aggregate [verify gate](https://github.com/endless-net/client/actions/runs/34676671634/job/103513660996)
fails; this source is not qualified for publication. The later enrolled TLS
lifetime recovery, DNS rename/address replacement, stricter untrusted-CA rejection
and rejected-packet observations are absent from this source and await native CI.
Follow-up remains in `endless-net/client` on `main`: diagnose the UDP failure,
validate those additions, and cover the remaining HC/BR/AC platform variants.
This matrix does not establish completion of any full HC row.

### IPv6 UDP consent failure with inspection present at 28cf961

[Ubuntu 22.04 repeat 3](https://github.com/endless-net/client/actions/runs/34676671634/job/103509328557)
at exact source `28cf961b8b796aa1dbe0b1ff8b9464c9f981defd` contains 38 PASS roots
and one FAIL root, `NativeFlowConsent/ipv6/udp`. At 2026-09-12 06:18:23.706 UTC,
the consented-traffic loop fails a classified UDP exchange to port 24001.
This is before the expiry loop. Reference received delta is 1 and echoed delta
is 0. Public IPC inspection is present without an inspection error, has one peer,
reports WireGuard OK and a handshake, and retains valid cache/connected intent;
the agent is present without a reported error. Aggregate RX/TX are 59228/64712.

The new diagnostics exclude absent inspection as an explanation for this status
snapshot; they do not prove continuous tunnel health. Reference received counts
all IP packets entering its TUN, whereas echoed counts accepted replies queued
for transmission. The single packet is not correlated to the failed request.
The 128-record history contains accepted UDP requests only, and this classified
timeout has no unexpected-reply digest to correlate. No evidence yet separates
an unrelated packet, a rejected request, scheduling delay or loss elsewhere.
Do not infer a flow-consent or Client runtime cause from temporal proximity.

The UDP participant now retains the last 32 rejected packet header observations:
receive sequence, length, IP version/protocol, expected endpoint-match flags,
and UDP header presence, ports, length and zero-checksum flag. Failure-only logs
withhold addresses and payloads. Sequence numbers can be compared with received
counts, but do not by themselves identify the failed application request.
Truncated IPv4/IPv6 headers have a bounded parsing component test. Native
validation of these observations remains pending; echo acceptance is unchanged.
Client-owned follow-up is to correlate rejected packet shapes with failed
exchanges without exposing payloads or reading Client state, then revalidate.
The completed matrix above does not qualify this source because of this failure.
Assertions remain strict and no retry or runtime change is justified yet.

### DNS peer rename and address replacement: native validation pending

`TestControlPlaneDNSWireRecovery` now keeps one peer ID/public key through six
signed-map phases: present, withdrawn, restored, renamed, IPv4/IPv6 addresses
replaced, and original name/addresses restored. Real CLI synchronization and a
fresh DNS proxy are used for every phase. UDP and TCP queries require exactly
the current A/AAAA address and NXDOMAIN with no answers for the inactive name.
The existing complete-UDP and truncated-UDP/TCP upstream variants run with
IPv4/IPv6 upstreams and listeners. Upstream transcripts must still contain only
their assigned external domains; renamed or withdrawn peer queries must not leak.

This is additional HC-025/HC-026 consumer coverage, not live DNS reload, OS
resolver integration or proof of application IP connectivity. Short tests, vet
and lint pass; native evidence for the new phases remains pending in the existing
eight-runner, three-repetition Client contract matrix. The test reads no Client
state files. Remaining implementation and validation belong to `endless-net/client`.

### Enrolled client TLS lifetime recovery: native validation pending

The untrusted-CA precondition now requires a real CLI process exit with code 1
and a certificate error, rather than accepting any command failure. Its HTTP
transcript must remain entirely unchanged, rather than checking only enrollment
events. This prevents a launch failure or unrelated refusal from proving TLS
rejection. Raw CLI output remains withheld; native validation is pending.

`TestControlPlaneTLSTrustBoundary` now adds `enrolled-expired` and
`enrolled-not-yet-valid`. Each stops the real enrolled agent, presents a leaf
with an invalid lifetime under the already trusted CA, and starts the agent
again. Public IPC must report the original node/network, credential presence,
valid cached map, connected intent and degraded state. The participant's HTTP
transcript must remain unchanged before the valid certificate is restored.
After restoration, the same running agent must clear degraded state and consume
a newer signed map without trust override or another enrollment.

This extends HC-021/HC-030 at the CLI/IPC/TLS boundary. It checks new handshakes
after restart, not rejection of an already established TLS session or actual
dataplane continuity. The existing native contract matrix invokes this root on
all eight runners with three repetitions. Native results for these additions
remain pending; existing passing TLS lifetime leaves only prove the earlier
registration-time checks. No Client private state or backend runtime is read.

### Completed matrix at 1f8df62: endpoint and startup race verified

Run [34675588798](https://github.com/endless-net/client/actions/runs/34675588798)
completed on 2026-09-12 at exact source
`1f8df62444b6c30e070c87d5f265c27e2e698a93`. All 24 reports contain the same
39 root names: **933 PASS, 3 FAIL, 0 SKIP** across 936 root executions.

| Runner | Three repetitions: PASS / FAIL / SKIP |
| --- | --- |
| ubuntu-22.04 | 117 / 0 / 0 |
| ubuntu-24.04 | 117 / 0 / 0 |
| ubuntu-22.04-arm | 117 / 0 / 0 |
| ubuntu-24.04-arm | 117 / 0 / 0 |
| windows-2022 | 117 / 0 / 0 |
| windows-2025 | 116 / 1 / 0 |
| macos-15-intel | 115 / 2 / 0 |
| macos-15 | 117 / 0 / 0 |

The final Windows 2022 reports
([repeat 1](https://github.com/endless-net/client/actions/runs/34675588798/job/103505041589),
[repeat 3](https://github.com/endless-net/client/actions/runs/34675588798/job/103505041531))
each pass all 39 roots. All 72 browser invalid-input recovery leaves pass,
including the corrected endpoint projection. All 24 `SingleAgentOwnership`
roots pass with the simultaneous startup race, winner synchronization and
process cleanup assertions. All 96 Relay family leaves pass. These are bounded
scenario results, not complete HC coverage or proof against intermittent faults.

All eight installation jobs, three platform Verify jobs and the separate Client
control-plane job pass. Optional external STUN is skipped. The aggregate
[verify gate](https://github.com/endless-net/client/actions/runs/34675588798/job/103509317868)
fails because of the three native failures described below; this source is not
qualified for publication. The later UDP digest correlation, interface snapshot
and inspection-presence diagnostics are absent from this source and still need
native evidence. Follow-up remains owned by `endless-net/client` on `main`:
diagnose those failures and implement the remaining HC/BR/AC platform variants.
No provider or infrastructure changes are justified by this result.

[Windows 2025 repeat 2](https://github.com/endless-net/client/actions/runs/34675588798/job/103505041486)
at source `1f8df62444b6c30e070c87d5f265c27e2e698a93` fails only
`NativeFlowConsent/ipv6/tcp` at 2026-09-12 05:37:17 UTC during the expiry loop.
The failed exchange is classified denial with reference application received/
echoed deltas both zero. Public status is available, cache valid and connected;
diagnostic `wireguard_ok=false`, handshake=false and RX/TX zero do not prove
tunnel removal: the previous logger also emits these defaults when the entire
WireGuard inspection is absent. Reference TCP counters do not count SYN packets.
No raw inspection error or peer-presence evidence was recorded in this source.
Flow failure diagnostics now separately record inspection presence, error presence,
peer count and agent presence, using only public IPC. Assertions are unchanged;
short tests, vet and lint pass, and native diagnosis remains pending.

Run 34675588798 at source `1f8df62444b6c30e070c87d5f265c27e2e698a93`
has an independent pair of failures in
[macOS Intel repeat 3](https://github.com/endless-net/client/actions/runs/34675588798/job/103505041497):
`NativeTCPTraffic` fails fresh access to retained port 24002 at
2026-09-12 05:42:46.613 UTC, while the subsequent `NativeIPv6TCPTraffic`
fails IPv4 underlay selection at 05:42:47.091 UTC. The latter requires an up,
non-loopback, non-point-to-point interface with usable IPv4 distinct from the
overlay endpoints. The report does not include the interface snapshot needed to
determine which condition failed. Timing alone does not establish one cause.
The browser corrected-endpoint and startup-ownership race roots pass in this
report; its two traffic failures prevent whole-source qualification.

Both failure locations now log public OS interface indices, flags and address
family counts, withholding actual addresses. The diagnostic changes no selection,
retry, timeout or traffic assertion. Local short tests, vet and lint pass; runner
observations from the new diagnostics remain pending. Investigation stays in
Client-owned tests and runtime, without changing hosted runner infrastructure.

### Completed matrix at 0cf6d73: Relay recovery passes, UDP mismatch remains

Run [34673867164](https://github.com/endless-net/client/actions/runs/34673867164)
has completed all 24 native reports at exact source
`0cf6d735193f1ffc0980b68cf19bb3b602a54423`. Each report contains the same 39 root
names: **911 PASS, 25 FAIL, 0 SKIP** across 936 root executions.

| Runner | Three repetitions: PASS / FAIL / SKIP |
| --- | --- |
| ubuntu-22.04 | 114 / 3 / 0 |
| ubuntu-24.04 | 114 / 3 / 0 |
| ubuntu-22.04-arm | 113 / 4 / 0 |
| ubuntu-24.04-arm | 114 / 3 / 0 |
| windows-2022 | 114 / 3 / 0 |
| windows-2025 | 114 / 3 / 0 |
| macos-15-intel | 114 / 3 / 0 |
| macos-15 | 114 / 3 / 0 |

Every report fails only the browser corrected-endpoint leaf of
`RouteAdvertisement`, except Ubuntu 22.04 ARM repeat 3, which additionally fails
`NativeIPv6UDPTraffic` with the unknown response documented below. The last
[macOS ARM report](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753948)
and [Windows 2022 report](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753980)
both contain 38 PASS roots and that one endpoint failure.

All 96 Relay family leaves (two roots, two families, 24 reports) pass, including
bounded same-socket TCP recovery after complete Relay outage. All 96 flow consent
family/protocol leaves also pass. These qualify those bounded executions at this
source, not all HC-028/HC-057 variants or freedom from intermittent failures.
The startup-race additions, endpoint fixture correction and UDP digest correlation
were added later and are not validated by this run.

All eight installation jobs, three platform Verify jobs and the separate Client
control-plane job pass. The installation scope and its 40 parent/child records
are documented above. Optional external STUN is skipped. The aggregate
[verify gate 103505030526](https://github.com/endless-net/client/actions/runs/34673867164/job/103505030526)
fails, so this source is not qualified for publication. Client-owned follow-up
is to verify the corrected fixture and startup race on later source, correlate
any repeated UDP mismatch, and continue remaining HC variants. No backend or
infrastructure changes follow from these bounded results.

UDP mismatch diagnostics now include SHA-256 digests of the synthetic request
and unexpected reply. The parent accepts only canonical, exactly 32-byte hex
digests and bounded counters; malformed or extra output remains withheld.
The UDP contract participant retains its last 128 accepted echo request digests
and source/destination ports, and the native test logs them only on failure.
These are observations at the participant's packet boundary, not private Client
state. A record proves packet acceptance for echo, not delivery of its reply.
The bounded history can help correlate a later unknown reply with earlier
traffic, but absence from this history does not prove corruption. Strict nonce
matching, denial classification and deadlines are unchanged. Short tests, vet
and lint pass; actual failure correlation awaits native runner evidence.

An additional failure in run 34673867164 is independent of the endpoint fixture:
[Ubuntu 22.04 ARM repeat 3](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753933)
fails `TestControlPlaneNativeIPv6UDPTraffic` at 2026-09-12 05:19:45 UTC, following
the map-signer confirmation phase. The application probe reports 32 differing
bytes and zero zero-valued reply bytes, exits with code 1 in 4 ms, and explicitly
reports no process deadline expiration. This is an unclassified data mismatch,
not an accepted network denial. Temporal proximity does not establish signer
rotation as the cause.

The probe remembers outstanding requests and bounded completed UDP replies
within its own process. Unknown replies remain fatal, as exercised by
`TestUDPPreviouslyCompletedEchoCannotSatisfyNewExchange/unknown-reply`.
The reference UDP echo copies the received packet before swapping addresses and
ports. These source observations do not locate the failing payload: no per-packet
correlation evidence currently distinguishes a delayed reply from another socket
from corruption in the Client or reference path. Do not suppress this failure or
infer a fix from passing flow-consent/Relay roots. Further packet correlation and
native validation belong to Client test tooling; no producer changes are required
by this observation. The endpoint fixture correction does not address this fault.

The first completed contract report from run 34673867164,
[Ubuntu 24.04 ARM repeat 2](https://github.com/endless-net/client/actions/runs/34673867164/job/103500753932),
contains 38 PASS roots, one FAIL and zero SKIP at source
`0cf6d735193f1ffc0980b68cf19bb3b602a54423`. Both Relay roots and all four flow
consent leaves pass in this report. Four retained TCP recoveries take 507 ms,
559 ms, 580 ms and 1.565 s after the initial failed challenge, using one or two
additional challenges on the same socket. This is one repetition, not full
platform qualification.

The failed route-advertisement endpoint leaf exposed a testcontrol omission:
new-node registration did not copy the request endpoint into the signed response.
A public SDK browser-enrollment fixture regression reproduces the missing field
without any real Client process, then passes after initializing the new node's
endpoint from the validated request. The regression also verifies the map
signature. Existing-node endpoint refresh remains owned by its endpoint operation.
The native assertion is unchanged; it must pass in a later source containing this
fixture correction. No Client runtime fix or producer behavior is claimed here.

HC-005 now also races two real agent processes after stopping the existing
owner, in both connected and disconnected states. A barrier releases both
launches; one must exit specifically with the configuration-ownership error,
while the other serves public IPC with the original identity and intent. The
winner is terminated, and a successor must acquire ownership and retain the
same state. The connected winner must also consume a later signed map revision.
Deferred cleanup cancels and waits for both process invocations even if an
assertion terminates the test early. A shared IPC endpoint cannot supply a false-positive loser because
address-in-use is not accepted. No private lock or identity file is read.
This extends the existing mandatory `SingleAgentOwnership` root without adding
a root count. Short tests, vet and lint pass; the actual startup race awaits
native CI on all eight platforms and does not claim hard-link alias coverage.

Per-exchange flow diagnostics now run in a defer, so an unclassified fatal probe
exit also records reference received/echoed deltas. `classified=false` preserves
the distinction from explicit network denial; such failures still fail the test.
Logging starts only after initial peer reachability is established, excluding
expected connection-establishment retries from consent-failure diagnostics.
Counters remain aggregate observations of the reference participant, not proof
of a particular packet's fate. Pass conditions and deadlines are unchanged.

The browser endpoint-correction variant now checks that the signed enrollment
projection contains the corrected endpoint before starting the agent. Later
endpoint discovery may legitimately replace this value. The assertion uses the
fixture's public DTO and registration transcript, never Client private state.
This proves propagation of the accepted input, not endpoint reachability. The
new assertion awaits native CI; earlier passing browser variants proved
completion but did not assert this endpoint field.

Run 34672491102, source `d0fc32a9f7c9d0d97623953fbbab3f452790089a`,
passed [Verify (Windows), job 103496467959](https://github.com/endless-net/client/actions/runs/34672491102/job/103496467959).
Its `internal/client` package passed in 15.318 seconds at 04:35:09 UTC on
2026-09-12. This is new Windows component evidence after the flow-spool mutex
fix, not proof of native flow-traffic correctness or cross-process queue locking.

[Windows 2025 repeat 3, job 103496467955](https://github.com/endless-net/client/actions/runs/34672491102/job/103496467955)
contains 39 roots and fails NativeFlowConsent and NativeRelayTraffic. The flow
children fail independently: IPv4 TCP reports `flow consent expiry disrupted
application traffic` at 04:19:33 UTC; its denied-exchange reference deltas are
received=0, echoed=0. Public status has valid cache, WireGuard OK, handshake and
no agent error. The counters count application data for this TCP reference, so
zero does not establish whether a SYN reached the peer. Aggregate received and
echoed are both 2079; RX=933580, TX=1015448. This is not yet evidence that consent
expiry caused the failed exchange rather than coinciding with it.

IPv6 UDP exits 1 with an unclassified probe failure at 04:26:13 UTC. Public cache,
WireGuard and handshake remain valid with no agent error; aggregate reference
received=2080, echoed=2045. This source predates the process-deadline diagnostic,
and the fatal unclassified path does not return to the per-denial counter logger.
No denial, nonce mismatch or process-timeout cause can be inferred from that exit.

The same Windows job's Relay IPv4 TCP connection recovers on its original socket
after three additional challenges and 2.085 seconds of observation. The original
immediate assertion remains failed in this historical report. This extends the
retained-session recovery observation to Windows, but does not qualify the later
bounded-recovery assertion or explain either flow failure.

The Relay post-outage retained-TCP assertion now explicitly requests bounded
recovery. After the normal one-second exchange reports blocked, it allows up to
15 additional seconds of fresh challenges on the same socket/process. Only a
current-challenge `ok` response passes; explicit continued denial, timeout, closed
process, malformed output or command failure does not. Initial connectivity,
primary-to-backup failover and outage-denial assertions remain immediate and
unchanged. The UDP assertion and fresh-connection checks are unchanged. This is
a test recovery budget, not a product SLA or a claim of lossless transition.

This corrects the unsupported immediate-TCP recovery expectation identified by
the 16 retained-session observations below. It does not rerun a failed test or
replace its connection. Short observer checks reject silence, continued denial,
closed streams, unexpected output and command failures; transient blocked
responses followed by explicit success pass. Previously recorded failing runs
retain their original verdicts. The changed recovery assertion still needs the
complete native platform matrix.

Run 34672491102 at source `d0fc32a9f7c9d0d97623953fbbab3f452790089a`
now supplies retained-session observations from four completed Linux jobs:
[Ubuntu 24.04 repeat 1](https://github.com/endless-net/client/actions/runs/34672491102/job/103496467983),
[Ubuntu 22.04 repeat 1](https://github.com/endless-net/client/actions/runs/34672491102/job/103496468007),
[repeat 2](https://github.com/endless-net/client/actions/runs/34672491102/job/103496468128)
and [repeat 3](https://github.com/endless-net/client/actions/runs/34672491102/job/103496468091).
Every one of their 16 Relay IPv4/IPv6 TCP failures reports `outcome=recovered`,
`attempts=1`, 493–583 ms into the additional observation after the initial
one-second exchange timeout. The same process/socket accepts a fresh challenge;
no redial or agent restart occurs. Original assertions still fail, so these
reports are not green evidence. They establish retained-session recovery for
these cases, contradicting permanent connection loss as their explanation.

[RFC 6298 section 5](https://www.rfc-editor.org/rfc/rfc6298.html#section-5)
requires retransmission backoff after timeout. The observed delay is consistent
with transport retransmission timing, but without a packet trace it does not
identify which endpoint's timer caused it. A one-second application exchange
deadline is not a TCP recovery guarantee. The recovery assertion needs an
explicit bounded recovery observation on the same socket, separately from
immediate access and outage-denial assertions; no reconnect, skipped failure,
unrelated probe exit or stale nonce may count as success. Other platform
repetitions and the separate flow/installation failures remain unqualified.

Cached-bootstrap component checks additionally reject missing cached maps,
mismatched device fingerprints and an enrollment in the recovering phase. Like
the existing invalid-signature, age-limit and missing-credential cases, each must
fail without control requests. These are validation-boundary tests using private
component fixtures, not substitutes for the public-contract/OS integration suite.

The installed-service cached-startup scenario now has an explicit disconnected
counterpart: after disconnected repair, stop the service, make control unavailable
and start through the OS manager. Public IPC must retain disconnected intent and
the same enrolled identity; fresh TCP traffic must remain denied and registration
attempt counts unchanged. Restoring control must still leave traffic denied until
the subsequent explicit connect. This protects the boundary of automatic cached
bootstrap and runs in all eight installation jobs. Registration and endpoint
refresh attempts are counted at the HTTP boundary, so unavailable responses
cannot hide an attempt before request validation. Native evidence is pending;
the connected-startup and disconnected-startup claims are separate.

Installed startup without control exposed a Client startup gap: the online
iteration returned on trust/heartbeat/map request failure before configuring
WireGuard. The long-running agent now attempts one cached bootstrap before its
first online iteration, under the existing operation lock and after disconnected
intent/recovery checks. It requires a node credential, current-device binding,
verified signed cached map and the configured cache-age limit, and uses the same
configuration/apply path as explicit offline mode. No ephemeral Relay credential
is restored from cache. Normal online sync and terminal-credential handling still
run; control failures still produce degraded status. Once/explicit-offline command
semantics are unchanged. A failed cache bootstrap does not prevent online repair.

The new short component test confirms that online sync fails while control is
unavailable, but cached bootstrap retains the node and revision without any
control request. It rejects modified signed content, a cache older than the
configured limit and missing node credentials. This test uses component config
fixtures and no real WireGuard device; it is not contract-only integration
evidence. The existing eight-platform installed-service scenario remains the
required proof of actual cached traffic, control recovery and disconnected
intent. New native evidence is pending; the earlier Windows/macOS failures are
not retroactively passing. No cache-age default, credential lifetime, protocol
or version number changed.

Run 34672491102 at source `d0fc32a9f7c9d0d97623953fbbab3f452790089a`
exposed an installation-fixture sequencing error: Linux missing-executable repair
reinstalled the Debian package after explicitly stopping the service, then waited
for IPC without starting it. Package postinst restarts only a previously active
service marked by preinst; stopped state is preserved. Ubuntu 24.04 ARM
[job 103496467902](https://github.com/endless-net/client/actions/runs/34672491102/job/103496467902)
and Ubuntu 22.04
[job 103496467951](https://github.com/endless-net/client/actions/runs/34672491102/job/103496467951)
report zero IPC responses at the repaired-service phase. Repair now explicitly
starts the Linux service after reinstall; package lifecycle behavior is unchanged.

Windows 2022
[job 103496467913](https://github.com/endless-net/client/actions/runs/34672491102/job/103496467913)
passes connected repair but times out at cached traffic after startup without
control. Public status is degraded with connected intent, node, network,
credential, trust, valid cached map and one peer; no local/cache/intent error is
reported. The failed condition precedes the actual TCP probe, so this does not
yet prove packet loss. A failure-only public IPC diagnostic now distinguishes
identity equality, WireGuard OK, usable listen port, actual WireGuard peer count
and endpoint equality. Its cause remains unresolved and new CI is required.

Unclassified application probe failures now report whether a process exit was
observed, its exit code, whether the parent three-second context expired,
elapsed time and output byte count. Raw process errors/output are not printed;
the existing fixed-message diagnostic allowlist remains. This addresses the
missing distinction in Windows 2025 repeat 3 of run 34670764847. It does not
retroactively identify that failure's cause. Successful/explicit-denial outcomes,
exchange and process deadlines, and retry behavior are unchanged. Native evidence
for the additional diagnostic is pending.

Browser input correction coverage now continues after approval. For each of the
three malformed-input variants, the fixture approves the one corrected request,
the real CLI completes it and the agent exposes a valid cached map and credential
through public IPC. The signed result must retain the intended hostname and,
for the advertisement variant, the corrected prefix. Exactly one approval request
and one registered node must exist in the fixture transcript. The short command
regression also completes the approved request; the agent/projection assertions
are native CI only. This adds approval-completion evidence requirements without
claiming that an accepted endpoint is reachable or an advertised route authorized.

The browser input regression now also covers invalid hostname and endpoint.
Both new short cases failed before the fix because an enrollment POST was sent.
The browser branch now checks the effective request hostname and optional
endpoint with the pinned public WireGuard validators before creating or resuming
an approval request. Direct credential renewal still validates its effective
saved hostname through the existing request validator. The native
`RouteAdvertisement/browser-invalid-input-recovery` branch has three named
variants: advertise, hostname and endpoint. Each rejects invalid input without
an enrollment POST and then reaches exactly one unapproved request after
correction. Native evidence remains pending; these are input-shape checks, not
proof of endpoint reachability or server authorization.

Browser advertisement validation exposed another Client input defect: a short
CLI-command/testserver test proved malformed CIDR reached the browser enrollment
endpoint. Direct registration's validation did not protect this branch. The CLI
now validates each advertised prefix before loading configuration or initiating
enrollment, using `wireguard.ValidatePrefix` from the pinned public contract
module. Correcting the same profile reaches exactly one pending approval request.
The native `RouteAdvertisement/browser-invalid-input-recovery` case requires
the real CLI to reject invalid input without an enrollment POST, then report
approval required for corrected input without registering an unapproved node.
The short regression failed before the fix and passes after it; the new native
branch awaits CI. This proves input correction, not approval completion, route
authorization or forwarding. No producer implementation or contract version changed.

Further results from source `985763edf13e74fc479abb293b26420cc653e6f3`
show the immediate retained-TCP recovery failure also on Windows 2025:
[repeat 1](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773671)
fails `NativeRelayFailover/ipv4`, and
[repeat 2](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773682)
fails both IPv4 and IPv6. All three fail at the same post-outage TCP assertion,
not during initial connection or primary-to-backup failover.

[Ubuntu 22.04 ARM repeat 2](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773689)
also fails `NativeFlowConsent/ipv6/udp` at 03:51:55 UTC on 2026-09-12:
the consented exchange is unavailable. Public status is available, cached map
valid, user-disconnected false, WireGuard OK with a handshake and no agent error.
Aggregate counters are RX 96636, TX 104792, reference received 868, echoed 862,
handshake initiations 2, responses 1, other packets 1143. These aggregates do
not identify which leg lost the particular failed exchange. The stricter probe
oracle in this source confirms explicit exchange unavailability rather than an
arbitrary process exit with code 2. Cause remains unresolved.

Flow probes now record reference received/echoed counter deltas around a denied
exchange, with protocol, address family and fixed test port only. For the UDP
reference, received counts decrypted inbound packets; echoed counts replies
queued to its outbound tunnel, not proven delivery to Client. Background packets
may contribute, so deltas are diagnostic evidence rather than per-packet
attribution. Pass criteria, deadlines and retry behavior are unchanged. New
native CI evidence is pending.

Relay TCP session probes now collect a bounded diagnostic observation when an
expected immediate exchange returns `blocked`. The same probe process and socket
send fresh challenges for up to 15 additional seconds, recording only a fixed
outcome, attempt count and elapsed time. Later success still fails the original
assertion; the one-second exchange deadline and pass criteria are unchanged.
This distinguishes eventual retained-session recovery from continued denial or
probe termination without reading Client internals. Native evidence is pending
in the parallel hosted-runner matrix; this is instrumentation, not a Relay fix
or a newly approved recovery SLA.

HC-006/HC-030 installed-service coverage now stops an enrolled service, makes
the contract control endpoint unavailable and starts it through the real OS
service manager. Public IPC must show degraded control with the original
identity, credential, valid cached map and connected intent; real TCP traffic
must work with that cached authorization. Restoring control must deliver a newer
signed map, clear degraded status and retain traffic. This is part of all eight
installation jobs, with native evidence pending. It tests late control
availability for an already-enrolled service within map validity; it does not
prove first enrollment offline, machine reboot or expired-map behavior.

HC-062 now has executable installed-client repair coverage inside
`TestInstalledClient/enrolled-reinstall`. On disposable hosted runners only,
the fixture stops the installed service, verifies its executable is the known
regular absolute path, removes that executable and requires a file-not-found
launch failure. Linux reinstalls the same Debian package; macOS/Windows restage
the same core binary and rerun their service installer. Both connected and
disconnected intent are tested. Public IPC must preserve node/network/hostname,
overlay address, trust and credential, with valid cached map; real TCP access
must return only for connected intent. Disconnected repair must not register or
refresh, and the complete lifecycle must still have one original enrollment.
Private identity/configuration files are neither read nor removed. No local
installer run was performed; all eight native installation results are pending.
This covers missing-executable repair, not different-version upgrade, arbitrary
file corruption, complete identity reset or reboot recovery.

Run `34670764847`, source `985763edf13e74fc479abb293b26420cc653e6f3`, failed
[Verify (Windows), job 103491773672](https://github.com/endless-net/client/actions/runs/34670764847/job/103491773672)
on 2026-09-12 at 03:38:36 UTC in
`TestTUNFlowProducerRetriesThroughTLSProtobuf`: opening the encrypted queue
during the post-ACK persistence check returned access denied. This component
failure is separate from native IPv6 UDP traffic availability.

A new short Windows regression reproduced a concurrent checkpoint/read/discard
failure before the fix. `flowSpool` now serializes save, load and discard on the
same instance; expiry and empty-checkpoint deletion use the already-held lock.
The regression requires each read to see either no queue or the complete original
window while another worker repeatedly saves and deletes it. Errors are not
retried away or reclassified, and existing ACK, encryption, lease and integrity
checks remain. This is component-level storage evidence, not an interservice
test or proof that the independent native flow failures share this cause.
Cross-instance/process access and external filesystem interference are outside
this fix's synchronization guarantee. New source-specific CI evidence is pending.

HC-010 response-loss coverage now deterministically exercises changed-input
rejection: the contract fixture drops every committed registration response,
including automatic retries, until explicitly restored. The native
`RegistrationResponseLoss` root requires the first CLI process to fail, rejects
a changed hostname without any HTTP registration request, then recovers the
original operation with unchanged request hash and exactly one registered node.
The previous conditional branch could bypass this negative assertion if the SDK
recovered within its first invocation. Existing evidence therefore does not
prove this now-unconditional branch on every platform.

A short command/testserver regression also proves that a changed valid route
prefix is rejected after a lost response, while retrying the original prefix
recovers the same operation. Together with the malformed-input regression, this
checks both sides of the early-validation fix without reading pending state.
Short checks pass; the strengthened native branch requires new CI evidence.

The new advertisement recovery case exposed a Client CLI defect in a short
test: malformed CIDR input was rejected without sending registration, but left
a pending direct operation that prevented correction on the same profile.
`up` now validates the signed direct-registration request before persisting that
operation. A short command/testserver regression requires rejection without an
HTTP registration request, then successful corrected enrollment exactly once,
without local-forget or inspection of configuration contents. Requests already
sent remain subject to the existing unchanged-input replay rule. Native coverage
is the pending `RouteAdvertisement` root; this is not new platform evidence.

HC-012/HC-034 now has `TestControlPlaneRouteAdvertisement`: the real CLI must
reject a malformed advertised CIDR before a registration request reaches the
contract fixture. Retrying the same profile with one IPv4 and one IPv6 prefix
must enroll successfully; the signed registration projection must contain the
requested hostname and both prefixes. Public IPC must report a valid map and
credential before and after agent restart with the original node identity, and
the transcript must contain exactly one enrollment. This is the 39th native
root (39 x 3 x 8 = 936 expected matrix root outcomes); native evidence is pending.
This checks advertisement submission and registration recovery, not route
approval, SNAT, router forwarding or access granted to another client.

The native DNS response-binding scenario also distinguishes valid upstream
negative answers from correlation failures: NXDOMAIN and REFUSED must retain
their original RCODE and contain no answer, then the same running proxy must
resolve successfully after upstream recovery. Wire counters verify the expected
UDP-only or UDP-to-TCP path. Both downstream transports and both IP families run
these cases in all six existing leaves. Their native CI evidence is pending;
the assertions do not treat an arbitrary upstream failure as name absence.

The later 2026-09-12 snapshot of run `34668955037` extends the partial evidence
above to nineteen completed jobs with the same 37-root inventory: **701 PASS,
2 FAIL, 0 SKIP**, including **74 PASS and 2 FAIL** flow-consent leaves. Five
contract jobs were still live. Additional completed jobs are listed below.
Windows 2025 now has all three repetitions: 111 root PASS outcomes and all twelve
flow-consent leaves PASS. Interrupted disconnect and exact CLI/IPC build-commit
assertions passed on that Windows runner too. This does not qualify Windows 2022,
the complete eight-platform matrix, or changes after source `4a8011f`.

| Additional runner and repetition | Job | Root outcomes |
| --- | --- | --- |
| windows-2025, repeat 1 | [103487108918](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108918) | 37 PASS |
| windows-2025, repeat 2 | [103487108991](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108991) | 37 PASS |
| windows-2025, repeat 3 | [103487109015](https://github.com/endless-net/client/actions/runs/34668955037/job/103487109015) | 37 PASS |
| macos-15, repeat 3 | [103487108993](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108993) | 37 PASS |
| macos-15-intel, repeat 3 | [103487109054](https://github.com/endless-net/client/actions/runs/34668955037/job/103487109054) | 37 PASS |

The DNS response-binding root now includes `invalid-truncated-udp` alongside
`udp-answer` and `tcp-answer`, for both IP families. All seven invalid response
variants must produce SERVFAIL without any upstream TCP question when the
untrusted UDP answer sets TC. Wire-observed counters require exactly one UDP
question and no TCP question for that rejection; repairing the same fixture
must require one UDP and one TCP question and restore resolution. The existing
direct-UDP and invalid-TCP cases now assert their transport counts too. This
distinguishes rejection before fallback from a failure after an unwanted TCP
attempt. Six native leaves are pending source-specific matrix validation.

Run `34668955037`, source `4a8011f90d839e6771d05999933a7738bc9a315f`, has
confirmed `NativeFlowConsent/ipv6/udp` failures on
[Ubuntu 24.04 repetition 3](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108890)
and [Ubuntu 22.04 ARM repetition 3](https://github.com/endless-net/client/actions/runs/34668955037/job/103487108946).
The consented-traffic probe reported unavailability at 03:10:34 UTC and
03:08:26 UTC on 2026-09-12 respectively. These are traffic-availability failures,
not evidence of an incorrect flow payload or the earlier unknown-nonce error.
Other matrix jobs were still live when these failures were recorded; this run
cannot qualify the four-way consent expansion. The source predates the stricter
exit-code/output oracle, so the old classification alone does not prove a
network-level cause. No root cause is assigned yet.

On native flow failures the fixture now records bounded reference packet and
handshake counters plus public IPC availability, cached-map validity, intent,
WireGuard health/byte counters and whether an agent error is present. It does
not log error text, payloads or credentials, and leaves all failure criteria and
deadlines unchanged. This supplies context for the next native failure without
reading Client-private state.

HC-025/HC-026: `TestControlPlaneDNSUpstreamResponseBinding` adds a real-CLI
boundary scenario for upstream answers with the wrong transaction ID, question
name/type/class, opcode, missing question or unset response flag. The same proxy
process must return SERVFAIL for each invalid answer and resolve successfully
after the fixture repairs its response. All seven faults run through IPv4/IPv6
upstreams, direct UDP answers and TCP answers after a valid truncated UDP reply,
with both UDP and TCP downstream queries. This is the 38th native root; its
source-specific matrix evidence is pending (38 x 3 x 8 = 912 root outcomes).

A short real-UDP regression reproduced all seven accepted unrelated answers
before the Client fix. The proxy now checks response headers and question
correlation before trusting an answer or its truncation bit, and checks the TCP
fallback response too. The question parser permits DNS name compression and
compares names case-insensitively. Connected upstream sockets constrain the
remote endpoint. These checks implement the query-matching boundary described
by [RFC 5452 section 9.1](https://www.rfc-editor.org/rfc/rfc5452.html#section-9.1);
they do not establish DNSSEC, complete answer-section validation, live map reload
or OS resolver integration. No DNS or product version was increased.

The application traffic oracle now requires both exit code 2 and the exact
`application exchange unavailable` output line before asserting denial. Exit 2
alone also represents Go flag-parser failures and runtime panics, so it cannot
establish a policy/outage result. A short regression reproduced four false
denials before the fix (flag error, empty output, panic and extra output), and
also checks LF/CRLF denial messages, wrong exit codes, success and nonce mismatch.
Unexpected failures still fail the test using bounded diagnostic filtering.
This tightens all callers of the shared probe helper, including native and
installation tests; it does not explain or suppress the unknown UDP nonce.

HC-030 native direct IPv4/IPv6 TCP/UDP scenarios now break control streams and
return temporary service failures, wait for the real agent's public `degraded`
status with valid cached authorization, then require three successful exchange
rounds on both existing sockets and new connections to both fixture ports.
The protocol-stack peer also requires ICMP echo during the outage. Restoring
control must deliver a newer signed map, clear degraded status, retain the
original identity, and preserve existing/new application access and ICMP.
The transcript must contain exactly one enrollment. These checks run within the
four existing native direct roots on every platform; CI evidence is pending.
This covers a short outage within map validity, not expired offline authority,
reboot, network change or dependency-specific server failure semantics.

HC-028/HC-030 retained-session coverage is now executable in
`TestControlPlaneNativeRelayTraffic` and `TestControlPlaneNativeRelayFailover`:
TCP and UDP probe processes open their sockets through the primary Relay before
any fault. The same connections must exchange fresh nonces after primary-to-backup
failover, deny exchange during complete Relay unavailability, and recover after
the primary returns. New-connection probes and public selected-path assertions
remain independent requirements. Both IPv4 and IPv6 run in every native matrix
job; source-specific CI evidence is pending. This does not prove uninterrupted
delivery during the transition, direct/Relay switching or healthy-backup failback.

`TestControlPlaneSingleAgentOwnership` now adds `crash-connected` and
`crash-disconnected` variants: forcibly terminate the alias-started agent, then
start its successor with the canonical configuration path and the same IPC
endpoint. Public status must retain the enrolled node, network, credential,
cached map and connection intent; the connected successor must consume a newer
signed map. The existing registration-event assertion still requires exactly one
enrollment across all restarts. This checks process ownership and IPC recovery
without inspecting lock files or persisted identity. Native matrix evidence is
pending; it does not establish host-reboot, crash-during-write or traffic recovery.

The UDP probe now retains a bounded history of 128 completed replies per
connection. A duplicate of one of those known replies is discarded while waiting
for the current nonce; it cannot establish access. A short real-UDP regression
failed before the fix for both duplicate-plus-current and duplicate-only cases.
It now requires success only with the current reply, timeout/denial when only an
old duplicate arrives, and a distinct fatal error for an unknown modified reply.
TCP behavior and unknown-response failure handling are unchanged. This fixes
duplicate handling within a retained UDP connection; it does not attribute or
fix the CI mismatch in a fresh probe process, which has no completed history.

Run `34668100780`, source `8a0d75cf3294db832828ebdd4c7da0c02509a032`, failed
the native logout root on Ubuntu 22.04 ARM repetition 2
([job 103484236542](https://github.com/endless-net/client/actions/runs/34668100780/job/103484236542)).
The logout/ipv4/udp leaf failed during its initial traffic baseline, before
cleanup: an unknown 32-byte echo differed in all 32 bytes, with zero zero-valued
bytes. The whole root took 97.34s; the failed leaf took 5.42s. This supplies no
successful cleanup evidence for that leaf and shows the nonce-mismatch class is
not Windows-specific. Unknown replies remain fatal, not classified as denial.

Source inspection found the reference echo copies its input before swapping
addresses/ports, and the [pinned ChannelTUN implementation](https://github.com/tailscale/wireguard-go/blob/ae172d45f0f7/tun/tuntest/tuntest.go)
copies incoming packets before handing them to the echo loop. This does not
establish the failure's cause or exclude corruption elsewhere, duplication or
late delivery from another probe. No runtime fix is claimed from this audit.

HC-018 adds `TestControlPlaneInterruptedDisconnect`, the 37th common root.
The contract peer applies one authenticated offline request but holds its HTTP
response. While the real disconnect CLI is still waiting, the driver forcibly
terminates the agent and requires the CLI to fail. After restart, public status
must retain the original credential/map and disconnected intent; explicit connect
must restore connected intent without another registration. No private persisted
state is read. A short fixture check verifies that the hold does not block other
requests, release is idempotent and the next offline response is not held.
This tests one process-crash boundary, not power loss during a storage write or
actual traffic retirement. Native evidence is pending; the required root matrix
is now 37 x 3 x 8 = 888 outcomes.

HC-022/053 request validation now calls the public native local-forget endpoint
directly, bypassing CLI validation. Omitted/false confirmation must return the
typed confirmation-required error; a string in place of the boolean must return
invalid-JSON. Every rejected request must preserve the original credential,
cached map and connected intent and produce no remote deletion/logout observation.
These are mandatory variants of the common request-validation root; native
evidence remains pending. They complement the CLI and actual-traffic checks.

The aggregate report verifier now requires one matching run/PASS pair for every
observed subtest as well as each compiled root. Two short regressions replaced
either the child's run or PASS with an output event while leaving the parent and
package successful; both invalid reports were accepted before the fix. Missing,
duplicate or incomplete child executions now fail verification. The recorded
test2json subtest fixture remains an acceptance check. This strengthens report
integrity; it cannot detect an intended variant absent from both code and report,
so the HC/variant audit is still required. New gate execution in CI is pending.

HC-058 native builds now embed the exact GitHub source SHA in `main.commit`.
The IPC-negotiation root requires the CLI `version` commit and OS/architecture
to match that source and runner, and requires the running agent's public IPC
ServiceCommit to match before and after restart. Missing/malformed expected SHA
fails the scenario. This complements the artifact source/shard gate; it does not
prove signed release provenance, installer metadata or different-version upgrade.
No version number is changed. Native evidence for this identity check is pending.

HC-057 native flow consent now runs all four IPv4/IPv6 TCP/UDP combinations.
Each variant requires real application traffic, no reporting before consent,
accepted public RPC windows matching the exact source/destination/protocol/port,
unchanged retry after a lost acknowledgement, silence after revocation and
expiry, and renewed collection after a new grant. Application access must survive
all consent changes. The root count remains 36; all four leaves are mandatory.
These additional real-time grant/expiry lifecycles, together with eight cleanup
traffic variants, increase the whole native repetition budget from 15 to 25
minutes and its job budget from 20 to 30 minutes. Individual operation deadlines,
consent windows, failure classifications and required repetitions are unchanged.
This budgets new coverage; it does not resolve or reclassify prior timeouts.
Native qualification of the expanded flow matrix is pending.

HC-022 additionally has `TestControlPlaneNativeLogoutTraffic`: eight variants,
with confirmed logout and forced local-forget independently covering IPv4/IPv6
TCP/UDP. Each variant establishes real native traffic and two sessions, then requires
cleanup to block those sessions and fresh traffic on both ports.
Public status must clear enrollment/map state; restart must retain that state
and traffic denial. The TCP reference peer also checks ICMP echo denial before
and after restart. The testserver transcript requires one original creation and
one confirmed deletion for logout. Local-forget runs while control is unavailable,
must report unconfirmed remote cleanup and must record no remote deletion/logout.
Before that cleanup, the real CLI invocation without confirmation must return
exit 1 and its confirmation diagnostic while preserving the original registration,
both established sessions, fresh traffic on both ports and applicable ICMP echo.
Control is restored before restart, which must still retain traffic denial.
This uses the existing protocol peer, not another real
Client or a backend runtime. Native evidence is pending. With this 36th root,
the automatic matrix inventory requires 36 x 3 x 8 = 864 root outcomes.

HC-022 adds `TestControlPlaneLogoutRetryAfterControlRecovery`, the 35th common
root. Unavailable control must cause an explicitly unconfirmed logout error;
public status must retain the original registration and credential across an
agent restart. After control recovery, retry must return confirmed cleanup,
produce exactly one deletion for the original node, clear public enrollment/map
state and retain disconnected intent after a second restart. Exactly one node
creation is observed throughout. This is distinct from forced local-forget and
does not yet prove real-traffic retirement or every remote cleanup stage.
The automatic native inventory now requires 35 x 3 x 8 = 840 root outcomes;
platform evidence for this new scenario is pending.

The CLI event consumer now rejects a first event other than hello before writing
it to stdout, matching the ordering requirement in the public IPC OpenAPI events
description. A short regression supplied status_changed followed by hello:
before the fix, the CLI reported success for this invalid order. The regression
now requires failure and empty stdout. The native CLI fault root includes the
same invalid sequence, followed by repaired-endpoint recovery, for all eight
platforms. Native qualification remains pending; this does not claim validation
of every later event sequence or payload.

HC-052/053 now has a 34th common root, `TestControlPlaneCLIIPCFailureBoundary`.
It drives the shipping CLI over an actual Unix socket or Windows named pipe
against an HTTP/NDJSON contract fault fixture, without starting an agent. A
stalled pre-hello response, empty EOF and malformed event must each produce exit
1, empty stdout and nonempty stderr within the outer deadline. The stalled case
must honor the explicit two-second CLI deadline; the fixture must have observed
the public events request so a dial/setup failure cannot satisfy the test.
After each failure, the same listening endpoint switches to a valid hello and
keeps the stream open. A fresh real CLI invocation must emit exactly that hello,
leave stderr empty and exit successfully at its two-second listening deadline.
This proves CLI consumer recovery only, not agent authorization or recovery.
The existing real-agent subscription/restart scenario remains separate. CI
discovers this root in all 24 native jobs and requires the same source inventory;
the new total is 34 x 3 x 8 = 816 root outcomes. Native evidence is pending.

Windows IPC peer inspection now locks the goroutine to one OS thread for
impersonation, thread-token inspection and reversion. Previously it could migrate
between those thread-scoped operations. This is a source-level correctness fix:
[Microsoft's impersonation contract](https://learn.microsoft.com/en-us/windows/win32/api/namedpipeapi/nf-namedpipeapi-impersonatenamedpipeclient)
binds the security context to the calling thread. It does not establish that
thread migration caused the observed Windows 2022 subscription failure.
The common IPC-events scenario additionally opens eight independent public
subscriptions together, requires hello and enrolled status on every connection,
then requires cancellation of each before continuing the existing mutation and
restart sequence. No failed subscription is retried. The fix and concurrency
increment passed the complete qualified `ef19a9f` matrix above; this does not
prove all local roles.

Source `08d5186` also failed Windows 2025 repetition 1
([job 103478854598](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854598)):
two truncated-UDP DNS variants could not bind their TCP fixture to the previously
allocated UDP port. No Client DNS conclusion follows from this setup failure.
The fixture now allocates TCP first, then binds UDP to that same endpoint, so TCP
port selection respects existing TCP sockets and TIME_WAIT. The original OS error
was withheld, so TIME_WAIT is not established as the specific cause. UDP bind
failure remains fatal; there are no retries, skipped families or relaxed wire
assertions. All DNS variants passed with the changed allocation order in the
qualified `ef19a9f` matrix above.

Run `34666246762`, source `08d5186`, failed the IPC-events root in Windows 2022
repetition 1 ([job 103478854527](https://github.com/endless-net/client/actions/runs/34666246762/job/103478854527)):
the first SDK subscription ended before the initial hello, after the CLI event
check. The cause is unknown; the test previously discarded the Stream error.
Unexpected termination now reports only a fixed error category or numeric OS
error and last observed sequence. It remains fatal, without retries or raw error
output. This is additional investigation, not a fix or a qualification claim.

HC-021 TLS lifetime acceptance now extends the common TLS root with expired and
not-yet-valid leaf certificates signed by the already trusted test CA, at the
same origin. Each real CLI enrollment must fail with a certificate error before
any HTTP handler observation. Restoring a valid leaf must allow the same Client
profile to enroll and preserve its identity across agent restart. The fixture
closes previous connections to require a fresh handshake and retains private
TLS material only in memory. A short contract-fixture test verifies both lifetime
denials and recovery independently. Short checks passed locally; all native
variants passed at qualified source `ef19a9f`. This does not prove expiry handling for an
already established agent session or data-plane retirement at expiry.

Failed harness Service commands now trigger one public status query with a
separate two-second outer deadline. Logs contain only boolean intent/credential/
cache/WireGuard presence and health, or a fixed status-error category. The
original command duration and fatal outcome are preserved. This may distinguish
responsive IPC with persisted intent from an unavailable status path on another
disconnect failure; it does not establish which internal stage stalled and does
not qualify the previous Windows timeout as fixed.

The unexplained Windows UDP nonce mismatch now has bounded probe diagnostics:
the number of differing bytes versus the current request and zero bytes in the
received 32-byte response. Payloads/nonces are not printed. The harness accepts
only an exact reconstructed format with valid counter ranges; trailing or
arbitrary child output remains withheld. The wrong-response regression still
requires an error distinct from traffic denial and verifies a one-byte mutation.
This improves investigation only: unknown replies remain fatal, and the observed
native mismatch has not been attributed or fixed.

Windows HC-004/053 installation now checks administrator-only local-forget using
a restricted child CLI token in both connection states. Administrators must be
deny-only, CLI version must execute, and local-forget must fail without a success
payload. Logs distinguish pipe access denial from explicit IPC administrator
authorization; the former does not prove application-role handling. The parent
then requires the original identity/intent and corresponding TCP behavior.
This preserves the account SID and leaves another-owner/observer scenarios open.
Both Windows hosted installation jobs passed for source `08d5186`, as recorded
above; only compilation/short checks ran locally.

HC-004/053 Unix installation acceptance now checks the pre-existing nobody
account: a non-root UID and executable installed CLI, then explicit IPC permission
denial for status/connect/disconnect/confirmed local-forget while the enrolled
service runs. Both connection states must retain identity and real TCP behavior.
This passed on the six Linux/macOS runners for source `c36c0db`, as recorded
above. Observer/owner/admin authorization after Unix transport access remains
open; these checks do not claim all-platform application-role coverage.

The DNS wire root now independently varies the Client listener address family
and upstream address family, with complete/truncated UDP replies: eight variants
per runner. Every listener must report exactly the requested loopback address,
then accept both UDP and TCP queries. The full private/global/split lifecycle
runs through each combination. This adds native IPv6 proxy-listener acceptance
without assuming it from IPv6 records or upstreams. All 192 DNS leaf outcomes
passed for source `c36c0db`; that source's aggregate gate failed on two other roots.

HC-025/026 also runs both upstream address families with truncated UDP answers.
The fixture binds TCP on the same endpoint, supplies no UDP answer records and
requires the Client to repeat the DNS question over length-framed TCP. Domain
observations and doubled query counts cover successful resolution and split
SERVFAIL/recovery without leaking to the global resolver. This is normal DNS
transport retry, not a legacy protocol fallback. The DNS variants passed across
all 24 jobs for `c36c0db`; whole-source qualification remains blocked by its two
Windows failures.

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
resolver setup or live map reload; eight-platform evidence is recorded at `38050bc` above.

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
This supplements SDK subscriptions; hosted evidence is recorded at `38050bc` above.

HC-004/052/053 now have installed-service absence assertions in both enrolled
connection states: six public CLI read/mutation/subscription commands must fail with exit 1,
stderr and no stdout payload within a bounded harness deadline. Restart must
retain identity and intent, restore connected TCP access and preserve disconnected
traffic denial without registration. This runs inside the existing eight-platform
installation matrix and passed all eight runners at `38050bc` above.

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

Run [34689252202](https://github.com/endless-net/client/actions/runs/34689252202)
completed at source `9df3146134c26bc184ef444a100a33d0f55d1959`. All 24
native reports were inspected and contained the same 41-root inventory: 971
PASS, 13 FAIL and 0 SKIP out of 984 expected root outcomes. All six macOS jobs
and all Ubuntu 24.04 ARM jobs passed. Twelve deterministic failures were the
new native system-resolver root on all Ubuntu 22.04 amd64/arm64 and Windows
2022/2025 repetitions. The remaining Ubuntu 24.04 amd64 failure was the IPv4
UDP flow-consent variant: a delayed, valid reply from an earlier exchange
arrived after the OS reused its source port and was rejected as the current
nonce. The reference fixture recorded the current request afterward. This is
not a Client dataplane denial.

The packet probe at `902a2b9` now retains an unknown UDP reply while continuing
to wait for the current nonce. It succeeds only when the current reply arrives,
and still returns a fatal mismatch if no current reply arrives before the
deadline. A component regression covers both outcomes. Linux and Windows
system-resolver diagnostics were added at `6777ede` and `73ebdf2`; they report
only bounded outcome booleans and will distinguish Client OS configuration
failure from an application resolver path that bypasses the platform manager.
These corrections postdate run 34689252202 and require their own complete
24-job exact-source matrix before either failing root is qualified.

`TestControlPlaneNativeApplicationRoute` adds Client-owned HC-040 coverage for
both address families. A real Client first receives the application without a
route and must return NXDOMAIN and block resource traffic. A signed route then
projects the domain to a distinct resource stack behind a WireGuard connector;
the declared TCP port must work while UDP and a second TCP port remain blocked.
Route withdrawal and natural expiry must each retire DNS and fresh traffic, and
a fresh signed grant must recover both. The test observes DNS wire answers,
application exchanges and forwarding-hop packet counters. It does not import
Client runtime internals or treat this Application scenario as HC-051 service
abstraction evidence. Its 42-root / 1,008-outcome native matrix is pending.

`TestControlPlaneNativeServiceCatalog` adds separate HC-051 coverage for IPv4
and IPv6. The signed service starts without approval and must return NXDOMAIN.
After approval, its logical DNS name must contain exactly two signed host
identities, and traffic to every returned address must cross the real native
WireGuard path on the declared TCP port while UDP and another TCP port remain
blocked by the signed peer policy. Removing one host must remove only that DNS
answer; losing approval must fail DNS closed; restoring approval must return
both usable hosts. Direct peer authorization is asserted separately from
logical-service membership. Health interpretation, balancing and connection
draining are still product gaps. This raises the pending common inventory to
43 roots / 1,032 native outcomes.

`TestControlPlaneSessionExpiryRecovery` adds HC-014 at the public credential
boundaries. The CLI first logs in through the discovery document and performs
an authenticated user RPC, then enrolls and starts the real agent. The fixture
rotates only the user session. The old session must be denied by the user RPC,
while the independent node credential continues consuming a newer signed map
and keeps native TCP traffic available. Login with the replacement session
must restore the user RPC without changing the node ID or registering again;
agent restart must retain that identity and traffic. The testserver exposes the
standard discovery document and a fixture-only session-rotation control; the
test never reads the Client config or service state. This raises the pending
common inventory to 44 roots / 1,056 native outcomes.

The first diagnostic Ubuntu 22.04 results exposed a Client DNS protocol defect,
not merely runner delay. An existing IPv4-only peer name returned NXDOMAIN to
an AAAA question. A validating system resolver can cache that response for the
owner name and discard the successful A result. Peer, Application and service
DNS now return NOERROR with an empty answer (NODATA) when the signed owner name
exists but has no address in the requested family; truly absent or withdrawn
names still return NXDOMAIN. Component regressions cover all three projections.
The native root keeps an IPv4-only peer so the combined system-resolver path
exercises this distinction. On Windows, the diagnostic run showed a present
NRPT rule and successful `Resolve-DnsName` while the Go packet probe bypassed
that platform path; Windows assertions now use the native resolver API and
also require the Client listener's exact negative wire outcome. The correction
and bounded OS resolver convergence wait require a new complete matrix.

`TestControlPlaneJoinTokenRotation` adds HC-015 at the public registration and
node-credential boundaries. A real enrolled Client first establishes native
TCP traffic. The fixture rotates only its join token; the old token must deny a
second Client, while the first Client consumes a newer signed map, retains its
node credential and traffic, and survives restart. The replacement token must
then enroll exactly one distinct node. The fixture control models token
revocation without reading a producer database, and the test observes only CLI,
IPC, signed-map and traffic outcomes. Token expiry and offline enrollment remain
separate. The pending common inventory is now 45 roots / 1,080 outcomes.

The HC-012 browser-registration matrix now includes requested tags alongside
hostname, endpoint and advertised prefixes. A tag containing a forbidden line
break must be rejected before an enrollment request reaches the testserver.
Correcting it must create one request, preserve the requested tag in the signed
registration result and complete exactly one node registration after approval.
This verifies Client input and projection behavior; it does not infer effective
policy authorization from the requested attribute. The common root count stays
45 while the expanded root awaits native qualification.

`TestControlPlaneSubnetRouter` adds HC-034 at the real Client dataplane boundary.
On Linux, two independently registered Clients run in separate network
namespaces and the router has a third LAN namespace behind it. The router
advertises the LAN prefix with forwarding/SNAT enabled. The source must fail to
reach a live TCP/UDP application before route approval, reach it only after the
signed peer projection includes the prefix, lose both paths after withdrawal
and router shutdown, then recover after approval and same-identity restart.
The isolated topology has no alternate source-to-LAN route. On Windows and
macOS the same common root requires an explicit unsupported outcome before any
registration because the current forwarding hooks are Linux-only; the CLI no
longer silently accepts a nonfunctional setting. IPv6, no-SNAT, independent
route policy and HA remain separate variants. This raises the pending common
inventory to 46 roots / 1,104 native outcomes.

The existing diagnostics root now adds a bounded HC-056 control-layer failure.
With a connected Client, the testserver becomes unavailable and public IPC must
report a degraded agent with an error while keeping connected intent, the same
node identity, its credential and the verified cached map. Restoring control
must return diagnostics to connected state without registration. This separates
a control outage from explicit user disconnect in the same root. Peer-path, DNS
and application-layer localization remain separate pending increments; the
common inventory stays at 46 roots.

The still-running matrix for source `00de988c019b27635c6392dedf9cbf75e0765e71`
exposed three repeated client-suite failures. Browser enrollment did not validate
attributes through the full published registration request contract; the
rotation scenario tried to reuse a denied durable operation with a different
token despite the unchanged-operation rule; and Ubuntu 22.04's `systemd-resolved` could
not reach a link-scoped loopback DNS server. The client now validates a signed
authorization-bearing copy without sending its synthetic credential, the
rotation scenario uses a distinct replacement client, and Linux publishes the
Client DNS proxy at the tunnel's assigned IPv4 address. The Linux choice also
covers the older loopback-ifindex defect fixed upstream in
[systemd PR 25438](https://github.com/systemd/systemd/pull/25438). A local native
Linux probe confirmed that the proxy answered at the tunnel address and
`resolvectl query` selected it; qualification still requires all 24 exact-source
CI reports.

Completed Windows reports from the same run also exposed a shared application
and service-catalog failure. A signed DNS projection change was included in the
platform router equality check, so Windows treated a DNS answer update as a
full interface change and removed/recreated its addresses instead of applying a
route delta. The DNS-aware wrapper remains responsible for restarting the proxy;
Windows and Darwin platform routers now compare only OS-installed state. Unit
checks require a proxy-only projection update to issue no interface commands.
Native qualification remains pending in the next exact-source matrix.

Run [34690909647](https://github.com/endless-net/client/actions/runs/34690909647)
completed at source `73ebdf2699fd89bb66ce52183178e5a9421fe2a6`.
All 24 reports contained the same 41-root inventory: 970 PASS, 14 FAIL and
0 SKIP out of 984 expected outcomes. The twelve system-DNS failures comprised
all Ubuntu 22.04 amd64/arm64 and Windows repetitions. Ubuntu diagnostics showed
the systemd stub in use but resolution failure; the source still returned
NXDOMAIN for an IPv4-only name's AAAA query. Every Windows failure had an
installed NRPT rule and a successful native `Resolve-DnsName`, proving the old
Go probe was not the Windows system-resolver path. Both causes are corrected
after this source.

Two unrelated single-repetition failures were also retained. macOS 15 ARM
missed the first UDP reply immediately after a live DNS map update; UDP wire
assertions now retry up to three independent bounded queries while preserving
exact question, response code and address checks. Windows 2025 repeat 3 timed
out waiting for diagnostic peer enumeration after a connected agent restart,
although public state was connected with one projected peer. The lifecycle
gate now requires the usable WireGuard listener and then proves configuration
with real TCP/UDP/ICMP traffic; peer enumeration is no longer substituted for
that traffic evidence. Timeout diagnostics now include bounded public agent
and WireGuard presence, error, port, peer-count and revision fields if the
failure recurs. All eight install jobs, all three platform verify jobs and the
separate Linux control-plane job passed. The aggregate verify job correctly
failed because the contract matrix was not green.

Run [34693000779](https://github.com/endless-net/client/actions/runs/34693000779)
completed with failure at source `00de988c019b27635c6392dedf9cbf75e0765e71`.
All 24 native artifacts were inspected. Their common inventory had 45 roots and
1,080 expected outcomes; two Windows 2025 jobs reached the old 25-minute suite
deadline, so 1,076 terminal root outcomes were recorded: 1,007 PASS, 69 FAIL and
0 SKIP, with four outcomes missing after the timeout. The repeated failures were
24 invalid route-advertisement inputs accepted by the Client, 24 join-token
replacement attempts that violated immutable operation identity, six Ubuntu
22.04 system-resolver failures, six Windows Application failures and six
Windows service-catalog failures. Windows 2025 also had one isolated routed
resource, Relay traffic and Relay failover failure. The two timeout reports had
already completed 43 roots in about 25 minutes and were executing map-signing
rotation; this was aggregate suite duration rather than a single stuck root.
The Client/input, scenario, Linux DNS and Windows route changes above address
the repeated causes. The test process now has 30 minutes inside a 35-minute job
so report upload remains possible. This source predates every correction and
does not qualify them.

`TestControlPlaneEphemeralLifecycle` adds bounded Client-owned HC-011 coverage.
The real Client exposes the provider-assigned `ephemeral` node attribute through
public IPC, carries a native TCP exchange to a WireGuard peer, is terminated
without graceful logout, consumes the provider's published terminal credential
outcome after cleanup, clears local access and permits a new job to enroll with
a distinct node identity. The fixture controls token metadata and the cleanup
decision but the assertions read only CLI/IPC, signed maps and traffic. Provider
absence detection and cleanup timing are deliberately not claimed. This raises
the pending common inventory to 47 roots / 1,128 native outcomes.

`TestControlPlaneExitProvider` adds Client-owned HC-038 coverage. On Linux, two
real Client processes run in isolated namespaces. The provider registers the
published `0.0.0.0/0` advertisement and SNAT intent; the consumer receives the
default route only after an explicit signed map update. Fresh TCP and UDP
payloads must then reach an address hosted beyond the provider and return
through its forwarding hop. Removing the default route closes both protocols;
restoring it and restarting the provider restores access without registration.
The external namespace has no route to the consumer overlay, so successful
replies also depend on the provider's SNAT behavior. Windows and macOS execute
the CLI path and require the documented unsupported result before registration.
IPv6 exit provision, HA and observation against a public Internet service remain
separate acceptance work. The pending common inventory is now 50 roots / 1,200
native outcomes across the 24-run matrix.

The native installation suite now adds a bounded HC-060 upgrade assertion on
all eight installation runners. CI builds two unpublished artifacts from the
same exact source commit with distinct embedded versions. Linux installs the
second artifact through Debian's real upgrade hooks; macOS and Windows replace
the stopped service binary through their generated service-install paths. The
test requires both the public CLI and live service IPC to report the new version,
while node identity, signed-map trust, connected intent and fresh TCP traffic
remain unchanged. Subsequent repair and reinstall operations use the upgraded
artifact. This establishes native replacement behavior without claiming
compatibility between different source revisions, rollback, interrupted
upgrades, low-disk behavior or immutable release-manifest acceptance.

`TestContainerWorkload` adds a dedicated HC-054 acceptance boundary. A required
Ubuntu 24.04 container job receives only the TUN device and `NET_ADMIN`, builds
the tested Client source, and runs the real Client and workload inside that
container. The persistent case carries a fresh TCP payload, restarts the agent
with its saved identity and reconnects after endpoint change. The ephemeral case
carries traffic, crashes, consumes the provider's published terminal outcome and
requires a replacement workload to receive a different node identity. The job
is an explicit dependency of aggregate `verify`; it is separate from the common
24-run native inventory so ordinary platform reports cannot turn absence of a
container runtime into a successful skip. Host/sidecar separation, capability
denial and orchestrator-specific volumes/restarts remain separate variants.

The HC-063 installation path now continues after explicit state removal. It
reinstalls the native service, requires public IPC to report `NeedsEnrollment`
without a node, credential or cached map, and performs a new real CLI enrollment
against the contract testserver. The replacement service must publish a new node
identity and usable WireGuard runtime. Assertions never read deleted or recreated
private state. The old provider-side node deliberately remains outside local
reset semantics; a standalone reset command without uninstall is still a
separate product/interface decision.

The existing diagnostics root now exercises HC-057 crash recovery. After a
disconnected agent exports a schema-valid bundle, the harness terminates that
real process without graceful shutdown and starts it again. Public IPC must
retain the same node identity and disconnected intent, and a repeated export
must report the same unexpired artifact with `reused=true` and identical public
lifecycle metadata. The later expiry path still replaces only the aged bundle
and preserves unrelated operator files. Hosted qualification remains pending.

### 2026-09-12: limits of the Darwin default-route correction

Source `52edca1` expands each Darwin default route into two `/1` routes to
preserve the physical interface's `/0` during withdrawal. Its
[native CI run](https://github.com/endless-net/client/actions/runs/34700234967)
was still pending at this inspection; compilation is not native route evidence.
The follow-up component regression exercises failure of the second route
mutation for both addition and deletion, restoration of the previous route set,
and a successful retry. It must execute on Darwin CI before it is qualified.

Preserving `/0` does not itself preserve remote peer reachability while `/1`
routes are installed. `magicbind.go` opens ordinary UDP sockets;
`magicbind_mark_other.go` provides no non-Linux socket mark, and the Darwin
router configuration has no explicit endpoint exclusions. The current
`TestControlPlaneNativeExitRoute` uses a same-host reference peer, so success
does not prove that an off-host WireGuard endpoint or control server remains
reachable through the physical network. HC-036 therefore still needs a
Client-owned remote-underlay test and any corresponding routing correction.
This inspection does not establish the cause of the earlier macOS runner's
interface loss, nor does it establish that `52edca1` resolves that failure.

The installation workflow constructs `${initial_version}+ci.1` for its second
unpublished artifact. The user explicitly authorized adding `+ci.1` on
2026-09-12. This approval covers that test artifact suffix; HC-060 still requires
native qualification. The 50 common test roots are
an executable inventory, not a count of fully accepted HC scenarios or a
percentage of the 65-scenario objective.

The follow-up Darwin setup correction tracks only successfully added routes
for cleanup and rejects `file exists` rather than adopting an existing route.
Its component regression injects both an existing-route conflict and a
permission failure after the first `/1` was installed. Cleanup must remove that
first half, preserve the pre-existing second half, and never attempt to delete
a route whose installation was not reached. Native execution remains pending.

The completed [Windows 2025 repetition 3 job](https://github.com/endless-net/client/actions/runs/34698630395/job/103567055140)
for source `c5bd840` reports only `TestControlPlaneNativeMachineSharing` as a
failed root: IPv4 cannot reach the granted TCP service and IPv6 reports a local
endpoint binding mismatch. This is evidence for the older fixture failures
addressed by `47e62ec`, not evidence that the corrected fixture passes.

The [Windows 2025 repetition 1 job](https://github.com/endless-net/client/actions/runs/34698630395/job/103567055274)
and [Windows 2022 repetition 2 job](https://github.com/endless-net/client/actions/runs/34698630395/job/103567055161)
also fail `TestControlPlaneNativeExitRoute/ipv4`. On Windows 2025, the failure
occurs on the first approved-route traffic assertion; IPv6 passes. Thus the
exit-route failure is not confined to Darwin, and the Darwin route correction
cannot be considered a resolution of the Windows failure. The reference-hop
test now reports TCP and UDP outcomes separately and requires fresh forwarded
packet counts on each reachability phase, including recovery. This avoids
using packets from the initial connection as evidence of the recovered path.

HC-015 now restarts the already enrolled Client during a control-plane outage
after its original join token has been revoked. Public IPC must retain the same
node identity, node credential presence, connected intent and valid cached map,
and the real Client must carry fresh TCP traffic to the reference peer before
control is restored. The subsequent signed map update and replacement-token
enrollment retain their existing checks, including exactly two distinct
successful registrations for the whole scenario. This tests cached startup
independence from the revoked registration token; it does not cover expired
maps or expiry of the node credential. Hosted qualification remains pending.

HC-034 now has separate Linux `snat` and `preserve-source` subtests under the
existing common root. The latter omits `--advertise-snat`. The harness supplies
the router namespace's IP-forwarding setting and the external LAN's return
route as operator prerequisites. An INPUT policy on the external resource
permits loopback probes and the exact originating Client overlay address only,
so translated traffic cannot satisfy the TCP/UDP echo assertions. The scenario
also removes the return route, requires fresh TCP and UDP probes to fail, then
restores it and requires traffic to recover. Existing approval, withdrawal and
router-restart assertions run in both modes. This is executable source-preserving
Client forwarding coverage, not evidence of automatic host configuration in
no-SNAT mode. Linux hosted qualification remains pending; the non-Linux branch
continues to assert only the explicit unsupported SNAT result.

The report verifier requires both `TestControlPlaneSubnetRouter/snat` and
`TestControlPlaneSubnetRouter/preserve-source` to start and pass exactly once
in every Linux repetition. A successful parent with either child absent is
rejected. The requirement does not apply to the non-Linux unsupported-mode
branch. Component verifier tests cover both missing variants, complete absence,
and complete execution across all eight platform identifiers. Other scenario
subtest inventories still need an explicit requirement-by-requirement audit;
rejecting observed skips alone does not prove that every variant ran.

The [macOS ARM repetition 1 check](https://github.com/endless-net/client/actions/runs/34698630395/job/103567055181)
was cancelled by GitHub's 35-minute job timeout, as confirmed by its check
annotation. Its execution step never reached report conversion or upload.
Repetitions 2 and 3 are also cancelled; they are not qualified passes. The cause
of the long-running execution is not established by this annotation.

The workflow now bounds each native execution step at 31 minutes while keeping
the Go test deadline at 30 minutes. The outer job allows 45 minutes for setup,
execution termination and the existing `always()` report steps. A timed-out
execution still fails the job and cannot pass the aggregate gate. This is a
report-preservation change, not a fix or qualification of the stalled native
scenario. Artifact upload after timeout remains to be verified on hosted CI.

Inspection of the Client harness found an unbounded wait after forced process
termination in `Stop` and `StopWithSignal`. These paths now bound the final
process wait and mark the test failed if it does not finish. Client CLI/agent
commands and native test-trust commands also use a two-second `exec.Cmd.WaitDelay`
to bound pipe-copy waits when descendants retain output handles after process
exit or context cancellation. Existing command deadlines and graceful-shutdown
assertions remain intact. This removes identified harness wait hazards; without
the cancelled job's stack trace it is not proof of the macOS timeout cause.

### 2026-09-12: nineteen-report snapshot for `c5bd840`

The [run for `c5bd8409d8ef85d737f51faf09adde70f1b2358c`](https://github.com/endless-net/client/actions/runs/34698630395)
has 19 downloaded native reports at this inspection. Every report declares 50
root tests and the same source SHA. Their terminal root outcomes total **925
PASS, 25 FAIL, 0 SKIP**. Five repetitions have no uploaded report, leaving 250
of the full 1,200 expected outcomes unaccounted for; these are not passes or
inferred skips. The workflow was still active, so this is a partial snapshot.

| Report group | Reports | PASS | FAIL | Failed roots |
| --- | ---: | ---: | ---: | --- |
| Four Linux variants, all repetitions | 12 | 588 | 12 | Machine sharing in every report |
| Windows 2022, all repetitions | 3 | 144 | 6 | Exit route and machine sharing in every report |
| Windows 2025, all repetitions | 3 | 146 | 4 | Machine sharing in every report; exit route in repetition 1 |
| macOS Intel, repetition 3 | 1 | 47 | 3 | Exit route, flow consent and machine sharing |

`TestControlPlaneExitProvider` has exactly one root PASS and no failed/skipped
child in all 19 reports. This supplies Linux IPv4 provider dataplane evidence
for all 12 Linux repetitions and explicit unsupported-mode evidence for six
Windows and one macOS Intel repetition. It does not qualify the five absent
macOS repetitions, IPv6 provision, HA, or subsequent changes to the Client.
The full matrix remains failed/incomplete. The machine-sharing fixture changes,
Darwin route corrections, cached token-revocation startup, source-preserving
subnet mode and process-wait changes made after `c5bd840` require a later run.

The Darwin `/0` expansion also needs set-based reconciliation when the same map
contains an explicit `/1`. Route setup now deduplicates expanded prefixes;
updates compare the actual unique system-route sets. Removing `/0` preserves
an independently required `/1`. A failed update rolls back completed mutations;
if rollback also fails, the router retains the actual remaining routes for a
later recovery attempt. Component regressions cover overlapping IPv4/IPv6 route
lifecycles, partial application, and recovery after a failed rollback. Local
Windows checks do not execute these Darwin-only tests; hosted verification is
still required. Remote exit-peer underlay coverage remains a separate open gap.

Run `34698630395` is now terminal failure with the same 19 reports; the last
macOS Intel repetition was cancelled without uploading a report. The subsequent
[run for `92983ae`](https://github.com/endless-net/client/actions/runs/34701403560)
started its native matrix. Its [container job](https://github.com/endless-net/client/actions/runs/34701403560/job/103575195348)
failed during Go VCS status discovery before either container workload ran.
The build step now registers only `$GITHUB_WORKSPACE` as a safe Git directory
in the shell's own HOME and checks repository access before building. VCS
stamping remains enabled. This addresses the suspected checkout/shell HOME
mismatch; a successful subsequent container build and workload run are still
required before HC-054 has native evidence.

The container dependency list also includes `iputils-ping`: the reused TCP
lifecycle helper invokes the native ICMP probe before and during its traffic
checks. Missing `ping` is an execution failure, not a valid denied-packet result.
This dependency was found by source inspection before a successful container
workload run. No additional container capabilities are added by this change.

A follow-up HC-025 ledger audit found that the summary row still listed IPv6
listener/upstream and truncated-UDP TCP retry as missing, although
`TestControlPlaneDNSWireRecovery` already executes their eight combinations.
Each of the 19 downloaded `c5bd840` reports contains all eight successful leaf
outcomes (152 PASS leaves). The summary now reflects that evidence without
claiming the absent five reports or the current source are qualified. Existing
full-matrix historical DNS evidence above retains its original source boundary.

The aggregate report verifier now also requires all eight DNS wire leaves on
every platform and repetition. A passing DNS root with any listener/upstream/
truncation combination absent is rejected. Verifier regressions omit each leaf
in turn for every platform identifier and accept the complete set. This enforces
the audited DNS inventory without adding duplicate network scenarios.

The HC-021 summary likewise no longer lists TLS expiry as an unimplemented
variant. The 19 available `c5bd840` reports contain all four TLS validity leaves
as PASS (76 leaves): expired and not-yet-valid certificates, both before
enrollment and on a new connection after an enrolled agent restart. The source
asserts rejection before control HTTP, retained identity/intent, recovery after
a valid certificate is restored, and exactly one enrollment. It does not claim
retroactive invalidation of an already established TLS connection. These four
leaves are now mandatory in every platform report, with omission regressions
for each leaf. Current-source native qualification remains pending.

HC-036 failure diagnostics now query Windows `Find-NetRoute` for the resource
target and record default-route and interface metrics before the client stops.
The output projects only route prefixes, numeric metrics, and whether each route
uses the client interface. A bounded read-only diagnostic failure cannot replace
the original scenario failure. This distinguishes OS route selection from the
existing TCP/UDP and reference-forwarding observations; it does not establish
the cause of the previous Windows failures or change routing behavior. Short
tests, vet, and lint pass locally; execution of this diagnostic remains pending
in Windows native CI.

### 2026-09-12: first eight native reports for `92983ae`

The first eight downloaded contract artifacts from
[run 34701403560](https://github.com/endless-net/client/actions/runs/34701403560)
identify source `92983ae0adf6c0746e25c56ed027b8576e932f50`. Ubuntu 22.04 amd64
repetitions 1–2, Ubuntu 22.04 ARM repetitions 1–3 and Ubuntu 24.04 ARM repetitions
1–2 each have 50 PASS roots. Ubuntu 24.04 ARM repetition 3 has 49 PASS roots and
one FAIL: `TestControlPlaneNativeApplicationRoute/ipv6` reports `DNS response
unavailable`. The subtotal is 399 PASS, one FAIL and zero SKIP root outcomes;
other reports and the complete matrix remain pending.

All eight reports pass both machine-sharing address families and both Linux
subnet-router modes (16 successful leaves for each root). This qualifies the
earlier sharing fixture repair and source-preserving subnet checks only for
these executions, without asserting Windows/macOS results or the entire HC scope.

The sharing scenario now additionally replaces rights on the same grant and
peer: TCP 24001 is revoked while TCP 24002 and UDP 24001 become reachable;
UDP 24002 remains denied. Restoring the original rights must restore TCP 24001
and deny the replacement rights before final withdrawal. The positive probes
establish that previously denied services actually respond when authorized.
This extension passes local short-test compilation, vet and lint; native
execution is pending and is not included in the `92983ae` evidence above.

The next inspected artifact, `client-contracts-macos-15-3` from the same run
and exact source, has 50 PASS roots, zero FAIL/SKIP roots and successful package
completion. Both exit-route families and both sharing families pass. This is
the first inspected macOS ARM result for the Darwin route corrections, not
qualification of the other macOS repetitions, Intel runners or off-host exit
underlay behavior. Together with the eight reports above, the inspected subtotal
is 449 PASS roots and one FAIL across nine reports.

The Ubuntu 24.04 ARM application failure occurs at the final positive DNS check
after expiry and a fresh grant, after TCP access has recovered. The DNS helper
now retains its final transport error and attempt count instead of reporting
only `DNS response unavailable`; timeouts and exact DNS assertions are unchanged.
HC-040 positive phases now require fresh bidirectional connector packet counts,
so recovery cannot reuse the initial forwarding observation. Short tests, vet
and lint pass locally. Neither addition establishes the DNS failure's cause;
the strengthened native scenario remains pending in CI.

Three further inspected `92983ae` reports — Ubuntu 22.04 repetition 3, Ubuntu
24.04 repetition 2 and macOS Intel repetition 1 — each contain 50 PASS roots,
no FAIL/SKIP roots and successful package completion. The inspected subtotal is
now 599 PASS and one FAIL across 12 reports. Both macOS architectures therefore
have one complete successful repetition; the remaining native repetitions are
still required. Linux, Windows and macOS component verification jobs also pass
in this run. None of these results qualifies later source changes.

HC-040 now retires the reference connector's UDP endpoint while keeping its
key, application and route lease. New application probes must fail against the
retired endpoint; a signed map changing only the peer endpoint must restore TCP
access with fresh bidirectional forwarding and the exact DNS answer. Both IP
families execute this phase. This covers recipient recovery from a stale
connector endpoint, not connector process restart, automatic health discovery
or multi-connector failover. Local short tests, vet and lint pass; native
qualification of the added phase remains pending.

### 2026-09-12: sixteen inspected native reports for `92983ae`

The same run now provides all twelve Linux repetitions, macOS ARM repetition 3,
macOS Intel repetition 1, and Windows 2022 repetitions 1 and 3. Each inspected
artifact identifies `92983ae0adf6c0746e25c56ed027b8576e932f50`.

| Inspected group | Reports | PASS roots | FAIL roots | SKIP roots |
| --- | ---: | ---: | ---: | ---: |
| All Linux variants and repetitions | 12 | 599 | 1 | 0 |
| macOS ARM repetition 3 and Intel repetition 1 | 2 | 100 | 0 | 0 |
| Windows 2022 repetitions 1 and 3 | 2 | 97 | 3 | 0 |
| Total inspected | 16 | 796 | 4 | 0 |

Both inspected Windows reports fail IPv4 exit routing with TCP=false,
UDP=false and reference forwarded=0/0 before=0/0. This localizes the failure
before resource forwarding but does not distinguish OS route selection from
the tunnel path. The Windows route-selection diagnostic added in `9772335` is
not present in this source and still requires its own native execution.

Windows 2022 repetition 1 also fails application-route IPv4 at its first map
application wait: public IPC has revision 1, zero peers, WireGuard not ready
with an error and no agent projection. This is an initialization/readiness
failure, distinct from Ubuntu ARM's final recovery DNS timeout. Do not count
the DNS diagnostic change as a fix for either failure. Eight native reports
remain uninspected or pending; the full matrix is not qualified.

Windows 2025 repetition 2 also reports a failed TCP probe in the flow-consent
expiry observation loop, with zero fresh packets at the reference peer while
public IPC still reports a valid map, ready WireGuard and a handshake. The loop
spans time both before and after expiry: its old failure wording is not proof
that expiry caused the loss or that the failing probe started after expiry.
The scenario now reports probe start/end relative to expiry and successful probe
counts on each side. Completion also explicitly requires a successful probe
started after expiry. Existing no-report and window-bound assertions remain.
Local short tests, vet and lint pass; native execution of the change is pending.

The aggregate verifier now requires 22 additional native leaves on every
platform/repetition: IPv4 and IPv6 application routes, exit routes, routed
resources, machine sharing and service catalogs; all four address/protocol
combinations for flow consent; and all eight logout/local-forget combinations.
Omission regressions remove each required leaf in turn on every platform and
verify rejection despite a passing parent, while accepting complete reports.
These checks enforce existing scenario variants and do not substitute for their
native execution. Local short tests, vet and lint pass.

The aggregate `verify` job now inspects contract artifacts before checking
dependency conclusions, so a failed platform no longer prevents report
validation from running. Verifier diagnostics are included in the job summary.
The final dependency check runs even after report validation fails and still
requires every component, installation, control-plane, native-platform and
container job to succeed. Either a bad report or a failed dependency keeps the
gate red. This workflow-only reorder was reviewed with `git diff --check`;
its hosted behavior remains pending on its own source revision.

Report validation now continues across all 24 platform/repetition slots and
returns their combined errors instead of stopping at the first bad artifact.
Source identity, compiled inventory, mandatory leaves and complete execution
remain required. A regression places a wrong source in Linux, a failed test in
Windows and a missing execution report in macOS simultaneously; the aggregate
must retain all three errors and return no verified scenario count. Complete
valid reports still pass. Local short tests, vet and lint pass; hosted aggregate
execution on this source remains pending.

### 2026-09-12: all eight installation reports for `92983ae`

All eight `installation-*` artifacts from
[run 34701403560](https://github.com/endless-net/client/actions/runs/34701403560)
were downloaded and inspected. Each contains exactly one successful
`TestInstalledClient`, four successful subtests (fresh installation, disconnected
service restart, enrolled reinstall/upgrade and uninstall), the connected enrolled
upgrade phase, and no FAIL or SKIP outcomes. The run identifies source
`92983ae0adf6c0746e25c56ed027b8576e932f50`; unlike the contract artifacts, these
installation logs do not contain independent source/shard files.

This qualifies the implemented HC-060/063/064 upgrade, identity/intent retention,
traffic, explicit state removal, distinct reenrollment and uninstall assertions
on the eight specified runners. It does not qualify their remaining failure
variants, later source commits, or the still-failing contract matrix.

### 2026-09-12: completed native matrix for `92983ae`

[Run 34701403560](https://github.com/endless-net/client/actions/runs/34701403560)
is terminal with conclusion `failure`; aggregate job `verify` also failed.
All 24 contract artifacts were downloaded. Source and repetition identities
match, every artifact has a package terminal outcome, and no test/subtest is
skipped. The 50 common roots produce the following outcomes:

| Platform group | Reports | PASS roots | FAIL roots |
| --- | ---: | ---: | ---: |
| Ubuntu 22.04 amd64 | 3 | 150 | 0 |
| Ubuntu 24.04 amd64 | 3 | 150 | 0 |
| Ubuntu 22.04 ARM | 3 | 150 | 0 |
| Ubuntu 24.04 ARM | 3 | 149 | 1 |
| Windows 2022 | 3 | 146 | 4 |
| Windows 2025 | 3 | 147 | 3 |
| macOS ARM | 3 | 150 | 0 |
| macOS Intel | 3 | 149 | 1 |
| Total | 24 | 1191 | 9 |

The failed roots are: IPv4 exit routing in all three Windows 2022 repetitions
and Windows 2025 repetitions 2–3; application-route initialization in Windows
2022 repetition 1; application-route recovery DNS in Ubuntu 24.04 ARM
repetition 3; and flow consent in Windows 2025 repetition 2 and macOS Intel
repetition 2. Windows 2025 repetition 1 passes all roots, so its exit-routing
result varies between runners. The macOS Intel flow root has two failing leaves:
IPv6 TCP loses reachability and the subsequent IPv6 UDP setup finds no usable
IPv4 underlay interface. This does not establish why the interface became
unavailable or connect that loss causally to consent expiry.

Installation passes on all eight runners as documented above. The separate
container lifecycle job fails during Go VCS stamping before its scenarios run;
later container checkout/dependency fixes remain unqualified by this source.
No full-matrix acceptance or 65-scenario completion is claimed. The next queued
[run 34704108878](https://github.com/endless-net/client/actions/runs/34704108878)
targets `cce41e6a3b91fd322f7bb9a91125a1dd3bc251c0` and includes the accumulated
diagnostics, scenario extensions and aggregate-verifier changes; its results
must be evaluated separately.

### 2026-09-12: native durable MTU preference scenario

`TestControlPlaneNativeMTUPreference` uses the real enrollment and offline-sync
CLI with an agent restart between mutations. Each IPv4/IPv6 variant observes
MTU 1280 after enrollment, 1400 after a saved change, retained 1400 after rejected
1279, and the documented default 1420 after saving 0. Each phase requires the
original node identity, ready public IPC, agreement with the OS interface MTU,
and fresh TCP and UDP application exchanges through the reference peer. No
saved configuration or identity material is read by the test.

Both address-family leaves are required in every platform report, with omission
regressions in the aggregate verifier. Local short tests, vet and lint pass.
The native scenario is not present in `92983ae` or the running `cce41e6` matrix;
its own hosted execution remains pending. It tests durable preferences across
process restart, not live MTU changes or path-MTU/fragmentation behavior.

### 2026-09-12: first successful container lifecycle evidence

[Container job 103581615827](https://github.com/endless-net/client/actions/runs/34704108878/job/103581615827)
for source `cce41e6a3b91fd322f7bb9a91125a1dd3bc251c0` passes
`TestContainerWorkload` in 66.75 seconds, with successful persistent-state-restart
and ephemeral-recreation subtests and no skips. This qualifies the checkout
trust/dependency repairs and the initial IPv4 TCP workload checks. The job runs
inside Ubuntu 24.04 with NET_ADMIN and `/dev/net/tun`; it does not recreate the
container itself or qualify a sidecar/container-orchestration lifecycle.

The persistent workload now runs all four IPv4/IPv6 TCP/UDP variants, including
the existing lifecycle, policy and signed endpoint-change checks. Ephemeral
recreation retains its IPv4 TCP evidence boundary. Local short tests, vet and
lint pass; the three added persistent variants require a later native container
run and are not covered by the successful job above.

### 2026-09-12: expiry of previously accepted cached authority

`TestControlPlaneNativeCachedMapExpiry` covers IPv4 and IPv6 through a real
Client and reference peer. A short-lived, valid signed map first permits TCP
and UDP while control is unavailable. After signature expiry, fresh traffic
must fail while public IPC retains node credentials and connected intent but
marks the cached map invalid. Offline CLI reuse must fail; an agent restart
must not restore access. A fresh signed map after control recovery must restore
the same identity and both application protocols.

The participant's `SetMapValidity` publishes an explicit signature window through
the existing map contract. Its regression applies the same wire event through
the published contract before and after expiry, proving acceptance then denial
without sleeping or reading Client internals. Both native family leaves are
mandatory in every platform report. Local short tests, vet and lint pass; native
expiry/retirement/recovery is executable but unqualified until its own CI run.
This does not cover node-credential expiry or application/route-specific leases.

### 2026-09-12: Windows exit route metric collision

[Windows 2022 repeat 2](https://github.com/endless-net/client/actions/runs/34704108878/job/103581615692)
and [repeat 3](https://github.com/endless-net/client/actions/runs/34704108878/job/103581615709)
at source `cce41e6a3b91fd322f7bb9a91125a1dd3bc251c0` fail the IPv4 exit
traffic check. Failure diagnostics identify a selected physical `/0` with route
metric 0 and interface metric 10; the Client `/0` has route metric 5 and
interface metric 5. Both effective metrics are 10, and reference forwarding
remains zero. These observations establish the wrong route selection in those
two jobs; they do not explain unrelated application or consent failures.

Windows now expands each approved default into two more-specific `/1` routes,
using the same helper as Darwin, whose expansion behavior is unchanged.
Updates compare expanded route sets so an explicit overlapping `/1` survives
default withdrawal and duplicate halves are not installed. IPv4/IPv6 regression
checks cover installation, transitions and withdrawal on the Client interface.
Local short tests, vet and lint pass; native execution of this fix is pending.
Preserving the physical `/0` does not establish off-host tunnel/control endpoint
bypass: separate underlay routing and its evidence remain required.

### 2026-09-12: Windows initial application-map wait diagnostics

[Windows 2022 repeat 1](https://github.com/endless-net/client/actions/runs/34704108878/job/103581615797)
at `cce41e6a3b91fd322f7bb9a91125a1dd3bc251c0` also fails IPv4 exit traffic:
the selected physical default has effective metric 5 versus Client metric 10.
The `/1` change addresses precedence independently of either metric value;
native verification remains pending.

The same job fails the first IPv4 application-map wait after 15 seconds, with
600 successful IPC responses, revision 1, no peers or agent snapshot, and an
unsuccessful WireGuard inspection. This reproduces the initial-application
failure class from `92983ae`; it does not establish a DNS or grant-policy defect.
The harness now counts public IPC inspection states throughout each wait and
reports those counts on failure. Exact known messages distinguish an operation
in progress from an engine not running; other error text is withheld. A short
regression ensures an arbitrary suffix on a known message is also withheld.
No readiness condition or deadline changes. Local short tests, vet and lint
pass; the additional native diagnostic evidence is pending.

### 2026-09-12: completed cce41e6 native matrix

[Run 34704108878](https://github.com/endless-net/client/actions/runs/34704108878)
completed with failure at source `cce41e6a3b91fd322f7bb9a91125a1dd3bc251c0`.
All 24 reports are available, with the same 50-root compiled inventory and
matching source identity. Results total **1196 PASS, 4 FAIL, 0 SKIP**:

| Native runner | Three-repeat root PASS / FAIL |
| --- | --- |
| Ubuntu 22.04 amd64 | 150 / 0 |
| Ubuntu 24.04 amd64 | 150 / 0 |
| Ubuntu 22.04 ARM64 | 150 / 0 |
| Ubuntu 24.04 ARM64 | 150 / 0 |
| Windows 2022 | 146 / 4 |
| Windows 2025 | 150 / 0 |
| macOS 15 ARM64 | 150 / 0 |
| macOS 15 Intel | 150 / 0 |

Windows 2022 IPv4 exit routing fails in all three repeats; initial IPv4
application-map readiness also fails in repeat 1, as detailed above. All other
roots pass in all 24 reports, including live sharing-rights replacement and the
expanded flow-consent observation. Application endpoint recovery is successful
in the other 23 reports; it is not qualified across every repeat. Previous
Ubuntu application DNS recovery and macOS flow failures do not recur in this
run, which alone does not prove their root causes have been resolved.

All eight installation jobs and the initial container lifecycle job pass.
The aggregate verifier reports all three failed Windows shards and rejects the
run. The later MTU and cached-map-expiry roots, expanded container transports,
Windows `/1` fix and inspection-state diagnostics are absent from this source
and remain pending on a subsequent run. These root counts do not represent a
percentage of complete HC-001–HC-065 coverage or release acceptance.

### 2026-09-12: join-token rotation across address families and transports

`TestControlPlaneJoinTokenRotation` now runs IPv4 and IPv6 variants. Both require
fresh TCP and UDP application exchanges and bidirectional reference forwarding
before rotation, after old-token enrollment rejection, after the existing Client
restarts during control unavailability, and after control recovery. Existing
node identity and authority must survive; the replacement token still enrolls
exactly one distinct Client. Both family leaves are mandatory in CI reports.
Short tests, vet and lint pass. This extends the qualified IPv4 TCP scenario;
native execution of the new variants is pending. Join-token expiry and provider
token-lifecycle implementation are not established by this rotation scenario.

### 2026-09-12: container IP and transport variants qualified

[Container job 103587387308](https://github.com/endless-net/client/actions/runs/34706279754/job/103587387308)
at source `169b09c91a010cf2c850a6bcd58c366de2b26d9c` passes
`TestContainerWorkload` in 188.08 seconds. All four persistent-state-restart
leaves (`ipv4/tcp`, `ipv4/udp`, `ipv6/tcp`, `ipv6/udp`) and the existing IPv4 TCP
ephemeral-recreation leaf pass without skips. This qualifies the transport
expansion after `cce41e6`, including its lifecycle, policy and endpoint-change
checks inside Ubuntu 24.04 with NET_ADMIN and `/dev/net/tun`. Process restart
inside the same container does not establish actual container recreation,
sidecar integration, other network modes or orchestration lifecycle behavior.

### 2026-09-12: expired map authority also gates ordinary peer traffic

Code inspection found that the application packet filter allowed ordinary
unprotected peer packets before reaching its map-signature expiry check.
`TestExpiredMapDeniesOrdinaryPeerPackets` reproduced this defect for IPv4 and
IPv6 before the fix. The filter now checks signed-map expiry before classifying
destinations, covering both inbound and outbound traffic independently of
control synchronization. The regression checks TCP and UDP immediately before,
at and after the deadline, plus recovery under a fresh map expiry. This is a
component-level packet-filter proof; the public-contract
`TestControlPlaneNativeCachedMapExpiry` remains the native acceptance test for
the running Client, offline CLI rejection, restart and control recovery.
The fix does not by itself prove OS route retirement, credential expiry or
release acceptance, and is absent from the running `169b09c` matrix.

### 2026-09-12: first native cached-map expiry failure

[macOS Intel repeat 1](https://github.com/endless-net/client/actions/runs/34706279754/job/103587387376)
at `169b09c91a010cf2c850a6bcd58c366de2b26d9c` reports 50 passing and two failing
roots out of 52. `TestControlPlaneNativeCachedMapExpiry` reaches the expired
public-cache state but still observes application traffic for both IPv4 and
IPv6. This supplies real-Client evidence of the expiry bypass reproduced by
the component regression; fix `d97b4ab` is not included in this run. MTU
preference persistence, rejection and recovery pass for both address families
in this report, without establishing the remaining platform repetitions.

The other failed root is native flow consent: an IPv4 TCP exchange fails after
earlier reference forwarding, and the three subsequent variants cannot find a
usable IPv4 underlay. The interface inventory reports the physical interfaces
without IPv4; the remaining IPv4 interface is point-to-point. This is a repeated
failure class requiring investigation, not proof that Client configuration or
consent expiry caused the underlay loss. The other matrix reports are pending.

### 2026-09-12: independent node restart with an expired user session

`TestControlPlaneSessionExpiryRecovery` now runs IPv4 and IPv6 with fresh TCP
and UDP exchanges and bidirectional reference forwarding. In addition to
denying expired-session user RPCs, retaining node authority and recovering user
RPCs after reauthentication, it restarts the real agent before reauthentication.
The restarted node must consume a new signed map and carry traffic while the
user RPC remains denied. The existing post-reauthentication restart and
exactly-one-registration assertion remain. Both family leaves are required in
CI reports. Short tests, vet and lint pass; these expanded native variants are
pending and are absent from the running `169b09c` matrix. Browser/OIDC refresh
and provider session-expiration scheduling remain outside this evidence.

### 2026-09-12: Windows exit-route fix qualified in all six repeats

All three Windows 2022 and all three Windows 2025 reports in
[run 34706279754](https://github.com/endless-net/client/actions/runs/34706279754)
at `169b09c91a010cf2c850a6bcd58c366de2b26d9c` pass
`TestControlPlaneNativeExitRoute/ipv4` and `/ipv6`. This qualifies the `/1`
route-precedence fix against the same-host reference egress, withdrawal and
recovery scenario that failed all three Windows 2022 repeats at `cce41e6`.
Remote tunnel/control endpoint bypass and public egress observation remain
unqualified; this evidence does not establish all HC-036 variants.

Application-route readiness also passes all six Windows repeats; its earlier
intermittent startup failure is not thereby explained. MTU preference tests
pass both families in all six reports. All Windows reports still fail cached
map expiry, and Windows 2025 repeat 1 additionally fails an IPv6 TCP exchange
3.752 seconds before consent expiry. The expiry packet-filter fix and later
flow diagnostics are absent from this source. The overall run remains
unqualified while the other native reports are pending.

### 2026-09-12: completed 169b09c native matrix

[Run 34706279754](https://github.com/endless-net/client/actions/runs/34706279754)
completed with failure at `169b09c91a010cf2c850a6bcd58c366de2b26d9c`.
All 24 reports have matching source identity, the same 52-root inventory,
terminal package results and no skips: **1222 PASS, 26 FAIL, 0 SKIP**.

| Native runner | Three-repeat root PASS / FAIL |
| --- | --- |
| Ubuntu 22.04 amd64 | 153 / 3 |
| Ubuntu 24.04 amd64 | 153 / 3 |
| Ubuntu 22.04 ARM64 | 153 / 3 |
| Ubuntu 24.04 ARM64 | 153 / 3 |
| Windows 2022 | 153 / 3 |
| Windows 2025 | 152 / 4 |
| macOS 15 ARM64 | 153 / 3 |
| macOS 15 Intel | 152 / 4 |

Cached-map expiry fails all 24 repeats, with both address-family leaves
observing continued traffic after public cache invalidation (48 reproductions).
The other two failed roots are flow consent on macOS Intel repeat 1 and Windows
2025 repeat 1, detailed above. All other 50 roots pass every repetition. This
qualifies the new MTU scenario and the corrected same-host exit-route scenario
on all eight native runners; it does not prove all HC variants or release
acceptance. The aggregate `verify` job fails as required.

All eight installation jobs and the expanded container lifecycle pass. The
packet-filter expiry fix `d97b4ab`, join-token/session transport expansions and
later flow diagnostics are absent from this source and need their own native
matrix. Existing evidence snapshots retain their original outcomes and limits.

### 2026-09-12: DNS diagnostics track the live signed projection

`TestControlPlaneNativeDNSMapUpdates` now reads public IPC diagnostics after
each applied address change, peer rename, withdrawal and restoration. It
requires the current map revision, consistent record counts and exactly the
expected peer name, FQDN and IPv4/IPv6 addresses, alongside the existing real
UDP/TCP A/AAAA answers. Withdrawn or obsolete peer records must not remain in
the diagnostic summary. No private Client state is read. Short tests, vet and
lint pass; this extension is pending native execution and is absent from
`90f8a28`. The summary contract describes configuration, not upstream health;
these checks do not establish DNS/peer-path/application failure classification.

### 2026-09-12: stalled unary IPC request and recovery

`TestControlPlaneCLIIPCUnaryTimeout` runs the shipping `service status` CLI
against the published HTTP IPC endpoint over a Unix socket or Windows named
pipe. The participant stalls before response headers or after a partial JSON
body. With a one-second request timeout, the CLI must return exit 1 with a
diagnostic and no result, independently of the outer process watchdog; the
request-to-exit bound allows two additional seconds for runner scheduling.
A subsequent invocation at the same endpoint must return valid typed status
after the participant repairs its response. Both leaves are required in native
CI reports. Short tests, vet and lint pass; hosted execution is pending. This
tests Client timeout handling, not how a real agent becomes stalled, and does
not replace event-stream or mutation-side-effect acceptance.
