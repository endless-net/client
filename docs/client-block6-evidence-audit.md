# Block 6 evidence audit (2026-09-23)

Source: `main` at `7c3d486`, clean checkout before this audit. This is an open
requirement and acceptance ledger, not a declaration of cutover completion.
The normative IT-01–33 and BR/AC-01–20, RULE-01–16 sources are the pinned
architecture revision `bdb5ba63c0e5356122c0760f4d63205e84ef507d`; the
local US-01–14 source is the pinned client-ui revision in
[the local map](client-local-requirement-map.md). Existing maps name unit-test
entry points. They do not establish the assertions or real effects below.

## Evidence available now

- Push [35828079929](https://github.com/endless-net/client/actions/runs/35828079929)
  passed the Linux, Windows 2025, macOS ARM and Intel **short unit** matrix at
  `7c3d486`. Earlier [35822315478](https://github.com/endless-net/client/actions/runs/35822315478)
  failed on Ubuntu with `failed to update fwmark: use of closed network
  connection`. Later passes have not identified the closure cause.
- `test.yml` runs installation, eight-platform contract scenarios, isolated
  dataplane and container jobs only for non-push events. No result from those
  jobs at the current source was inspected in this audit. Push success is not
  native or installed-artifact evidence.
- `tests/control_plane_exit_route_test.go` explicitly checks **no implicit
  exit**; it does not exercise SelectExitNode or LAN_ALLOW. No test in `tests/`
  mentions LAN_ALLOW. `tests/control_plane_network_selection_test.go` covers
  current-ID reselection and account-catalog denial, not cross-network
  handover. These are concrete system-coverage holes.
- `service_rpc_diagnostics.go` reports uncollected default-route/resource
  observation and unobserved OS resolver state. `service_rpc_update.go` returns
  SOURCE_UNAVAILABLE. Neither is an accepted implementation of the full
  diagnostics/distribution contract.

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
| 28 | + one active profile and valid target; − stale response/active removal/mixed routes; R cross-network apply/abort after crash | Native network/profile transition and producer cleanup |
| 29 | + independent session/node clocks; − unknown expiry not infinite or seamless; R interrupted renewal | Native timing and producer renewal |
| 30 | + explicit Select/Apply/Contain/Clear, exact LAN family result; − path loss, overlap, lock, expiry and no implicit default; R boot/namespace/ownership recovery | Linux kernel verifier, packet and route/firewall observations; other OS capability decisions |
| 31 | + UI_QUIT, runtime_start and trusted OS events follow distinct settings; − source loss never becomes user DISCONNECT; R crash/suspend/logoff/overflow | Real Linux logind, Windows SCM, macOS IOKit/Endpoint Security lifecycle |
| 32 | + bounded authorized redacted bundle; − foreign owner, stale handle/offset/context; R restart and expiry | Installed archive and resource/route/resolver observations |
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
| Diagnostics | Route target lookup and privacy bounded bundle | Full default-route/table, resource and OS resolver observations absent; v0 DTO has no resource observation section. Contract/consumer decision needed without implicit version increase |
| Update | GetUpdateInfo reports SOURCE_UNAVAILABLE; support URLs absent | Distribution/product/UI owners must provide index/API, trust bootstrap/rotation, channel/platform/expiry/replay/classification and binding to existing UI/core schema-3 provenance; do not invent source/key/URL |
| IPC/consumers | Agent serves generated v0 handler via protected local transport; CLI/helper use native client; Go/Dart bindings and descriptor exist | Targeted search found no production HTTP IPC v2 route, but does not prove all obsolete artifacts removed; run generation/transport and installed pairing gates. Backend HTTP is separate from local IPC |
| Packaging | Core release, APT and cross-platform workflows live here | Installed artifact provenance, upgrade/reset/uninstall and exact UI/core pair need release CI; no version or generation increase is authorized |

The fwmark failure is not isolated as a confirmed code defect. In pinned
wireguard-go, `IpcSet(fwmark)` calls `BindSetMark`, which calls this repository's
`MagicBind.SetMark`. Its `net.ErrClosed` means `session == nil`; the source
review does not establish **why** the bind had closed. Ignoring that error or
claiming a passing rerun fixed it would hide a failed socket operation.
Capture device state, preceding BindUpdate/Close errors and fixture event order
on the next authorized platform run before changing behavior.

## Acceptance plan and decision gate

1. Complete the missing implementation and assertion work above, especially
   cross-network transition, diagnostic observation and an explicit LAN_ALLOW
   system scenario. Keep unavailable results explicit while external contracts
   are unresolved.
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
5. Qualify diagnostics privacy/bounds and verified route/resource/resolver
   observations, then update discovery only after the approved signed source
   and exact UI/core pairing contract exist. Exercise expiry, replay, rotation,
   incompatible pair and external updater failure.

Current `AGENTS.md` permits local checks only via goimports, vet, configured
golangci-lint and short tests. It says E2E, installer, privileged networking,
release and system validation run in GitHub **pull-request or release CI**;
the same file prohibits PRs and version increases, while the `Test` workflow's
non-push jobs are reached by PR or `workflow_dispatch`. The latter is not named
as an allowed venue in `AGENTS.md`. A release tag publishes assets and is not
authorized for this block. Therefore the system plan above is prepared but
cannot be executed under the current instructions. An explicit decision on an
allowed CI venue is required after the independent implementation/audit work;
do not bypass this with a PR, dispatch or release.
