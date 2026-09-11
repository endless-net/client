# Headless client coverage ledger

Status: in progress. This is a coverage audit, not a completion claim.

Scope: [HC-001–HC-065, architecture main](https://github.com/endless-net/architecture/blob/main/docs/ru/headless-client-use-cases.md),
[business requirements](https://github.com/endless-net/architecture/blob/main/docs/ru/headless-client-business-analysis.md),
and [system design / IT specifications](https://github.com/endless-net/architecture/blob/main/docs/ru/headless-client-system-design.md).

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
- **R**: real service/client pairs and product acceptance on an immutable manifest;
  owned jointly with [system-tests, main](https://github.com/endless-net/system-tests/tree/main).

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
| HC-009 | C scenario enrollment; D proof validation | Independent machines, wrong/expired join authorization |
| HC-010 | C RegistrationResponseLoss; D ResponseLossPreservesOperation | Real producer replay/conflict and distinct-machine batch |
| HC-011 | No C/R evidence audited | Decide ephemeral lifecycle, then normal/crash expiry tests |
| HC-012 | D request/proof tests | Producer-owned allowed/denied attributes and effective access |
| HC-013 | C BrowserEnrollment approve/reject | Pending must not authorize traffic; completion binding |
| HC-014 | U recovery matrix | C/R session expiry, reauthentication and preservation |
| HC-015 | D credential revocation | Revoke join authorization separately from existing Node |
| HC-016 | C initial cached-map status | Actual allowed application traffic |
| HC-017 | C Lifecycle disconnect/restart | Data flow stops/resumes without new identity |
| HC-018 | C disconnected intent survives restart | Connected intent, reboot and unavailable-network variants |
| HC-019 | U configuration tests | Public preference mutation and observed effect |
| HC-020 | No C/R evidence audited | Product decision for profiles, isolation and switching |
| HC-021 | U control-endpoint security | Public origin/trust changes and wrong endpoint |
| HC-022 | C Lifecycle logout; U typed logout | Remote cleanup unconfirmed, local forget, profile semantics |
| HC-023 | C Lifecycle peer projection | Authorized listing and peer absence semantics |
| HC-024 | U encrypted engine tests | Two real client processes and application probes |
| HC-025 | C DNSProjection; U resolver tests | Real resolver and allowed/denied resource |
| HC-026 | U DNS/router configuration | External DNS control and IP-access preservation |
| HC-027 | C Lifecycle peer/ACL updates | Wire deltas, bad base/hash and real withdrawal of traffic |
| HC-028 | U direct/Relay implementations | R direct versus forced Relay with positive/negative probes |
| HC-029 | U endpoint/reconnect tests | External network change and stale response ordering |
| HC-030 | C TemporaryFailurePreservesEnrollment | Full typed/malformed error matrix and offline lease limits |
| HC-031 | U ACL enforcement | Local restriction through IPC with packet observations |
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
| HC-065 | C RejectsInvalidMaps terminal revoke; D revoke | R offline/online retirement and separately verified local removal |

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
retention. Real client execution still awaits this increment's CI.

## Next work

Verify peer delta recovery in CI, then extend C with real peer probes.
Run producer conformance in owning service repositories. Keep contract gaps and
platform decisions explicit; do not replace unresolved HC rows with generic smoke
tests or infer broad completion from this initial increment.
