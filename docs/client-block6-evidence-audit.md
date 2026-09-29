# Block 6 evidence audit (updated 2026-09-28)

Source: completed system evidence through manual dispatch `36127380078` on
`main` at `3f9f432`; later fixture and diagnostic corrections, short-unit
evidence through `76481a5`, and manual system evidence through dispatch
`36540037972` on `main` at `687cdf3` (ledger updated 2026-09-29). This remains an
open requirement and acceptance ledger, not a declaration of cutover completion.
The normative IT-01–33 and BR/AC-01–20, RULE-01–16 sources are the pinned
architecture revision `bdb5ba63c0e5356122c0760f4d63205e84ef507d`; the
local US-01–14 source is the pinned client-ui revision in
[the local map](client-local-requirement-map.md). Existing maps name unit-test
entry points. They do not establish the assertions or real effects below.
The later [D-035](https://github.com/endless-net/architecture/blob/e3c40f6/docs/ru/decisions/d-035.md)
and [update-source design](https://github.com/endless-net/architecture/blob/e3c40f6/docs/ru/client-update-source-and-pairing.md)
add an approved UI-Q10 *channel direction* and a still-proposed trust/discovery
design; they do not revise the pinned IT or BR/AC acceptance baseline or supply
runtime evidence. Current architecture `main` (`e3c40f6`) adds an explicit
mapping from D-035 to proposed business requirement EN-R-08. EN-R-08 requires
installing, updating or restoring an accepted platform distribution and then
confirming real resource access; the accepted UI/core pair must be decided by
business acceptance. This mapping clarifies the acceptance dependency without
making the proposed trust/discovery design approved or providing implementation
or runtime evidence.
The Client UI consumer SA was refreshed in the [local map](client-local-requirement-map.md)
to [b03fd54](https://github.com/endless-net/client-ui/blob/b03fd540789a3a33fce26b9203888b899ce311f1/docs/client-ui-system-analysis.md),
the latest edit to that path. Current architecture `main` is
[e3c40f6](https://github.com/endless-net/architecture/commit/e3c40f6df91e13cfc3256b5195b2a8d73f1a3aa9).
The update-source design remains unchanged from `9ff22c1`; D-035 only gains the
EN-R-08 mapping. Its trust/discovery proposal and owner dependencies remain open.

## Evidence available now

- Push [35828079929](https://github.com/endless-net/client/actions/runs/35828079929)
  passed the Linux, Windows 2025, macOS ARM and Intel **short unit** matrix at
  `7c3d486`. Earlier [35822315478](https://github.com/endless-net/client/actions/runs/35822315478)
  failed on Ubuntu with `failed to update fwmark: use of closed network
  connection`. Later passes have not identified the closure cause.
- Push [35832270453](https://github.com/endless-net/client/actions/runs/35832270453)
  passed all four short unit jobs at `93d854e` after the initial bind-mark
  change. One green Linux run does not establish that every closed-bind path
  causing the earlier intermittent failure has been removed.
- Push [35851486488](https://github.com/endless-net/client/actions/runs/35851486488)
  passed the same four short unit jobs at `000fa30`; it did not run native
  cross-network, LAN_ALLOW, installer or update-source scenarios.
- Push [35852868522](https://github.com/endless-net/client/actions/runs/35852868522)
  passed the same four short unit jobs at `16fe207`. The profile/network exit
  ownership guards therefore have Linux, Windows 2025, macOS ARM and Intel
  short-unit evidence; native kernel containment and combined transition
  acceptance remain outstanding.
- The documentation-only push at `26f44c5` had one Windows short-unit failure:
  `TestAgentNativeRPCHostBootstrapAndStop` received `STALE_STATE` on Disconnect.
  Rerunning only that job on the same SHA passed. The test had reused a
  revision from an earlier operation instead of fetching a fresh snapshot for
  the successful mutation; it now fetches the profile catalog immediately
  before Disconnect and retains a separate deliberately stale request. This
  corrects the test's compare-and-set input, but the one-off runner failure
  does not establish the cause of every possible revision race.
- Push [36106109151](https://github.com/endless-net/client/actions/runs/36106109151)
  passed all four short unit jobs at `0c70a54`, including the fresh-snapshot
  native CLI Disconnect scenario on Windows 2025.
- Push [36107626963](https://github.com/endless-net/client/actions/runs/36107626963)
  passed all four short unit jobs at `a9b2eca`. The new cross-network system
  scenario compiles but is skipped by the short-test gate; this run does not
  establish that the native switch executes successfully.
- Push [36109476988](https://github.com/endless-net/client/actions/runs/36109476988)
  passed all four short unit jobs at `65f1303` after adding the native profile
  context scenario. Its non-push contract, installer, verify, container, STUN
  and control-plane jobs were skipped. The profile scenario compiled but was
  skipped by `-short`, so it has no execution result.
- `test.yml` runs installation, eight-platform contract scenarios, isolated
  dataplane and container jobs only for non-push events. No result from those
  jobs at the current source was inspected in this audit. Push success is not
  native or installed-artifact evidence.
- `tests/control_plane_exit_route_test.go` explicitly checks **no implicit
  exit**; it does not exercise SelectExitNode or LAN_ALLOW. No test in `tests/`
  mentions LAN_ALLOW. `tests/control_plane_network_selection_test.go` covers
  current-ID reselection and account-catalog denial, not cross-network
  handover. These are concrete system-coverage holes.
- Diagnostics now projects the existing v0 `default_route_present` field from
  bounded Linux/macOS route-table reads and Windows IP Helper route tables;
  failure remains explicitly unobserved. This adds no IPC/schema version.
  `TestObserveLinuxDefaultRoutes`, `TestObserveDarwinDefaultRoutes` and
  `TestZeroWindowsRoutePrefix`,
  `TestRPCDiagnosticsProjectsDefaultRouteOnlyWhenObserved` cover parsing,
  completeness and projection. The four allowed local checks passed, and push
  [36112986842](https://github.com/endless-net/client/actions/runs/36112986842)
  passed all four platform short-unit jobs, including Windows prefix tests.
  Runtime/system checks remain outstanding.
  Resource observations and OS resolver readback remain unavailable, and
  platform runtime behavior still needs acceptance. `service_rpc_update.go`
  returns SOURCE_UNAVAILABLE; this is not full diagnostics/distribution
  acceptance.

## Per-IT assertion and acceptance matrix

Production entry points and unit entry points for **every row** are in
[the headless map](client-headless-requirement-map.md#per-it-client-trace).
`+` is a positive result to prove; `−` is denial/invalidation; `R` is a
restart or concurrency boundary. The named tests are starting points for
assertion review, not row-level passes. All rows remain open.

| IT | Concrete assertion still requiring review or execution | Required evidence beyond existing unit entry points |
| --- | --- | --- |
| 01 | + approved enrollment sync; − pending/denied cannot use credentials or map; R poll/completion keeps original operation | Pinned producer approval and live connection |
| 02 | + own valid authority; − foreign/expired poll and expired session cannot register; R denial cannot alter prior node | Producer rejection and authorization trace |
| 03 | + completion binds device/request/registration; − changed binding or missing registration never applies; R late completion after cancellation | Producer G-02 semantics and live observation |
| 04 | + separate machines obtain separate node authority; − wrong account/key/attributes; R repeated registration cannot clone identity | Producer and two-node traffic |
| 05 | + lost response reuses exact request; − changed payload conflicts; R same outcome after process restart | Producer replay/idempotency evidence |
| 06 | + existing node evaluated independently; − revoked join basis cannot create a node; R revocation during registration | Backend admin/revocation evidence |
| 07 | + signed map projects both peers and nonce flow; − foreign network map; R map replacement withdraws old projection | Two-client bidirectional traffic |
| 08 | + valid delta advances effective state; − replay cannot restore withdrawn right; R checkpoint restart | Producer delta and real withdrawal flow |
| 09 | + full snapshot recovery; − wrong base/vector/gap cannot advance cursor or permit flow; R stream interruption | Producer stream and denied traffic |
| 10 | + valid control map accepted; − tamper, identity, signature and expiry each deny new rights; R clock/restart | Native denial and accepted control case |
| 11 | + resume from confirmed base; − partial frame/EOF never adopted; R missing base forces full state | Producer stream/restart evidence |
| 12 | + direct and relay nonce echo; − forbidden pair/direction; R endpoint loss/recovery | Real two-client direct/relay path |
| 13 | + valid relay credential/direction; − invalid credential, reverse and foreign network/private caller; R credential rotation | Relay producer authorization and traffic |
| 14 | + new endpoint restores flow; − stale response cannot roll context back; R network change mid-request | Platform network handover and producer timing |
| 15 | + allowed control flow stays live; − revoked ACL/inbound flow stops within agreed bound; R active TCP/UDP and map expiry | Native packet observation and revoke bound |
| 16 | + approved DNS/route reaches exact address/port; − unapproved/removed route cannot; R resolver/route change and restart | OS resolver, route and traffic readback |
| 17 | + authorized discovery remains lease-bound; − wrong node/hash/expired report grants no traffic; R stale report after replacement | Producer acceptance and connector traffic |
| 18 | + consent-bound recipient/direction flow; − revoke/expiry/new node loses grant; R replacement during flow | Producer consent and real sharing flow |
| 19 | + exact confirmed key enables verified renewal; − different key/no privilege/no confirmation preserves old trust; R interrupted adoption | Native elevated helper and producer trust rotation |
| 20 | + typed temporary/terminal/policy result; − malformed/401/403/timeout cannot enroll implicitly; R retry retains authority | Pinned producer error matrix |
| 21 | + renewal reuses original identity after lost reply and enables new flow; − stale/miscorrelated result; R process restart | Producer renewal wire trace and traffic |
| 22 | + Disconnect/new context wins; − delayed old reply cannot restore access; R response released after stop/switch | Cross-worker and native route isolation |
| 23 | + confirmed logout differs from local forget; − transport failure cannot claim remote cleanup; R renewal drain/replay | Backend cleanup result and native traffic |
| 24 | + authorized current node works; − revoked old node fails, offline lease stays bounded; R reconnect | Producer revocation and two-client traffic |
| 25 | + consent window yields one durable receipt; − expired/revoked/duplicate window; R lost response replay | Producer receipt and bounded flow export |
| 26 | + same request/payload returns one outcome; − changed payload conflicts; R lookup after restart for **each** mutation kind | Full mutation-kind journal audit and transport |
| 27 | + opening snapshot precedes events; − observer sees no private IDs/deadlines; R concurrent Connect, overflow and reattach | OS-local owner/observer transport |
| 28 | + one active profile and valid target; − stale profile response/active removal/mixed profile routes; R profile switch after crash | Native profile context scenario now covers registered→empty→restart→restore with stable NodeID; stale response, active removal and mixed-route assertions remain. Cross-network handover is US-04; its late-response boundary is IT-22 |
| 29 | + independent session/node clocks; − unknown expiry not infinite or seamless; R interrupted renewal | Native timing and producer renewal |
| 30 | + explicit Select/Apply/Contain/Clear, exact LAN family result; − path loss, overlap, lock, expiry and no implicit default; R boot/namespace/ownership recovery | Linux kernel verifier, packet and route/firewall observations; other OS capability decisions |
| 31 | + UI_QUIT, runtime_start and trusted OS events follow distinct settings; − source loss never becomes user DISCONNECT; R crash/suspend/logoff/overflow | Real Linux logind, Windows SCM, macOS IOKit/Endpoint Security lifecycle |
| 32 | + bounded authorized redacted bundle; − foreign owner, stale handle/offset/context; R restart and expiry | Installed archive, resource/resolver observations, and per-platform default-route collector execution |
| 33 | + compatible verified pair and approved update; − digest mismatch, expired/replayed metadata, updater failure; R source rotation | Approved discovery contract, signed artifact pairing and installed updater |

BR/AC-01–20 and RULE-01–16 retain the exact row-by-row trace in
[the headless map](client-headless-requirement-map.md#business-requirements-and-acceptance-criteria).
Their unresolved dependencies are substantive: BR/AC-01/04 require declared
unattended/container variants; 02/03/05/20 require producer admission and
revocation; 06–11 require native lifecycle, peer and network traffic; 12–14/16
require resource/exit/sharing traffic and product scope; 15/17 require a
product/provider decision before implementation; 18 requires observable bounds;
19 requires distribution identity. RULE-01–05/10–12 require separate authority,
identity and cleanup boundaries; RULE-06–09/13–16 require real policy, lease,
diagnostic and product-scope evidence. No BR, AC or RULE is marked accepted.

US-01–14 retain their row-by-row production/test trace in
[the local map](client-local-requirement-map.md#implementation-and-unit-paths).
The still-open acceptance joins are: US-01 transport/artifact pairing; US-02/03
producer enrollment and connection; US-04 cross-network transition; US-05 native
exit/LAN; US-06 trust/helper; US-07 complete diagnostics; US-08 remote/local
cleanup; US-09 session renewal; US-10 managed settings and OS effects; US-11
resource route/path/application observations; US-12 lifecycle; US-13 signed
distribution; US-14 privacy and action/reason projection. Each needs the
positive, negative and restart/concurrency outcomes in the related IT rows.

## Current source boundaries and specific gaps

| Area | Inspected production boundary | Remaining code/contract work |
| --- | --- | --- |
| Exit/LAN | Native exit executor and integrated Linux guard/BPF/nft/route adapters; signed selection and no implicit default-route export | Native kernel/packet/boot/namespace acceptance; combined profile/network transition; user-defined unsupported OS variants must remain explicit |
| Resources | Signed HOST/SUBNET/SERVICE catalog, durable enablement, packet filter and correlated reply observer | A SUBNET sample proves one destination, not a whole prefix; SERVICE reply proves tuple, not application health. Complete transition and native overlap/route observation remain open |
| Lifecycle | Durable UI_QUIT/runtime_start/lifecycle settings; Linux logind power/session source, Windows SCM, macOS IOKit and optional Endpoint Security | Validate actual logoff and KEEP_INTENT through service restart. Released macOS binary lacks required entitlement/signing/FDA, so user_logoff is UNSUPPORTED there |
| Context/security | Native enrollment, selection, session, trust, logout and forget workers with bounded journals and owner checks | Cross-worker late-response, uncertain remote cleanup and real provider outcomes require end-to-end evidence |
| Diagnostics | Route target lookup, privacy bounded bundle, bounded default-route presence collection | Resource observation and OS resolver state remain absent; v0 DTO has no resource observation section. Consumer contract decision needed for new fields without implicit version increase |
| Update | GetUpdateInfo reports SOURCE_UNAVAILABLE and installed pair UNKNOWN; `reported_ui` is caller input. UI-Q10 now approves Windows signed MSI via WinGet/manual/enterprise, Linux APT exact UI/core pair, macOS Developer ID package and mobile store as channel direction | D-035 technical trust/discovery design remains proposed. `client-ui`/distribution owners must first establish an attested or signed-envelope binding for existing schema-3 provenance, authenticated installed package identity/receipt and exact running bytes, signed fresh channel index, publisher roles/trust bootstrap, rotation/revocation, and OS install outcome. Current Windows attestation subject omits `release-provenance.json`. No positive GetUpdateInfo state is implementable honestly from the present inputs; do not invent source, key, URL or a parallel pairing schema |
| IPC/consumers | Agent serves generated v0 handler via protected local transport; CLI/helper use native client; Go/Dart bindings and descriptor exist | Targeted search found no production HTTP IPC v2 route, but does not prove all obsolete artifacts removed; run generation/transport and installed pairing gates. Backend HTTP is separate from local IPC |
| Packaging | Core release, APT and cross-platform workflows live here | Installed artifact provenance, upgrade/reset/uninstall and exact UI/core pair need release CI; no version or generation increase is authorized |

### Focused cross-cutover assertion review

- Profile and network switches previously could accept a different context
  while source `ExitProtection`/`ExitSelection` remained owned; `Down` contains
  the native guard but does not release it. Admission now rejects these
  switches with `BUSY`. A pre-existing persisted plan observed before target
  activation terminates as `POLICY_BLOCKED` without calling Stop, preserving
  the source and allowing explicit exit Clear. Same-profile selection remains
  a no-op. These are fail-closed boundaries, not an implementation of native
  exit ownership transfer or an accepted combined transition. The native
  Select→Maintain→Contain→Clear plus profile/network scenario remains open.
- `PrepareNetworkSelectionTarget` drops the source Node, signed map, resource
  choices, network preferences and exit selection before target registration;
  `TestNetworkSelectionTargetSeparatesOldNetworkState` asserts those fields and
  immutability of the source. Activation records `DownStarted` before Stop and
  adopts the target only after a confirmed Stop;
  `TestNetworkActivationStopsSourceBeforeAtomicTargetAdoption` and
  `TestNetworkActivationResumesUncertainStopFromDisk` assert ordering/restart.
  Apply/abort tests assert stale target rejection, failed apply containment,
  no repeat apply after restart and remote target cleanup. These use injected
  drivers/providers. The installed `TestControlPlaneNetworkSelectionBoundary`
  checks same-network no-op, accountless denial and replay, **not** a real
  cross-network target with route/exit/resource withdrawal. New
  `TestControlPlaneNativeCrossNetworkSelection` exercises authenticated
  catalog selection, separate source/target node registration, target signed
  map activation, connected intent and restart using the native agent and test
  control plane. It compiles in short tests but is skipped there by
  `requireControlScenario`. The three-repeat contract dispatch executed it on
  all eight runners and recorded `ERROR_CODE_STALE_STATE` after acceptance on
  all 24 platform/repetition jobs. The separate control-plane job passed, but
  runs only `TestClientDataplane`; it does not provide kernel route, exit,
  resource withdrawal or real packet evidence for this scenario. Its trace is
  US-04 and its restart boundary overlaps IT-22; the
  delayed-old-response case in IT-22 and profile-switch stale-response,
  active-removal and mixed-route assertions in IT-28 remain open, as do
  US-05/11 native path assertions.
- `WireGuardEngine` resets the bounded resource-flow samples on every apply
  and Down. `TestResourceFlowIsCollectedOnlyAcrossAllowedTUNDirections` and
  `TestResourceFlowCollectorBindsSignedServiceSubnetAndLiveRoute` exercise
  correlated admitted replies, signed target/path and route binding. The
  HOST/SUBNET/SERVICE publication tests reject expired and changed topology
  receipts. No platform test proves that a profile/network switch withdraws
  a previously AVAILABLE receipt while the old path still sends packets.
  A SUBNET receipt remains existential for one address; a SERVICE receipt
  remains tuple evidence, as stated by the source.
- Healthy `runtime_start` KEEP_INTENT preserves the saved requested intent.
  When an enrolled connected intent has no verifiable map, however,
  `InitializeRuntimeIntent` writes a **temporary durable disconnected gate**
  with a context-bound `StartupRecovery` record; `RequestedConnectionIntent`
  projects the original request and policy recovery may restore it after a
  fresh signed map. `TestRPCStartupRecoveryProjectsRequestedIntentWithoutStarting`
  and `TestRuntimeStartupRecoveryKeepsOriginalIntentWithoutRevivingNewDisconnect`
  cover this distinction. `TestRuntimeLifecyclePreservesCurrentIntentAcrossRestart`
  covers KEEP_INTENT for logoff/suspend/resume. Native service restart, OS
  delivery and user-visible phase/traffic observations remain untested. The
  temporary gate is not a user Disconnect operation, but the durable top-level
  desired state is disconnected until verified recovery; treat literal
  no-DISCONNECT-intent wording as an unresolved semantic check.
- The agent serves the generated v0 handler over the protected local listener;
  CLI and recovery helper bootstrap the generated Go client. The descriptor
  digest is embedded in Go (`clientipc/rpc/protocol.go`) and generated Dart
  metadata (`packages/client_api/lib/src/contract.dart`). Protobuf CI checks
  regeneration, baseline compatibility and the Dart mobile bridge contract.
  This does not prove a native mobile bridge or installed UI/core pairing.
  Targeted source search found no old HTTP IPC v2 route in production agent,
  CLI or helper; backend HTTP and generated Connect codecs are distinct.
- Under D-035, component work that can be checked now is the current
  `SOURCE_UNAVAILABLE`/`UNKNOWN` projection and caller-claim handling, the
  existing core manifest/attestation and Windows schema-3 provenance contents,
  plus deterministic *negative* cases that do not imply trusted source
  availability. Signed-index freshness, installed identity, positive
  AVAILABLE/UP_TO_DATE, incompatible-pair mutation gating and external manager
  outcomes depend on approved producer wire/receipt contracts, publisher
  signature/attestation binding and a platform identity adapter. The direction
  specifies package channels, not production feed URLs, keys, accounts or
  install actions; release/system evidence requires exact immutable artifacts.
- `tools/verify-source-ci` requires a **successful main Test workflow_dispatch
  with contract_repetitions=3 on the exact release SHA**; it rejects a short
  push success. The release and APT workflows call that gate before publishing.
  This makes a dispatch an explicit technical release prerequisite, while the
  repository's current `AGENTS.md` still permits system validation only in
  PR/release CI and forbids PRs. The human decision on an allowed venue is
  required; a tag must not be created to obtain it.

The fwmark failure has a concrete initial-open race. Pinned wireguard-go marks
the device `Up` before its first `BindUpdate` obtains the net lock. An initial
`IpcSet(fwmark)` may call this repository's `MagicBind.SetMark` while the new
bind has never opened, as seen before `device-up begin` in failed Ubuntu log.
The bind now accepts a mark **only before its first successful Open**; the
wireguard-go device retains the requested mark and `BindUpdate` applies it to
the sockets. Once opened, a later closed bind still returns `net.ErrClosed`.
`TestMagicBindMarkBeforeFirstOpenAndAfterClose` covers this boundary. The exact
CI failure has not been reproduced with instrumentation; platform recurrence
and other bind-closure causes remain to be checked before calling it resolved.

## Acceptance plan and decision gate

1. Complete the missing implementation and assertion work above. Native
   cross-network selection/restart now has an integrated test, and IT-28 now
   has a native profile isolation/restart/restore test. Diagnostic observation
   and an explicit LAN_ALLOW system scenario remain open. Keep unavailable
   results explicit while external contracts are unresolved.
2. Pin one commit and approved producer/consumer artifacts. Run the existing
   `Test` workflow's non-push jobs with reports: `verify-*`, `installation`,
   `control-plane-platforms` on all eight runners, `control-plane`, `container-client`
   and `verify`; run the Protobuf contract workflow for descriptor/Go/Dart
   regeneration and local transport. Require every job and every declared test
   on the same source SHA to pass, including repeated runs to investigate the
   fwmark failure. A rerun alone is not root-cause evidence.
3. Add/execute native Linux LAN_ALLOW traffic for both IP families: exact
   approved LAN versus non-LAN, expiry, NIC/gateway replacement, route change,
   suspend/process death, crash/boot/namespace recovery, Select→Maintain→Contain→Clear,
   and no default-route leak. Observe actual BPF verifier, nft, kernel routes,
   sockets and denied packets. Exercise HOST/SUBNET/SERVICE, ACL, DNS, direct/
   relay, profile/network switch and cleanup against real signed maps.
4. Qualify Windows SCM, Linux logind and macOS IOKit on installed artifacts:
   runtime_start KEEP_INTENT without invented DISCONNECT, UI_QUIT versus crash,
   sleep/wake, logoff, source loss/recovery, owner changes and stream reattach.
   macOS logoff requires an entitled, signed and FDA-approved artifact and
   fast-user-switch/missed-event checks. Record unsupported status until then.
5. Qualify diagnostics privacy/bounds, per-platform default-route collection,
   and verified resource/resolver observations, then update discovery only after the approved signed source
   and exact UI/core pairing contract exist. Exercise expiry, replay, rotation,
   incompatible pair and external updater failure.

Current `AGENTS.md` permits local checks only via goimports, vet, configured
golangci-lint and short tests. It says E2E, installer, privileged networking,
release and system validation run in GitHub pull-request or release CI; the
same file prohibits PRs and version increases. The user explicitly authorized
three one-time `Test` workflow dispatches on main, so system evidence was gathered
without creating a PR or release. No further dispatch is authorized by that
approval.

### 2026-09-25 native context scenarios

Added `TestControlPlaneNativeProfileContextSwitch` for IT-28: it starts from a
registered, connected profile, creates and selects an empty profile, checks
that node identity, network, credential and map do not leak into it after a
restart, then restores the original profile and verifies the same NodeID,
signed map and connected intent without another registration. The cross-network
scenario separately covers US-04/IT-22 selection and restart. Both are guarded
system tests compiled but skipped by `go test -short ./...`. The third Test
dispatch ran both across 24 contract jobs: the profile scenario passed in 22
and failed on two macOS Intel repetitions because CreateProfile's snapshot was
stale at admission; `d8b7232` adds a bounded same-request retry for that case.
The cross-network scenario failed on all 24 with `STALE_STATE` after acceptance.
The newer `d8b7232` correction has local and push short-unit evidence only.

Push run [36109476988](https://github.com/endless-net/client/actions/runs/36109476988)
passed all four short unit jobs (Ubuntu, Windows, macOS ARM and macOS Intel).
Verify, contract, installer, container, STUN and control-plane jobs were skipped
because this was a push. The four permitted local checks passed after adding
the scenario: goimports, vet, configured golangci-lint and short tests. Full
system acceptance and the allowed CI venue decision remain outstanding.

### 2026-09-25 default-route diagnostics

The existing v0 boolean `default_route_present` now reflects a bounded read of
both address families: Linux `ip -j` route rows, macOS `netstat` routing tables,
or Windows `GetIpForwardTable2`. If either family cannot be read or validated,
the response retains the fixed `diagnostics_default_route_not_observed`
limitation. `TestObserveLinuxDefaultRoutes`, `TestObserveDarwinDefaultRoutes`,
`TestZeroWindowsRoutePrefix` and
`TestRPCDiagnosticsProjectsDefaultRouteOnlyWhenObserved` cover the collectors
and projection. The local permitted checks passed and push run 36112986842
passed the four short unit jobs; the non-push verification, contract, install,
container, STUN and control-plane jobs were skipped. This samples default-route
presence only; it does not collect the full route table or resource/resolver
state, and it does not provide installed-platform runtime evidence. No IPC or
schema version was changed.

Push run [36114327050](https://github.com/endless-net/client/actions/runs/36114327050)
passed all four short-unit jobs at `9f5faa4`, including the diagnostics provider
injection test. Verify, contract, installer, container, STUN and control-plane
jobs were skipped because this was a push.

Push run [36114870086](https://github.com/endless-net/client/actions/runs/36114870086)
passed all four short-unit jobs at `b1753cd`. Verify, contract, installer,
container, STUN and control-plane jobs were skipped because this was a push.
For IT-22, `TestControlPlaneNativeCrossNetworkSelection` exercises an accepted
target registration through signed-map adoption and restart; unit coverage in
`TestNetworkApplyContainsContextChangeDespiteProviderSuccess` confirms a
Disconnect cancels target application even when the provider returns success
after cancellation. The native test does not yet hold and deliver a late
registration response across Disconnect, so that race remains unqualified.

### 2026-09-25 approved system acceptance dispatches

The first explicitly approved manual `Test` dispatch,
[36115587959](https://github.com/endless-net/client/actions/runs/36115587959),
ran at `6863321` with three contract repetitions. Verify, native control-plane
scenarios and all eight installer/smoke jobs passed. Contract artifacts exposed
two fixture defects: account catalog selection was absent in the cross-network
scenario, and the empty-profile test incorrectly required an immediate
DISCONNECTED intent. The profile assertion correction in `9c50158` passed its
system scenario on the next dispatch. The account fixture was first adjusted to
select an account, but that run exposed an ordering error: successful join-token
enrollment clears the saved user session while adopting the node credential.
The fixture now enrolls first, logs in afterward, then selects its account.
Container acceptance failed before native IPC readiness because the container
had no system logind service.

The second dispatch explicitly approved without further confirmation,
[36120208250](https://github.com/endless-net/client/actions/runs/36120208250),
ran on `79b99eb` with three repetitions and completed with failure. All eight
installer/smoke jobs, Verify Linux/Windows/macOS, and native control-plane
scenarios passed. The container job starts the agent and serves IPC using a
test-only isolated logind bus, but Connect remains RUNNING for 30 seconds in
each persistent IPv4/IPv6 TCP/UDP case and ephemeral recreation; the lifecycle
job fails. Safe test diagnostics show the operation still pending (kind=2,
state=1), with the later operation read unclassified.

All 24 platform/repetition contract jobs failed at least one of the two new
scenarios: `TestControlPlaneNativeCrossNetworkSelection` failed in all 24 due
to the fixture logging in before join-token enrollment (which clears the saved
session); `TestControlPlaneNativeProfileContextSwitch` also received a
canonical `STALE_STATE` on SelectProfile in five jobs. The cross-network
fixture now logs in after enrollment; profile selection retries only a
classified stale admission using the same request ID, target and unchanged
source context. These corrections are in `f9994af`; the 79b99eb dispatch cannot
validate them. The aggregate `verify` gate therefore failed. Push run
[36124005128](https://github.com/endless-net/client/actions/runs/36124005128)
passed all four platform short-unit jobs on `f9994af`. A third manual Test
dispatch was explicitly approved and run once on current `main` at `3f9f432`
with `contract_repetitions=3`:
[36127380078](https://github.com/endless-net/client/actions/runs/36127380078).
It completed all 40 jobs with conclusion `failure`. Verify Linux, Windows and
macOS; native control-plane scenarios; and all eight installer/smoke jobs
passed. All 24 platform/repetition contract jobs failed:
`TestControlPlaneNativeCrossNetworkSelection` failed on every job after its
operation was accepted, with state `FAILED` and `ERROR_CODE_STALE_STATE`.
Two macOS Intel repetitions also hit `ERROR_CODE_STALE_STATE` when the profile
scenario submitted CreateProfile using an older CAS snapshot. `d8b7232` makes
that test refresh status and retry only this explicit stale admission, keeping
the same request ID and semantic payload. Local permitted checks and its push
workflow passed, but no non-push run exercised that correction.

The container job installed and started its isolated test logind service and
reached native IPC, but Connect stayed RUNNING for 30 seconds in persistent
IPv4/IPv6 TCP/UDP cases and ephemeral recreation. At each timeout the last safe
observation was `kind=2 state=1 failure=0`; the subsequent operation read was
unclassified. The aggregate `verify` gate failed because of the contract and
container jobs. Push run [36127198821](https://github.com/endless-net/client/actions/runs/36127198821)
passed all 13 jobs on `3f9f432`; push run
[36131416680](https://github.com/endless-net/client/actions/runs/36131416680)
passed all 13 jobs on `d8b7232`. The cross-network test now includes its
operation `ReasonKey` in failure output. `internal/testclient.AwaitNativeOperation`
also reports a bounded, numeric-only public status snapshot when an operation
times out; this can distinguish control, connection and agent state without
exposing provider details. Commit `29de96c` contains this timeout diagnostic.
Its push run
[36135490518](https://github.com/endless-net/client/actions/runs/36135490518)
completed successfully on that exact SHA; as a push, it ran only the four
platform short-unit jobs, while system, installer and contract jobs were
skipped. The four permitted local checks passed at `29de96c`. This was the
third manual Test dispatch covered by the user's explicit approvals; no
further system workflow is authorized by those approvals. Overall system
acceptance remains open.

Commit `4afd0c3` preserves fixed phase keys for network-selection aborts
(source change, preparation, registration, activation or exit guard), while
falling back to the generic key for any unknown value. This prevents the
operation from collapsing every stale failure into `network_selection_aborted`
and avoids exposing provider error text. Its short unit test changes source
intent during catalog preparation and verifies that the durable result keeps
the preparation-stage key. Local `goimports -w .`, `go vet ./...`,
`golangci-lint run --config .golangci-lint.yaml ./... --timeout 1m`, and
`go test -short ./...` passed. Push run
[36138621082](https://github.com/endless-net/client/actions/runs/36138621082)
is the exact-SHA push CI evidence; its system, installer and contract jobs are
skipped because it is a push. The first macOS Intel unit job failed at
`TestWireGuardRelayReplacesConnectionsWhenUnderlayMarkChanges` with
`relay retained a connection with the previous socket policy`. Rerunning only
that job at the same SHA passed in 4m28s; the other three platform unit jobs
also passed. The isolated failure's cause remains undetermined. The change is
diagnostic only and does not resolve the failed system acceptance.

At `76481a5`, timeout diagnostics now include a bounded numeric-only native
status snapshot, and cross-network aborts preserve one of five fixed phase
reason keys (`source_changed`, `preparation_failed`, `registration_failed`,
`activation_failed`, `exit_protection_active`); unknown keys remain generic.
The phase test changes source intent during catalog preparation and confirms
the durable preparation reason. The permitted local checks passed, and push
[36139842205](https://github.com/endless-net/client/actions/runs/36139842205)
passed all four platform short-unit jobs on this exact SHA. Its non-push system,
installer and contract jobs were skipped, so neither the cross-network failure
nor the container Connect timeout had been revalidated at that point. System
acceptance remained open pending a system run and resolution of the recorded
failures.

The fourth manual Test dispatch was explicitly authorized for `main` at
`aad13ca` with `contract_repetitions=3`:
[36468528401](https://github.com/endless-net/client/actions/runs/36468528401).
It completed with conclusion `failure`. All four platform Verify jobs, the
control-plane scenarios job and all eight installer/smoke jobs passed. The
control-plane scenarios job does not include the cross-network selection
contract. All 24 platform/repetition contract jobs failed both
`TestControlPlaneNativeCrossNetworkSelection` with
`ERROR_CODE_STALE_STATE` / `network_selection_preparation_failed`, and
`TestControlPlaneNativeProfileContextSwitch` with an unclassified
`CreateProfile` subprocess failure. The phase key narrows the first failure to
preparation, but does not identify which source, catalog or checkpoint guard
rejected the operation. This run therefore confirms the failure is repeatable
on every platform and repetition; it does not establish a fix.

The container lifecycle job also failed. Connect remained RUNNING in all four
persistent IPv4/IPv6 TCP/UDP cases and ephemeral recreation. The operation
snapshot reported `kind=2 state=1 failure=0`; the subsequent status snapshot
was unavailable because both operation and status CLI reads returned an
unclassified subprocess error. Thus the new snapshot confirms that native
status could not be collected at timeout, but it still does not locate the
stalled Connect phase. The aggregate `verify` job failed. No system acceptance
requirement is closed by this run, and further system runs require a new
authorization after a code or diagnostic change.

The preparation failure is now split into three fixed reason keys without
changing stale-state behavior: source changed before catalog lookup, target
context was stale during candidate preparation, or source changed at the final
checkpoint after catalog lookup. The concurrent-source unit test confirms the
third key survives durable abort. `go test -short ./internal/client`,
`go test -short ./...`, `go vet ./...`, and
`golangci-lint run --config .golangci-lint.yaml ./... --timeout 1m` passed
locally after this change. The prior full-suite attempt caught and corrected an
outdated unit assertion for the newly precise key. No subsequent system run
has exercised the new diagnostics; Block 6 acceptance remains open.

One further Test dispatch was explicitly authorized for `main` at `d1be114`
with `contract_repetitions=3`; run
[36478835005](https://github.com/endless-net/client/actions/runs/36478835005)
completed with `failure`. All 24 contract jobs failed, across every OS and
repetition. Downloaded reports from Ubuntu 24.04 ARM repeats 1–3, Ubuntu 22.04
ARM repeat 2, macOS 15 repeat 2, and Windows 2022 repeat 2 have the same pair
of failures: cross-network selection reports
`network_selection_target_context_stale`, and profile creation reports an
unclassified subprocess failure. The container lifecycle job failed five
Connect operations (persistent IPv4/IPv6 TCP/UDP and ephemeral recreation)
after 30 seconds each. Their last operation observation was RUNNING with no
failure code; subsequent operation and status CLI reads were unclassified.
Verify on all three OSes, the control-plane job and all eight installer/smoke
jobs passed. Aggregate `verify` failed because the contract and container jobs
failed. The D-035 / UI-Q10 acceptance ledger remains open.

After this dispatch, the preflight stale reason was split into fixed owner,
profile, already-selected network and control-origin keys, with targeted unit
coverage. Native CLI test diagnostics now classify process deadline,
cancellation and numeric exit status while withholding subprocess output and
arbitrary error text; this diagnostic addresses the repeated unclassified
`CreateProfile` failure. The local short suite, vet and lint passed. Push run
[36482868939](https://github.com/endless-net/client/actions/runs/36482868939)
passed all four platform short-unit jobs for `15a6f8e`. The system dispatch was
pinned to `d1be114`, so it did not exercise the newer preflight or subprocess
diagnostics. Acceptance remains open pending those system results and the
Connect lifecycle failure's cause.

The user-triggered Test dispatch
[36485364304](https://github.com/endless-net/client/actions/runs/36485364304)
on `15a6f8e` completed with `failure`. Verify (Linux, macOS, Windows), the
control-plane scenario and all eight installer/smoke jobs passed. Eight of 16
contract shards failed: shard 2 on each platform reproduced cross-network
selection as `network_selection_target_owner_stale` and profile creation as
`subprocess exit_code=1`; shard 1 passed on each platform. The container
lifecycle job again failed five Connect operations after 30 seconds. Operation
and status reads both exited with code 1, with output withheld by design, so
the underlying cause remains unknown. The aggregate verifier failed on the
contract and container jobs.

The owner-stale rejection was incorrect for administrator-authorized network
selection on an unclaimed local installation. Preparation now preserves the
empty local owner binding; the source-matching checkpoint continues to guard
against concurrent changes. The profile-context acceptance test now supplies
only arguments supported by `create-profile`, removing the invalid
`--profile-id` flag that caused its subprocess exit. The permitted local
checks (`goimports -w .`, `go vet ./...`,
`golangci-lint run --config .golangci-lint.yaml ./... --timeout 1m`, and
`go test -short ./...`) passed after these changes. Push run
[36485532172](https://github.com/endless-net/client/actions/runs/36485532172)
passed all four platform short-unit jobs on its exact SHA `0fb341e`; its
system, installer and contract jobs were skipped. The subsequent push run
[36488441226](https://github.com/endless-net/client/actions/runs/36488441226)
passed all four platform short-unit jobs on `d7d3f63`.

The user-triggered Test run
[36488191430](https://github.com/endless-net/client/actions/runs/36488191430)
used SHA `0fb341e`, before these fixes. It completed with `failure`: all 24
shard-2 contract jobs failed and all 24 shard-1 jobs passed. Reports from
Ubuntu 24.04 ARM repeats 1 and 3 and Windows 2025 repeat 1 reproduce the same
cross-network `network_selection_target_owner_stale` and
`create-profile` subprocess `exit_code=1` failures. Verify on Linux, Windows
and macOS, the control-plane scenarios and all eight installer/smoke jobs
passed. The container lifecycle job again failed five Connect operations;
operation and status reads exited with code 1 while output remained withheld.
The aggregate verifier failed because the shard-2 reports and container job
failed. This run confirms the pre-fix failures are repeatable, but does not
test the changes at `d7d3f63`. A system run on the fix SHA remains necessary;
the Connect timeout also remains unresolved, so Block 6 acceptance is open.

At `70fe547`, the native operation timeout diagnostic also records whether the
agent process has exited and whether that exit was unsuccessful. It emits only
two booleans and preserves the process completion value for cleanup. The four
permitted local checks passed, and push run
[36494552837](https://github.com/endless-net/client/actions/runs/36494552837)
passed all four platform short-unit jobs. This diagnostic has not yet run in
system acceptance; the next authorized Test dispatch must target `70fe547` or
a later source SHA containing it.

The Linux exit-provider system scenario now also submits native
`SelectExitNode` requests against a signed, lease-bound grant. It verifies
external TCP/UDP forwarding through that provider, both `LAN_BLOCK` and
`LAN_ALLOW` effects for a local service, IPv4 applied state with the IPv6
family explicitly cleared, and `ClearExitNode` restoration. This closes the
previously identified test-coverage hole for HC-036/037; the new path still
requires system execution. The permitted local checks passed after adding it,
and push run
[36496541809](https://github.com/endless-net/client/actions/runs/36496541809)
passed all four platform short-unit jobs at `b6a12e8`. System acceptance is
still required for the new scenario and the remaining Connect timeout.
The documentation update was pushed at `2fd23f2`; push run
[36497001970](https://github.com/endless-net/client/actions/runs/36497001970)
passed all four platform short-unit jobs.

The user-authorized Test dispatch
[36498730448](https://github.com/endless-net/client/actions/runs/36498730448)
used `contract_repetitions=3` on `d1be114`, before the fixes at `d7d3f63`, the
operation-exit diagnostic at `70fe547`, and the exit/LAN scenario at `b6a12e8`.
The temporary tag needed to select that commit was removed before runners had
checked it out. As a result, 28 jobs failed at checkout, the aggregate verifier
failed for missing contract reports, and two installer/smoke jobs passed
(Ubuntu 24.04 and macOS 15 Intel). Seven contract shards did execute. All seven
reproduced `TestControlPlaneNativeCrossNetworkSelection` with
`network_selection_target_context_stale` and
`TestControlPlaneNativeProfileContextSwitch` with an unclassified
`create-profile` subprocess failure. The macOS 15 repeat-3 shard additionally
failed `TestControlPlaneNativeMachineSharing/ipv6`: packet probes logged
`errno=65`, and diagnostics did not expose a bound tunnel port within 30
seconds. These results characterize the earlier SHA only; they do not qualify
the current fixes or the later exit/LAN coverage. The temporary tag has been
removed after the run completed. Push run
[36502157561](https://github.com/endless-net/client/actions/runs/36502157561)
passed all four short-unit jobs for documentation commit `4f482bd`; the
documentation-only push does not replace the missing current-SHA system run.

The user-authorized Test dispatch
[36532058005](https://github.com/endless-net/client/actions/runs/36532058005)
ran three contract repetitions on the current `main` SHA `1b50f8a`. It
completed with `failure`: 46 jobs passed, 14 contract shards failed, the
container lifecycle job failed, and aggregate `verify` failed; two jobs were
skipped. All three platform Verify jobs, the control-plane scenarios, and all
eight installer/smoke jobs passed. Of 48 contract shards, 34 passed. Twelve
failed shards are Linux `TestControlPlaneExitProvider` runs: repeats 1–3 on
Ubuntu 22.04 x64, Ubuntu 22.04 ARM, Ubuntu 24.04 x64, and Ubuntu 24.04 ARM.
Each timed out while applying the explicit IPv4 exit selection after 30
seconds. The native operation remained RUNNING with no failure code, the
agent process was still alive, and no tunnel port became observable. This
reproduces the same failure on both supported Linux runner generations and
architectures; HC-036/037 exit-provider/LAN system acceptance remains open.

Two additional shards failed `TestControlPlaneNativeCrossNetworkSelection`:
Windows 2022 repeat 2 and macOS 15 Intel repeat 1. Both report
`ListNetworks` as `failed_precondition: ERROR_CODE_STALE_STATE` before the
cross-network mutation is submitted. The profile-context scenario passed on
the Windows shard. All contract shards for macOS 15, macOS 15 Intel (other
than the one above), Windows 2022 (other than the one above), and Windows
2025 passed. This makes the network catalog issue intermittent; it is not
closed by the passing repetitions.

The container lifecycle job failed all four persistent-state Connect cases
(IPv4/IPv6 TCP/UDP) and ephemeral recreation. At each 30-second timeout the
Connect operation's subprocess had exited unsuccessfully with exit code 1;
subprocess output remains withheld, so the underlying Connect failure is
still unknown. Container system acceptance remains open. The aggregate
verifier failed because system coverage was incomplete. No release was
created, and no version was increased. Block 6 remains unaccepted pending
the Linux exit operation, intermittent authenticated catalog, and container
Connect failures, as well as the external business and update-source evidence
listed above.

The user-authorized Test dispatch
[36540037972](https://github.com/endless-net/client/actions/runs/36540037972)
ran three contract repetitions on `main` SHA `687cdf3`. It completed with
`failure`: 48 jobs passed and 13 failed, including 12 contract shards, the
container lifecycle job, and aggregate `verify`; the remaining three jobs were
skipped. All three platform Verify jobs, the control-plane scenarios, and all
eight installer/smoke jobs passed. Of 48 contract shards, 36 passed. The 12
failed shards are again exclusively Linux `TestControlPlaneExitProvider` runs:
repeats 1–3 on Ubuntu 22.04 x64, Ubuntu 22.04 ARM, Ubuntu 24.04 x64, and Ubuntu
24.04 ARM. Each stalls applying explicit IPv4 exit selection for 30 seconds.
On a Ubuntu 22.04 report, the bounded timeout diagnostics confirmed the native
WireGuard interface was present and healthy (`present=true`, `ok=true`,
`interface_present=true`, MTU 1420, listen port 51820), while the operation
remained RUNNING without a failure code and the packet probe reported network
unreachable. This narrows the failure to a later part of exit selection or
confirmation; the exact cause is not established. HC-036/037 system
acceptance remains open.

No Windows or macOS contract shard failed in this run, and no
`ListNetworks`/`STALE_STATE` failure appeared. This is evidence that the bounded
retry prevented the intermittent catalog failure in this matrix, not proof
that the catalog race is impossible. The container lifecycle job again failed
the four persistent-state Connect cases (IPv4/IPv6 TCP/UDP) and ephemeral
recreation after 30 seconds each. At every timeout the Connect operation
remained RUNNING, but the native subprocess had exited with code 1; status and
tunnel diagnostics were unavailable because those subprocesses also exited
with code 1. Their output remains withheld, so the root cause is unknown.
Block 6 remains unaccepted pending the Linux exit-selection and container
Connect failures, plus the external business and update-source evidence above.
No release was created, and no version was increased.

The user-authorized Test dispatch
[36581364655](https://github.com/endless-net/client/actions/runs/36581364655)
ran three contract repetitions on `main` SHA `f38312a`. It completed with
48 successful jobs, 14 failed jobs, and two skipped jobs. All 12 Linux
`TestControlPlaneExitProvider` shards failed across repeats 1–3 on Ubuntu 22.04
x64, Ubuntu 22.04 ARM, Ubuntu 24.04 x64, and Ubuntu 24.04 ARM. Every report
reached durable operation validation and then emitted the exact stage
`Native exit guard: owner id missing`; the request owner binding fix is
therefore working, while the test installation lacked its local owner identity
in the retained protection scope. Container lifecycle again failed all four
persistent-state Connect cases and ephemeral recreation. Its exact diagnostic
was `logind lifecycle stage: preparing state value uint64`, with reconnect
succeeding between attempts; the isolated test logind mock returned a numeric
variant although the client correctly expects the D-Bus boolean property.
Aggregate `verify` failed on the container and Linux contract reports. The
remaining platform contract shards and installer/smoke jobs passed; no
cross-network stale-state failure appeared. The failure stage is identified,
but Block 6 requires another system run after the fixture and owner-binding
changes below.

The pending remediation corrects the test logind mock to return `false` for
`PreparingForSleep`, initializes Linux exit-provider test configurations with
the effective IPC peer identity before enrollment, and makes selection binding
reject an empty installation owner. `goimports`, `go vet ./...`,
`golangci-lint run --config .golangci-lint.yaml ./... --timeout 1m`, and
`go test -short ./...` passed on the working tree. The remediation still needs
push CI and a three-repetition system Test dispatch. Block 6 remains open,
including the external business decision D-035 and accepted update-source and
discovery evidence UI-Q10. No release was created, and no version was
increased.

The follow-up system Test run `36589087113` on `0e0d44d` completed with 48
successful jobs, 14 failed jobs, and two skipped jobs. All 12 Linux
`TestControlPlaneExitProvider` shards again timed out with the durable operation
still running. They passed guard validation but did not report engine
configuration completion. The operation subprocess remained alive, so this run
does not yet distinguish a wait on the engine mutex from a later configure
stage. The next run adds exact public markers before and after that lock and
before `configureExit`.

The container lifecycle job failed all five workloads with
`exit_category=logind_initialization_failure`; unlike the previous run, it did
not report a `PreparingForSleep` value error. Inspection of the isolated
logind fixture showed that it served `PreparingForSleep` but rejected the
additional `InhibitDelayMaxUSec` property required by `openLogindSession`.
The fixture now returns the expected boolean and a positive `uint64`, with a
unit test for both property types. This diagnosis and the exit-stage
instrumentation have passed local checks; a new push CI run and system Test
acceptance are still required. D-035 / UI-Q10 remain external acceptance
blockers, and Block 6 is not complete.

Push CI run `36598096514` for `2f14add` passed all 13 jobs. The authorized
three-repetition system Test run `36598774376` on that commit completed with
48 successful jobs, 14 failed jobs, and two skipped jobs. The container
lifecycle job passed, confirming that the isolated logind fixture now serves
the startup properties with their expected D-Bus types. All 13 Linux
`TestControlPlaneExitProvider` shard-1 jobs failed with an unfinished operation;
the corresponding shard-2 jobs passed. Each failed report shows that engine
mutex acquisition completed but does not show the later `exit guard contained`
stage. This narrows the next inspection to the start of
`configureWithExitLocked`, its context check, packet-filter withdrawal, and
guard containment. Exact fixed markers for those boundaries are now added and
need their own push CI and system acceptance run. One macOS Intel shard also
failed `TestControlPlaneSessionExpiryRecovery` on IPv6; it did not affect the
Linux exit-provider or container results and should be assessed after the
primary failure is instrumented. Block 6 remains open, including external
D-035 / UI-Q10 evidence; no release or version increase was made.

Two later authorized system Test attempts were interrupted by newer pushes to
`main` before their matrices completed. Run `36605891826` on `1f8dfc5` had
five install-and-smoke failures; their `TestInstalledClient/fresh-install`
subtests each hit the 45-second deadline waiting for native service status.
Run `36606594466` on `16d2457` was also cancelled by subsequent `main` pushes
after four install-and-smoke failures. Push CI `36607581914` for the latest
pre-remediation main commit passed all 13 jobs.

The installer failures exposed an interaction with the new default-on debug
logger: the systemd service uses `ProtectHome=true` while the CLI default
resolves logs beneath the service user's home, and launchd / Windows renderers
omitted an explicit false value when their service options disabled logging.
Systemd now uses its managed `/var/log/endlessnet-client` log directory;
launchd and Windows service templates pass the selected debug value explicitly.
The CLI artifact assertion was updated for the explicit true value. All four
allowed local checks pass on this remediation. Push CI and an uninterrupted
three-repetition system Test run are still needed. The Linux exit-provider
configure-stage diagnostics remain in `68f4d06` and will be included in that
next system run. D-035 / UI-Q10 and the prior Linux exit acceptance remain open.
