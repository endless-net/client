# Block 6 evidence audit (updated 2026-09-25)

Source: completed system evidence through manual dispatch `36127380078` on
`main` at `3f9f432`; fixture corrections and short-unit evidence through
`d8b7232` (updated 2026-09-25). This remains an
open requirement and acceptance ledger, not a declaration of cutover completion.
The normative IT-01–33 and BR/AC-01–20, RULE-01–16 sources are the pinned
architecture revision `bdb5ba63c0e5356122c0760f4d63205e84ef507d`; the
local US-01–14 source is the pinned client-ui revision in
[the local map](client-local-requirement-map.md). Existing maps name unit-test
entry points. They do not establish the assertions or real effects below.
The later [D-035](https://github.com/endless-net/architecture/blob/9ff22c1/docs/ru/decisions/d-035.md)
and [update-source design](https://github.com/endless-net/architecture/blob/9ff22c1/docs/ru/client-update-source-and-pairing.md)
at architecture `9ff22c1` add an approved UI-Q10 *channel direction* and a
still-proposed trust/discovery design; they do not revise the pinned IT or
BR/AC acceptance baseline or supply runtime evidence. At current architecture
`main` (`68b4c5b`), both files retain their `9ff22c1` blob identities; the newer
commit changes D-036 and leaves these client update decisions unchanged.
The Client UI consumer SA was refreshed in the [local map](client-local-requirement-map.md)
to [b03fd54](https://github.com/endless-net/client-ui/blob/b03fd540789a3a33fce26b9203888b899ce311f1/docs/client-ui-system-analysis.md),
the latest edit to that path. Current architecture `main` is
[68b4c5b](https://github.com/endless-net/architecture/commit/68b4c5bd11be4484e1eeda6164b9b5cab4fb80c4),
but the two D-035 files have no edits after `9ff22c1`; their trust/discovery
proposal and owner dependencies remain unchanged.

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
operation `ReasonKey` in failure output, so a future authorized run can
distinguish source invalidation from target-readiness failure without exposing
provider details. This was the third manual Test dispatch covered by the
user's explicit approvals; no further system workflow is authorized by those
approvals. Overall system acceptance remains open.
