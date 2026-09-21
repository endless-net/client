# Client runtime implementation gaps

Status: incomplete implementation; current blocking-path audit, 2026-09-15.
Scope: `client` and its producer contracts only. No changes to UI, backend
services or infrastructure are authorized by this implementation plan.

## Execution order

1. Map every applicable BA/SA requirement to runtime code, unit tests and external
   dependencies. The inventory below is a starting point, not the complete matrix.
2. Implement the required functionality with positive and negative unit tests:
   authorization, validation, durable idempotency, concurrency and recovery.
3. Audit functional completeness before moving to integration, system and
   platform acceptance. Missing providers do not count as implemented features.
4. Fix acceptance defects with unit regressions where applicable. SDK generation,
   transport conformance and green CI alone do not establish feature completeness.

The [local v0 requirement matrix](client-local-requirement-map.md) complements
the headless BR/AC/RULE/IT inventory with all UF/UBR/UR/UI-AC IDs from the pinned
BA and their US implementation/unit and external-owner joins. Inventory coverage
does not close the remaining assertion audit or implementation gaps below.

## Confirmed source gaps

LAN live-hook readback increment (2026-09-21): `exit_lan_bpf_hook*.go` collects
a bounded NFNL_SUBSYS_HOOK dump from a fresh NETLINK_NETFILTER socket in the
current network namespace. Completion requires one exact BPF program/family/
hook/priority match and a successful DONE. Kernel sender, sequence, the distinct
data/DONE header PID rules, native framing and BE payloads are checked. Loss,
interrupted dumps, malformed/duplicate evidence and work/time limits reject
confirmation. Cancellation closes the pollable socket and joins its callback.
Ordinary, nft and other non-BPF hook metadata cannot grant BPF authority.
This is a point-in-time primitive; both-family validation, retained link/pin
ownership, lifecycle synchronization and adapter integration remain open.
Kernel dump consistency uses the hook-array pointer, not a durable generation;
IPv4 and IPv6 observations are not atomic together
([hook dump implementation](https://raw.githubusercontent.com/torvalds/linux/v6.12/net/netfilter/nfnetlink_hook.c),
[hook attributes](https://raw.githubusercontent.com/torvalds/linux/v6.12/include/uapi/linux/netfilter/nfnetlink_hook.h)).
Integration audit: the standalone link/pin primitives currently require both
families. That is not a policy requirement: `exit_lan_plan.go` compiles only
selected families, and native exit leaves an unselected family ordinary. Before
integration, create and pin the explicit selected family set, naming links by
their family rather than slice position. Failure of a selected family must not
downgrade dual-stack. Route collection currently reads both families and needs
the same audit; unsupported unselected AF must not fabricate selected evidence.
Validation: goimports, vet and configured lint (0 issues) passed; full local
short suite passed (internal/client 136.552 s, CLI 10.278 s). Linux transport
units require Linux CI, and real netlink/poller behavior remains unqualified.
The preceding pinning commit `075d5b1` passed all three short CI platforms
([run 35636421689](https://github.com/endless-net/client/actions/runs/35636421689)).

LAN pinning increment (2026-09-21): `exit_lan_bpf_pin.go` uses the directory-FD
OBJ_PIN/GET ABI with fixed bounded relative basenames. It closes the lease,
rejects any pre-existing pin, creates map/program/IPv4/IPv6 pins exclusively,
then reopens each pin and compares its kernel identity with the held original
object. Failure returns successfully created names and leaves partial pins
closed for recovery; it never unlinks or replaces existing objects. Close
releases descriptors without unpinning. This is not startup recovery: persistent
ownership records, adopting a previous process's objects and integrating the
live-hook readback above are still required before native LAN can open.
`exit_lan_bpf_directory_linux.go` opens the fixed bpffs path one directory at a
time with O_NOFOLLOW, checks root ownership, safe parent modes, exact 0700 for
the owned directory and BPF_FS_MAGIC. It creates only a missing owned directory;
existing paths are never chmod'ed, replaced or removed. Nonblocking directory
flock remains held until Close. Retained ancestor FDs allow subsequent path,
inode, permission and filesystem revalidation. Unsupported or busy locking
fails closed. These checks trust administrators able to change root-owned paths
or mounts and have only injected-syscall tests so far.
Validation: goimports, vet and configured lint (0 issues) passed; full local
short suite passed (internal/client 140.084 s, CLI 11.318 s). Linux-only directory
units await Linux CI; native filesystem/locking effects remain unqualified.
The preceding closed-link commit `6569be2` passed all three short CI platforms
([run 35635277246](https://github.com/endless-net/client/actions/runs/35635277246)).

LAN closed-link increment (2026-09-21): `exit_lan_bpf_link.go` creates IPv4
and IPv6 netfilter links only after withdrawing the lease. It verifies the
program ID and each returned link's type, nonzero ID, program, family, hook,
signed priority and zero flags. Partial failure/cancellation closes only newly
created links; duplicate attachment is rejected. Preparation Close releases
owned link FDs before program/map FDs and does not issue explicit detach.
These are object identities, not live-hook receipts: metadata remains after
detach. Link pins are now created by the pinning primitive above; current-netns
hook dump is implemented separately, but adapter integration is still missing. No LAN capability is
enabled by either private primitive.
ABI and detach semantics were checked against
[Linux UAPI](https://raw.githubusercontent.com/torvalds/linux/v6.12/include/uapi/linux/bpf.h)
and the [netfilter implementation](https://raw.githubusercontent.com/torvalds/linux/v6.12/net/netfilter/nf_bpf_link.c).
The next ownership step should use `BPF_F_PATH_FD` (already in pinned x/sys)
with a trusted, open bpffs directory and a single relative basename. It is
openat-style path resolution, not a no-symlink guarantee. Directory ownership,
cross-process exclusion, full object binding and exclusive pin creation must
precede recovery or cleanup; unknown pins must not be replaced or unlinked.
Pins hold object references across process death, not reboot. Pin creation is
implemented above; adopting old pins and guarded cleanup still need implementation
([pin/lookup implementation](https://raw.githubusercontent.com/torvalds/linux/v6.12/kernel/bpf/inode.c)).
Validation: goimports, vet, configured lint (0 issues) and the full local short
suite passed (internal/client 137.679 s, CLI 10.486 s). Review caught and fixed
link cleanup being outside Close; the ownership regression now passes. The
preceding immutable-loader commit `1cee3d1` passed all three short CI platforms
([run 35634231298](https://github.com/endless-net/client/actions/runs/35634231298)).

LAN BPF preparation increment (2026-09-21): `exit_lan_bpf*.go` now builds the
bounded netfilter program, reads layout from fixed kernel sysfs BTF and loads
initially closed, unattached objects through the pinned x/sys BPF syscall.
The packet program compares a full unsigned absolute boottime expiry; it drops
matching LAN marks with a missing or elapsed lease. Publication swaps an outer
ARRAY_OF_MAPS slot to a frozen, program-read-only inner ARRAY. Delayed writes do
not renew the deadline. Failed refresh revokes previous authority; ambiguous
revocation remains an error requiring independent nft containment. Cancellation
while another publisher owns the mutex returns without claiming revocation.
Close releases user FDs only. Portable tests use a fake syscall and an instruction
interpreter; neither demonstrates kernel verifier acceptance or packet effects.
Pinned live links, current-netns hook readback, shared evidence caps, boot-bound
recovery and adapter integration remain open. Native LAN_ALLOW stays closed.
Validation: goimports, vet and configured lint (0 issues) passed; the full local
short suite passed (internal/client 136.199 s). Linux syscall/BTF entry points
still require Linux CI compilation and later privileged qualification. The
preceding installed-recovery budget fix passed all three short CI platforms
([run 35633546764](https://github.com/endless-net/client/actions/runs/35633546764));
its Windows installed scenario has not been rerun.

LAN clock preparation increment (2026-09-21): `exit_lan_deadline*.go` captures
CLOCK_BOOTTIME / CLOCK_REALTIME / CLOCK_BOOTTIME before preparation, converts
the already-intersected expiry using the first boot sample, and retains a fixed
absolute nanosecond deadline. Delayed publication cannot be represented as a
new TTL. Validation rejects overlapping/regressed samples, elapsed equality,
overflow and saturated time arithmetic; boottime accounts for suspend and the
wall limit additionally closes forward clock jumps. The native preparation
wrapper retains the topology and health gates and opens no kernel rule.

This is one-preparation clock evidence only. Shared caps for repeated evidence,
durable boot-bound recovery, qualified BPF publication and live attachment verification
remain mandatory before LAN_ALLOW can be enabled.

`exit_lan_btf.go` resolves `bpf_nf_ctx.skb` and unsigned 32-bit `sk_buff.mark`
from bounded BTF metadata, including anonymous aggregates and qualified types.
Pointer width is explicit; no kernel struct offsets are hardcoded. Ambiguous
layouts, target bitfields, malformed records, cycles and unsupported extensions
fail closed. Native BTF loading and instruction generation are now implemented
by the BPF preparation above; verifier acceptance and attachment ownership
remain unqualified and incomplete respectively.
Validation: goimports, vet and configured lint (0 issues) passed; the full local
short suite passed (internal/client 139.047 s, CLI 10.687 s). Sampler tests are
Linux-specific and require push CI; portable deadline/BTF units ran locally.
The preceding HOST lifetime commit `d810473` passed all three short CI platforms
([run 35630016440](https://github.com/endless-net/client/actions/runs/35630016440)).

HOST observation lifetime increment (2026-09-21): collection and Current recheck
immutable expiry after the final device/relay readback. A relay generation that
ends during UAPI inspection cannot retain a valid receipt. Signed selected-exit
grant expiry also caps HOST evidence alongside map, handshake, path freshness
and the original five-second collection deadline. This fixes publication of
stale transport evidence, not SERVICE/application reachability or an OS lease.
Validation: goimports, vet and configured lint (0 issues) passed; the full short
suite passed (internal/client 138.087 s, CLI 12.274 s). Independent review found
no blocking defects. The preceding topology watcher commit `d1d914b` passed all
three short CI platforms, including Linux datagram units
([run 35628903554](https://github.com/endless-net/client/actions/runs/35628903554)).

Absolute LAN lease research (2026-09-21): do not implement evidence expiry as a
renewable nft set timeout. `NFTA_SET_ELEM_EXPIRATION` is a remaining duration;
the kernel adds current jiffies during insertion/update. Delayed installation
can therefore move expiration beyond the original evidence deadline
([kernel set implementation](https://kernel.googlesource.com/pub/scm/linux/kernel/git/torvalds/linux.git/+/7dd38d9dd7a05329825fe2324d4d8e27ad4b3cec/net/netfilter/nf_tables_api.c)).
`NFT_META_TIME_NS` uses realtime, so it alone is vulnerable to clock rollback
([kernel meta implementation](https://kernel.googlesource.com/pub/scm/linux/kernel/git/torvalds/linux/+/80119a77e5b03747b8886505df1b3cb26f49168d/net/netfilter/nft_meta.c)).

A candidate adapter must compare an immutable, conservatively captured absolute
CLOCK_BOOTTIME deadline in the packet path (`bpf_ktime_get_boot_ns`, including
suspend), optionally intersecting realtime expiry for clock jumps forward.
Reusing an old handshake/map/grant after rollback or restart must never create
a later boottime cap. BPF netfilter can access the helper, but this repository
has a loader and closed-link creation but lacks live readback and durable ownership. Losing
the last attachment handle must not leave nft LAN allowance behind. These are
implementation requirements, not an enabled fallback or platform qualification
([helper](https://kernel.googlesource.com/pub/scm/linux/kernel/git/bpf/bpf/+/refs/tags/v6.17-rc7/kernel/bpf/helpers.c),
[netfilter verifier](https://kernel.googlesource.com/pub/scm/linux/kernel/git/torvalds/linux/+/80119a77e5b03747b8886505df1b3cb26f49168d/net/netfilter/nf_bpf_link.c)).

Implementation sequence for that adapter: prepare with closed lease under the
existing BLOCK guard; pin the IPv4/IPv6 links (program/map pins alone do not hold
attachments); reopen and verify scope and real current-netns hooks; publish one
immutable lease; revoke by deleting the outer map slot while retaining links. Close must only
close user FDs. Unpin/detach is allowed only after confirmed BLOCK. Recovery
starts with BLOCK and a closed lease; unknown pinned objects must not be deleted.
Link INFO alone can retain family/hook/priority after detach, so it is not live
hook evidence. Hook dump must also match program ID/family/hook/priority
([link lifecycle](https://android.googlesource.com/kernel/common/+/9f5cbdaae5f760c218c82e0a5e0f9c58bac56f0c/net/netfilter/nf_bpf_link.c)).
The pinned x/sys supplies syscall/constants. Bounded raw BPF loading, instruction
construction and BTF field resolution are implemented; pin ownership and hook
readback still need implementation. Missing support must preserve BLOCK.

LAN topology lifetime increment (2026-09-21): native collection subscribes to
Linux routing notifications before either snapshot. A one-shot receipt closes
on link/address/route/rule events, receive loss, cancellation or shutdown. Copies
and compiled plans share that same lifetime; final health readback cannot return
a plan whose receipt was invalidated. A replacement receipt cannot revive an
older plan. Native preparation requires a receipt; pure CIDR compilation alone
does not grant runtime authority.

This bounds reuse of reported topology evidence, not notification delivery time
or packet-time device identity. Wi-Fi association events, capture/install ABA,
kernel-expiring leases and actual route/firewall installation remain open. Local
units inject change sources/datagrams and do not open native netlink sockets.
Validation: goimports, vet and configured lint (0 issues) passed; the full local
short suite passed (internal/client 128.992 s, CLI 11.173 s). Linux-specific
datagram tests run in push CI; local Windows execution does not cover them.
The previous evidence commit `578e40e` passed all three short CI platforms
([run 35627901524](https://github.com/endless-net/client/actions/runs/35627901524)).

LAN preparation (2026-09-21): `exit_lan_source*.go` collects bounded Linux
link/address/main-route snapshots with fixed sysfs device/driver/bus reads.
Before/after canonical state must agree. Known virtual topology, gateway/via,
multipath and ambiguous attachments cannot produce a candidate. Earliest finite
address validity/preference limits the source deadline; countdown itself is not
treated as an identity change. Sysfs hardware candidates do not rule out guest
hardware emulation, so physical-platform qualification remains required.
IPv6 RA routes require a positive bounded integer `expires` and an explicit
low/medium/high router preference. Route expiry also caps the source deadline;
countdown is not identity, but preference and timed/permanent transitions are.
Unknown fields never widen the candidate set.

`exit_lan_health.go` binds preparation to authenticated selected-peer handshake,
signed authority, engine/device/path generation and direct check or live relay
generation. Repeated reads cannot extend the original evidence deadline.
Preparation intersects this deadline with topology/map/grant expiry and checks
it again after final readback. This is peer transport evidence, not proof of
Internet forwarding; kernel lease enforcement and native LAN admission remain
unimplemented.

`exit_lan_policy.go` authenticates the map and exact exit grant, preserves all
non-default peer/resource destinations independently of enablement, includes
declared application CIDR targets even without route leases, application routes
and sticky filter reservations, and subtracts them exactly
from a connected prefix. Application link-local, multicast, unspecified,
loopback and broadcast addresses are excluded; /31 and /32 are preserved.
`exit_lan_plan.go` retains original prefix/interface/source bindings, caps expiry
by topology and policy, and rejects empty selected families. These are native
transaction inputs only; Apply/capability remains BLOCK-only.

The next enforcement step is an exclusively owned direct-route table with a
terminal marked prohibit rule, avoiding fallback to main/default. Linux routes
bind device objects and are removed on unregister, unlike nft oifname/ifindex
or netdev hooks that may match a replacement. Capture-to-install ABA, changes
to attachment/prefix, table/mark/rule ownership, expiry without the process,
fresh exit health and strict readback/recovery remain unimplemented. Wi-Fi
reassociation can preserve ifindex/address/carrier, so it needs separate
attachment invalidation rather than treating this snapshot as network identity.
A repeated
snapshot or `rt nexthop == destination` is not sufficient: IPv4 via IPv6 gateway
is a counterexample to that stock nft check. D-034 in architecture (`8719ce7`)
records the accepted semantics; none of this preparation proves OS acceptance.

Preparation review additionally rejected path-like interface names before sysfs
access and bounded work across the entire plan, including fully excluded routes.
The CIDR-without-lease regression prevents a restarted application reservation
from becoming an unintended LAN fallback. Linux boundary units use only rejected
input and pre-cancelled contexts; they do not read the host's sysfs or routes.

LAN evidence validation: goimports, vet and configured lint (0 issues) passed.
The final full short suite passed: internal/client 128.730 s; other packages
passed or reused cached results. An earlier run failed in internal/client with
the failing assertion lost in truncated tool output; the unchanged repeat with
a retained full log passed. This intermittent failure remains undiagnosed.
The preceding LAN preparation commit `62113c9` passed cross-platform short CI
([run 35625058266](https://github.com/endless-net/client/actions/runs/35625058266)).
No native/system checks were run.

Relay HOST increment (2026-09-21): private bridge observations bind signed-map
hash/revisions, recipient, peer key, local UAPI endpoint, selected external relay
and live bridge generation. Confirmation requires a complete authenticated
handshake timestamp strictly after bridge readiness and path transition, while
retaining native route/rule/filter checks. Ended/replaced bridges and revoked DNS
leases invalidate receipts. Bridge reuse includes authority and peer keys; public
status copies cannot mutate private evidence. Nanoseconds remain private engine
evidence and do not change the wire/JSON contract. Direct paths also require a
post-transition handshake; an existing session without a new handshake stays
unknown. Tests cover collector route/filter rejection, binding, lifetime and
subsecond ordering using injected OS/handshake evidence, not live relay traffic.

Relay validation: goimports, vet, configured lint (0 issues) and the full short
suite passed; internal/client 130.398 s and cmd/endlessnet-client 10.743 s.
The preceding worker-lock commit also passed cross-platform short CI
([run 35621070454](https://github.com/endless-net/client/actions/runs/35621070454)).
Native/system qualification remains outstanding.

Worker-lock increment validation (2026-09-21): goimports, vet, configured lint
(0 issues) and full short tests passed; internal/client 126.168 s and CLI
11.216 s. A follow-up source search found no remaining direct blocking worker or
driver-effect mutex acquisition in RPC workers. This does not prove cancellation
of remote effects already dispatched. The retained-exit resource regression
additionally covers real admission, packet denial/removal, replay and maintenance.

Transition increment (2026-09-21): post-Disconnect offline notification uses the
engine-owned control transport. Successful local Down remains successful when
notification authority/transport is unavailable; no ordinary HTTP fallback is
permitted. Retained exit now has a separate, durably bound preference/resource
apply path. It validates the RUNNING operation, scope and exact candidate before
guarded effects, observes native state and rechecks the durable snapshot afterwards.
Saved resume still refuses transaction candidates. Failure/cancellation withdraws
the attempted exit; cancellation between Start and durable commit additionally
performs bounded independent Stop under the worker effect lock, retaining the
journal for retry. Profile/network switches remain separate transitions.
Profile/connect/disconnect/logout/preference/network-selection/trust-adoption
workers now acquire their serialization and shared runtime mutexes through
context-aware waiting. The driver contract requires the concrete shared mutex;
no blocking generic-lock fallback is retained. Cancellation while waiting leaves
durable work recoverable and cannot dispatch effects after the caller has left.
Session/enrollment/trust providers and network coordinator/preparation/registration
now use the same cancellable worker-lock acquisition. Short admission mutexes
remain unchanged; remote callbacks run outside them. Provider cancellation after
dispatch, late effects and native OS lifetime still need the full audit.

Changed-interface recovery now keeps original artifact identity separate from
current engine configuration. Clear and containment use the durable old interface
and table, including retry after the release checkpoint. Select and Resume still
require the configured interface. Foreign live runtime is rejected before native
effects. After Clear, read-only observation checks absence of old artifacts and
the current ordinary runtime identity/UAPI/default routes independently. This
does not migrate saved exit selection to a new interface or qualify OS effects.

Transition validation: goimports, vet, lint (0 issues) and the full short suite
passed; internal/client 126.442 s and cmd/endlessnet-client 11.771 s. The first
run exposed an uninitialized test RPC state and a scope check placed after guard
construction; both were corrected before the passing run. Native/system tests
were not run.

Cross-platform short CI for `36ff58a` passed on Linux, macOS and Windows
([run 35618126821](https://github.com/endless-net/client/actions/runs/35618126821)).
Native/system qualification jobs were skipped, as required for push CI.

Resources/probe increment validation (2026-09-21): `goimports -w .`,
`go vet ./...`, configured golangci-lint (0 issues) and `go test -short ./...`
passed; internal/client 121.081 s and cmd/endlessnet-client 11.187 s. The positive
HOST projection/event fixture now applies the same persisted configuration
representation as ConfigStore. Standalone status without an engine reports cached
facts without a readiness request. Native OS acceptance remains outstanding.

LAN_ALLOW decision (user-approved, 2026-09-21): permit only directly connected
subnets of physical Ethernet/Wi-Fi interfaces, including public subnets. No manual
CIDR list is required. RFC1918/ULA classification grants nothing by itself.
Gateway-routed networks, other VPNs, bridges and container networks are excluded.
Each permission binds the exact prefix and interface instance; route changes must
not open a gateway bypass. Overlay/resource routes take priority. Exit loss or
unconfirmed enforcement closes LAN access. Application link-local and broadcast/
multicast discovery are excluded for now; DHCP/ND are handled separately.

This explicit decision resolves the semantic question left open by pinned BA
Q-07 and UI SA US-05. Pinned clientapi already supplies AllowedLANAccess; do not
invent a CIDR extension or treat the enum alone as OS evidence. Architecture
follow-up records the accepted semantics; native classification, bound rules,
readback, invalidation and acceptance remain client implementation work.

Native host/restart increment (2026-09-21): the agent now constructs an opaque
Linux exit runtime from its actual engine, store and shared operation mutex. The
exit worker starts on the same service as RPC, independently of whether a local
listener is enabled. One supervisor cancels the agent before joining a failed
worker's siblings; Serve also reports failure before waiting for external
lifecycle lock owners. Normal stop joins exit maintenance/observation and RPC
workers before engine teardown. A public native worker advertises CAPABILITY_EXIT_NODE
only for its own live token; cleanup cannot revoke a replacement worker.

The profile driver's saved-selection path now uses protected resume with fresh
persistent-context checks before and after effects. Select requires explicit
connected intent at admission, durable dispatch/completion and the native
boundary. Missing/empty/disconnected intent is unavailable, never an implicit
Connect; durable replay and Clear remain available independently. Deferred
Select-before-Connect remains a product decision rather than implemented behavior.

Successful Clear atomically replaces retained protection with a nonsecret durable
observation scope. After restart and operation pruning, the native observer
reconstructs only the original artifact address and repeats all absence checks.
Inactive-profile cleanup keeps original artifacts distinct from current active
identity. Credential rotation does not erase this read-only scope or grant it
control authority. New Select success discards it. Automatic interface migration,
LAN_ALLOW, complete profile/network transitions with retained exit,
and native/system qualification remain open; host wiring is not acceptance.

Clear can adopt a committed ordinary runtime only after matching its owner,
profile, control origin, node/network, interface/table and actual UAPI. Adoption
withdraws the packet filter and grants no control-plane authority; ambiguous
containment retains ownership for retry. Engine rollback restores the bounded
runtime identity along with the previous device. Select and Clear reject invalid
or reserved route tables before journaling; retained Clear validates its original
scope independently of current enrollment. Unit assertions cover foreign owners
with the same node, failed containment retry and device-recreation rollback.

Startup policy fetch now uses requested intent, including only a context-bound
connected recovery checkpoint, or explicit runtime_start CONNECT. An unavailable
policy reason alone no longer initiates control traffic on repeated startup.
The existing no-saved-intent command regression and seven source-admission
scenarios cover this distinction without changing lifecycle defaults.

Host/restart validation: goimports, vet and lint (0 issues) passed. The final
full short suite passed: cmd/endlessnet-client 10.563 s, internal/client reused
its successful 118.000 s run. Earlier failures exposed one incomplete exit
fixture and automatic control probes without a connection request; both were
fixed while retaining the original assertions. No native/system run was made.

Active readiness probes now request the engine's control HTTP transport. Missing
engine, factory refusal, nil transport, cancellation or body-read failure cannot
fall back to ordinary networking or report readiness. The copied client has no
cookie jar, refuses redirects and closes idle connections. Disconnected/offline
background observations remain passive. This is transport wiring with unit
assertions; native DNS/exit revocation still needs platform acceptance.

HOST observation increment: the native Linux observer reads assigned interface
addresses, policy rules and unforced unmarked /32 or /128 route lookups, then
rechecks interface/rules. Live UAPI identity, PSK/routes/endpoints and a fresh
direct peer path are distinct requirements. The engine's saved routerCfg list
does not count as OS route evidence. Unknown policy-routing selectors and
ambiguous native results remain unknown. Relay paths now use the separate bound
bridge evidence described above. HOST confirmation does not
establish remote application health, and never promotes SUBNET/SERVICE/APPLICATION.

ListResources performs native reads outside the RPC mutation mutex under the
shared effect lock, then rechecks owner, profile worker, persistent context and
map expiry. Applied packet restrictions take precedence over positive evidence.
The resource clock fingerprints confirmed host identities as well as denial
state; path/route loss can invalidate DOMAIN_RESOURCES without a filter change.
Full subnet/service/application observations, rollback/restart audit and
native traffic qualification remain open.

Read-model increment (2026-09-21): post-Clear observations retain an in-process
scope receipt after verified Release and bind it to terminal Clear before first
publication. Every read independently checks nft/rule/default-route absence;
an ordinary live engine must also have no exit projection and matching actual
UAPI without defaults or transport mark. Operation pruning after the first
confirmed commit does not erase that scope. Restart without the receipt remains
unavailable in that earlier increment; the native host/restart increment above
replaces this receipt with an atomically committed durable scope.

Catalog modes now intersect authenticated grants and exact worker-supported
pairs without widening the two wire lists into unsupported Cartesian choices.
Aggregate mutation control reflects Clear admission, including offline Clear;
it does not promise that every Select is available. Readiness is rechecked after
native observation. Worker install/removal invalidates existing event streams
with STALE_STATE for rebootstrap, without advertising the unfinished capability.
A separate cancellable/joined observation loop publishes DOMAIN_EXIT_NODE on
normalized changes (including first confirmed evidence), not repeated timestamps
or private command errors. Its digest is never used as evidence by GetExitNode.
This increment passed goimports, vet, lint (0 issues) and the full short suite
(internal/client 104.634 s). The first short run caught an invalid comparison
between caller-attached store metadata and the persistent update document;
the revision CAS now compares persistent snapshots while retaining the full
configuration/source checks around native observation. Platform qualification
was not run. Specific assertion links are in the local requirement matrix.

Host integration audit (2026-09-21): the effect lock must be shared by the
native exit adapter, profile driver and ordinary map loop. The map-loop recovery
barrier now includes `ExitChange`, preventing the main loop from applying cached
or refreshed maps between native dispatch and durable selection commit. Connect
after restart now uses protected saved-selection resume rather than ordinary
Configure with an empty in-memory selection. The native worker is supervised
outside Serve, so disabling the optional local listener does not skip it.

The pinned architecture revision `bdb5ba63c0e5356122c0760f4d63205e84ef507d`
(client UI BA BP-UI-02/BP-UI-09; headless SA SR-12/F-07/IT-30) requires that
Disconnect stop local access without removing enrollment, and that exit failure
never silently fall back to ordinary routing. It does not specify whether Select
while disconnected is rejected or deferred until Connect. Admission, dispatch
and completion must preserve that independent connection intent. The UI's
prohibition on automatic command retries is not a prohibition on runtime
recovery of the same authorized connected intent. Existing guarded Configure
already recontains before reapplying a matching live selection; host integration
must distinguish this path from restoring a stopped selection.

Lifetime/resume increment (2026-09-21): the exit worker owns a one-second
maintenance loop, cancelled and joined before worker shutdown. It skips busy
effects and reads current configuration only after acquiring the shared lock.
Changes admitted during observation trigger an immediate second check. Native
maintenance observes the actual engine-owned scope; failed authority or native
evidence withdraws packet policy before bounded independent containment. A
stopped runtime still checks and repairs its retained firewall. Failed repair
remains an error and is retried; no maintenance error proves blocking.

A separate saved-selection callback can resume a stopped protected runtime with
connected intent, current profile/origin/identity/map and no transition barriers.
It does not manufacture a Select operation. An already running selection is
observed without silently reopening withdrawn enforcement. The worker rechecks
durable context after resume and invokes maintenance before releasing the effect
lock when admission changed it. Lock acquisition for operation reconciliation is
now cancellable, including while lifecycle holds the effect lock.

WireGuard evidence additionally compares derived live/committed local public
keys with the authenticated map, and checks every live peer's PSK against the
intended value. Comparison models retain only public identity and PSK digests;
raw keys and UAPI errors are not returned. These are bounded periodic checks,
not an atomic lease on external routing/firewall state or reachability proof.
The later read-model increment adds catalog/control/events, a main-loop pending
barrier and in-process post-Clear observations. The host/restart increment adds
native worker lifetime, capability and durable cleared scope. Changed-interface
recovery and LAN_ALLOW remain open. Select admission/application now require
explicit connected intent, as does saved resume. Recovery of a withdrawn but
still live runtime needs full guarded map-loop assertions; a permanently
unknown/closed state is not
completion of connection recovery.
This increment passed goimports, vet, lint (0 issues) and the full short suite
(internal/client 108.985 s). `native_exit_maintenance_test.go` covers failed
evidence, containment ordering, stopped protection and bound pending effects;
`service_rpc_exit_maintenance_test.go` covers fresh snapshots, retry, cancellation
and joining. Native/worker resume tests cover stopped application, refused
transitions and admission changes during effects. These fixtures use injected
OS command evidence, not native platform qualification.

Native adapter increment (2026-09-21): the Linux executor now binds effects to
the dispatched durable operation and protection scope, applies through the
engine, and independently observes firewall/routes and committed packet policy.
Dispatch rereads the saved RUNNING operation after its transaction commits;
the callback's earlier snapshot still contains the previous serialized state
and cannot be used as native authorization evidence. A changed binding during
that interval starts containment instead of applying the old snapshot.
Live WireGuard UAPI must match committed peer keys, endpoints, canonical route
sets and the guard's transport mark; selected defaults cannot belong to another
peer. The guard supplies the mark even when a platform route projection omits
it. DNS-dependent application also rechecks the engine's current source lease.
UAPI comparison is bounded and never returns raw keys or device output. Engine
path reconciliation updates the committed endpoint; autonomous roaming remains
unconfirmed until reconciled. This is configuration evidence, not a handshake
or application-reachability guarantee.
Clear uses stopped-runtime recovery before its durable release checkpoint;
Release repeats cleanup and observes absence. Read-only RPC observation is
serialized with effects and must not promote an old operation result to current
runtime status. The later lifetime/resume increment supplies worker-level
monitoring and saved resume; host wiring is now implemented, LAN_ALLOW remains open.
The read path observes settled active selections and, after the later read-model
increment, independently verified Clear using a durable observation address.
Pending changes, inactive profiles and Clear without its original scope remain unknown.
The Clear operation itself requires observed cleanup/release before success.
The production host owns catalog/events lifetime and advertises exit capability
through the public native handle, not through arbitrary injected callbacks.
The later changed-interface increment permits explicit Clear against original
protection even when configured interface changed; Select/Resume still require
the configured interface. Automatic migration is not implemented.

The firewall now preserves ordinary output/forward policy for the unselected
family in IPv4-only/IPv6-only modes and restricts its TUN gate to the selected
family. Containment still closes both families. Startup distinguishes confirmed
containment without control authority from unconfirmed protection: only the
former may continue to authenticated local recovery RPC, without startup policy
traffic. The final full short run passed (internal/client 103.293 s), alongside
goimports, vet and lint (0 issues); native qualification remains required.
Evidence sources for this increment are `native_exit_executor_test.go`
(all family modes, durable checkpoint and ambiguous release),
`native_exit_uapi_test.go` (live configuration tampering),
`service_rpc_exit_observation_test.go` (authorization/context/source races),
and `exit_startup_recovery_test.go` (confirmed containment without authority).
Their injected nft/ip responses do not prove native kernel enforcement.

Restart cleanup increment (2026-09-21): stopped-engine recovery first confirms
containment, revokes control underlay, inspects both route families and removes
only the direct default tuple belonging to the durable interface/dedicated
table. Each deletion is followed by readback, so partial progress can be retried
by a new process without router.current. Release repeats recovery before its
final absence checks and never treats command failure as proof of absence.
Reserved tables 0/253/254/255 are rejected before guard construction.

Both exit policy rules now bind explicitly to the owned mark, including main
suppression. Cleanup preobserves the whole target-table dump, requires an exact
rule with a unique priority, deletes with priority/full mark mask and confirms
absence. Generic main suppression and rules belonging to other marks are not
deletion grants. No generic-rule compatibility deletion is retained. The global
src_valid_mark sysctl is not reset: its previous value is not journaled and it
may be shared with another VPN. Exact tuple ownership cannot distinguish an
external administrator replacing the same tuple between dump and delete;
iproute2 does not provide an atomic compare-and-delete operation.

DNS lifetime/route increment (2026-09-21): a single engine-owned, lazily started
source lease serializes explicit checks and one-second monitor ticks; an
observation has a five-second bound. Source failure permanently revokes the
lease, cancels HTTP requests through body lifetime and closes its bounded socket
registry, including late successful dials. Read/write guards use cancellation
state rather than spawning native commands. Explicit recapture replaces the
lease; protected Down retains it for recovery, final Close/release revokes it.
Flow and relay compare lease identity as well as DNS snapshot identity.

DNS socket creation observes `ip route get` with the socket mark, family,
protocol, destination port and optional bound interface before and after
connect. The selected interface and preferred source must match captured
non-owned, up interface state. This includes configured global DNS servers.
These are bounded point-in-time checks, not an atomic kernel routing lease;
route/firewall changes between observations, native socket enforcement and
platform acceptance remain open. Tests use injected command output, socket
wrappers and a local HTTPS streaming response, not privileged networking.
The JSON fields (`prefsrc`, IPv6 `from`, `mark`, `cache`, named `dev`) were
cross-checked against [iproute2's route printer](https://github.com/iproute2/iproute2/blob/main/ip/iproute.c).

Earlier native adapter audit (2026-09-21): restart Clear/Contain has an exact scoped
cleanup step for the recovered guard; ordinary Down alone still does not prove
that all kernel effects from a previous process disappeared. Adapter
callbacks were added by the later increment above; host worker startup/shutdown,
map-loop reconciliation and live invalidation remain required before readiness.

Pre-exit DNS increment (2026-09-21): the Linux source provider reads actual
systemd-resolved Manager/Link properties and native interface addresses, bound
to one unique D-Bus owner, and requires two equal observations. It excludes the
owned Client link and local/stub endpoints, preserves routing domains and link
scope, and rejects unsupported DNSSEC/DoT instead of downgrading them. Source
capture after containment is safe from Client stub recursion because the Linux
router changes only its own link; no stale DNS cache is restored after crash.
Literal authorized IP endpoints do not require a DNS source.

The marked dialer resolves only exact authorized control/relay names, uses
longest-domain selection, direct marked UDP/TCP with truncation retry and a
bound interface index, and never consults the default resolver/hosts/search
path. Engine capture/recovery, control HTTP, flow transport and relay reuse now
carry immutable source identity. Source change rejects lookup/connection and
HTTP pre/post-header checks; relay Ensure stops an invalid source. Flow transport
replacement retains the same consent/spool worker identity. Application discovery
and the general DNS proxy do not receive this privileged path.

CNAME-only replies continue through the captured routing domains with cycle and
depth bounds. A/AAAA queries share a four-second budget; connection attempts
interleave families, use at most two sockets and a two-second per-attempt bound,
and close/join losing attempts before returning. The caller retains the total
deadline. `underlay_connect_test.go` covers cancellation, late success, family
ordering, source replacement and preservation of socket-mark error identity.

`underlay_dns_source_test.go`, `underlay_dns_resolver_test.go`,
`underlay_dns_engine_test.go`, control transport and relay recovery tests cover
these injected boundaries. They do not prove native busctl/SO_MARK/interface
effects. The later lifetime/route increment above adds lease revocation and
global DNS route observations. Encrypted DNS modes, atomic route/firewall
invalidation, full native adapter and platform acceptance remain open. Neither
increment advertises a ready exit worker.

Firewall observation increment (2026-09-21): `exit_guard_observation.go`
validates numeric nft JSON after Contain/OpenTunnel, including the owned inet
table, both drop base chains, exact output exemptions and requested TUN gate.
Duplicate keys, malformed/oversized JSON, extra objects and widened predicates
are rejected. Release requires a successful table listing without the owned
table; command failure is not absence. Ambiguous Open/Release attempts bounded
containment with an independent cancellation context and still returns failure.
The engine retains ownership for retry. Stateful runner fixtures and
`TestExitGuardNativeSuccessRequiresReadbackAndRecoversAmbiguity` cover these
ordering/error/cancellation branches. The schema was checked against upstream
[nftables](https://www.netfilter.org/projects/nftables/manpage.html) and its
[1.1.5 source](https://www.netfilter.org/projects/nftables/files/nftables-1.1.5.tar.xz).
Only exact additional IPv6 protocol prerequisites are normalized. Combined
Neighbor Discovery kernel readback, actual packet enforcement and ongoing
invalidation still require native qualification/implementation; this does not
enable the production exit worker or close IT-30/US-05.

Release CI scenario migration (2026-09-15): the old HC-036/HC-038 fixtures
expected a peer's signed default route to activate consumer exit routing without
SelectExitNode. That contradicts the current no-selection projection. The native
IPv4/IPv6 exit-route scenario now proves those defaults remain ineffective,
including withdrawal and reappearance, and uses an authorized host route as a
positive TCP/UDP forwarding control. The provider scenario retains real default
advertisement, forwarding/SNAT, revocation, established-session withdrawal and
restart checks using an explicit resource route on its consumer. It also checks
ordinary LAN access remains independent of that route. The superseded offline
exit-LAN CLI fixture is removed. These checks do not close HC-036 explicit exit
activation, HC-037 selected-exit LAN policy, or complete HC-038 consumer/provider
acceptance; those remain open with the native exit runtime work below.

Local verification observation (2026-09-14): the resume-policy short run initially
hit Windows TCP bind access-denied errors in
`TestDNSProxyServesTCPOnTheUDPAddress` and
`TestDNSProxyPairRecoversFromTransportSpecificExclusion`. The final full short
rerun passed unchanged DNS code. Source inspection then found that a failed
ephemeral selector bind terminated before trying the other transport. Selection
now continues within the existing 16-attempt bound; explicit ports still get
one attempt. Deterministic socket units cover either selector failing,
transport-specific exclusion, full exhaustion, fixed-port failure and ownership
of all retained reservations. The separate loopback TCP DNS response/shutdown
test remains. A later source-refresh short run also exhausted this bound in
`TestServiceDiscoveryThroughRunningDNSListener`. Native `:0` selection now draws
independent candidates from the dynamic/private port range instead of relying
on sequential OS allocations through excluded ranges. Both transports must bind
the same candidate; explicit ports, the attempt bound and socket cleanup remain.
`TestDNSPairCandidateEscapesContiguousTransportExclusion` covers the range jump;
`TestDNSPairCandidatePreservesBindingAndEntropyFailures` covers explicit/IPv6
binding and incomplete entropy. These units do not prove every Windows exclusion layout has an
available pair, and host exclusion qualification remains open. No native/system
acceptance was run.

The user also accepted KEEP_INTENT defaults for user_logoff, suspend and resume
on 2026-09-14. Per-profile local preferences now persist those choices, resolve
signed account/device baselines and locks, expose matching preference/managed
catalogs, and reset selectively to policy or the accepted default. Missing
authenticated authority for an enrolled profile exposes unknown effective
behavior; rejected source or locked choices cannot partially commit a patch.
The shared network/resource worker captures all three settings across restart,
commits them only with the complete effect, and retains old settings on failure.
`service_rpc_desktop_preferences_test.go`, the worker apply/containment matrix
and native catalog checks cover these preference semantics. They do not prove
OS event execution: trusted logoff/suspend/resume adapters and CONNECT execution
for those events remain unimplemented, and platform qualification remains open.

The engine now provides a mutex-serialized suspend gate: teardown blocks a later
Configure, failed cleanup keeps the gate closed, and Resume never reapplies the
old map. Failed route removal retains the router for retry, including when the
device is already gone; Configure cannot replace that unresolved cleanup.
Windows router teardown now propagates enumeration/removal errors and retains
its pending state instead of returning success on a repeated Down. Its native
script enumerates then filters the owned interface so absent entries can be
retried without suppressing command failures. Engine and Windows command-runner
units cover these paths, not actual platform cleanup or SCM event delivery.
Windows SCM power events now feed the gate through the agent consumer described
below; other native event adapters remain open.
The internal `ApplyRuntimeLifecycleIntent` handler now resolves signed
logoff/suspend/resume policy and commits the current intent decision before
cancelling an in-flight apply. It validates the logoff owner, rejects unknown
policy without mutation, preserves newer Disconnect under KEEP_INTENT, clears
a startup-recovery checkpoint under managed DISCONNECT, and propagates that
disconnect to a pending profile-switch target. Runtime lifecycle tests cover
these decisions, restart and duplicate delivery; no public RPC operation or
completed OS teardown is fabricated. Native event delivery, worker ordering,
source-failure recovery and CONNECT behavior remain to be integrated.
The new `RuntimeLifecycleExecutor` joins those intent decisions to the engine
Suspend/Resume/Down interface and the same operation lock used by ordinary
workers. It retains that lock across suspension and failed resume; an
unconfirmed Down or unavailable policy cannot release the gate. Resume wakes
ordinary reconciliation without applying a saved map. Foreign logoff is rejected
before engine access, and Close requires cancelled worker lifetime so shutdown
cannot release live workers into an accidental reconnect. Executor units cover
failed teardown, failed resume/source, Disconnect during suspend and shutdown.
The Windows service now accepts SCM power notifications and queues copied
PBT_APMSUSPEND / PBT_APMRESUMEAUTOMATIC values to the agent executor. It ignores
the subsequent user-interaction resume notification to avoid a second lifecycle
decision. Microsoft documents the automatic wake event as occurring on each
[resume](https://learn.microsoft.com/en-us/windows/win32/power/pbt-apmresumeautomatic)
and the limited processing time for
[suspend](https://learn.microsoft.com/en-us/windows/win32/power/pbt-apmsuspend).
The bounded queue cancels the runtime on overflow instead of silently dropping
an event. The agent serializes delivery, retries an unsuccessful latest transition,
and releases the held worker lock on cancelled lifetime before worker shutdown.
Headless service mode uses the same durable mutations without requiring an IPC
listener. `windows_service_lifecycle_windows_test.go` covers scalar delivery,
ignored events, stop and overflow; `agent_runtime_lifecycle_test.go` covers
durable default intent, held gate, retry, resume wake, source loss and shutdown.
These are synthetic source/engine units. Resume now confirms teardown under
the effect lock before refreshing an expired/missing signed policy snapshot,
including when current intent is disconnected. The common snapshot fetch keeps
startup admission separate; offline mode validates local authority without
contacting control. `RefreshRuntimeLifecyclePolicy` uses a fresh full-context CAS,
validates recipient/signature/expiry/revisions/hash and commits only authority,
with domain invalidations on a changed revision. It never adopts fetched intent,
credentials or profiles. Resume resolves current intent after refresh, so a
newer user Disconnect is preserved. Fetch/validation/CAS failures retain the gate
and the consumer retries. Primitive units cover rejected authority, cancellation,
owner/intent races and idempotence; source units cover a real signed HTTP snapshot
while disconnected; executor units cover source failure and subsequent recovery
inside the closed gate. Native delivery, callback latency, effect observation,
logoff, non-Windows subscriptions and the full suspension/recovery race audit
remain open. The b4165b8 short run passed on all three OS runners.
Lifecycle transitions now publish a bounded status observation under the shared
effect lock. Confirmed teardown projects DISCONNECTED while preserving a
connected KEEP_INTENT; failed teardown projects DISCONNECTING. Resume never
projects CONNECTED before a new ordinary apply. A fresh profile/map-bound agent
snapshot clears previous path/relay/apply successes, with fixed failure keys
instead of raw source bodies and no control-plane probe. Observation failure
retains resume serialization for retry. Executor units cover confirmation and
publication ordering/failure; agent units cover stale-path replacement, failure
privacy, no probes and separation of intent from dataplane state. Full native
event/subscriber delivery, restart semantics and resources/exit effective-state
observations still require the remaining audit and platform qualification.
`runtime_lifecycle_events_test.go` now exercises the executor with the actual
mutation observation, snapshot and subscriber queues: failed teardown reports
DISCONNECTING, retry reports DISCONNECTED with connected KEEP_INTENT, and resume
cannot invent CONNECTED or an RPC operation. Owner/observer projections share
the committed revision and maintain per-stream sequence, while observer output
omits private identity/failure details. Reattachment receives a fresh stopped
snapshot; reopening durable storage in a new mutation runtime preserves intent
without replaying a previous runtime's observation as evidence of a tunnel.
Native transport/SCM timing, interruption during durable writes and complete
cross-domain invalidation coverage remain separate open evidence.
The pinned x/sys SCM host forwards SessionChange EventData after its callback
returns; the adapter must copy session data during a live native callback rather
than dereference that forwarded pointer later.
The session-owner component now binds a WTS session ID to the same SID string
used by the named-pipe peer. A newer logon invalidates the old binding before
lookup; a delayed lookup cannot restore it after replacement or logoff. Initial
enumeration uses temporary logoff tombstones, so stale enumeration cannot
resurrect a departed session. Bindings/tombstones are bounded, duplicate/unknown
logoff has no owner, and post-enumeration logoff frees its slot. Unit races cover
replacement, failed lookup, departure, late seeding, duplicate events and limits.
Windows source functions enumerate copied session IDs and retrieve SID from
[WTSQueryUserToken](https://learn.microsoft.com/en-us/windows/win32/api/wtsapi32/nf-wtsapi32-wtsqueryusertoken),
closing token handles and freeing enumeration buffers. They require the
documented LocalSystem/SE_TCB_NAME service context; that OS execution is not
locally qualified. The owner component is now attached to SCM. Safe copying
of [session notification](https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-wtssession_notification)
data is implemented by the dispatcher below.
The service now uses a local SCM dispatcher instead of forwarding native data
through x/sys's asynchronous EventData field. Its HandlerEx callback validates
and copies the WTS session ID while native data is live; the queue contains no
pointer. Malformed session data or a full bounded control queue cancels the
runtime. The status pump drains shutdown after reporting failure, joins runtime
cleanup, and reports STOPPED exactly once as required by
[SetServiceStatus](https://learn.microsoft.com/en-us/windows/win32/api/winsvc/nf-winsvc-setservicestatus).
Synthetic callback/status-pump units cover copy lifetime, invalid data, overflow,
reporting failure and terminal ordering. Power and SessionChange use this
dispatcher. Startup enumerates sessions, seeds SID bindings and fails before
runtime launch if enumeration fails. Logon refreshes the binding; logoff consumes
it and sends an owner-bearing internal notification to the agent. Unknown,
duplicate and failed-lookup logoffs never infer the current profile's owner.
The consumer rechecks the current local owner before execution; mutation
admission rechecks it under its lock. Logoff retries cannot replace pending power
retries, and a foreign logoff leaves a failed suspend retry intact. Source units
cover seeded/reused sessions, unknown owners and enumeration failure; agent units
cover owner-bound DISCONNECT and foreign-logoff/power-retry interleaving.
Before resume releases the stopped gate, refreshed policy also resolves an
earlier failed owner logoff; otherwise resume remains pending. Unrelated events
do not postpone an already armed retry timer. Agent tests in
`agent_runtime_lifecycle_pending_test.go` hold teardown at a barrier and verify
that an unavailable logoff decision is committed before engine resume once
local policy becomes available; a replaced owner retains its newer intent.
An unsuccessful offline policy refresh neither resumes the engine nor wakes
reconciliation, and a later successful attempt resolves the pending logoff.
These are consumer-ordering units, not signed-map acquisition or native OS
execution evidence. A committed logoff decision is now distinguished from a
failed teardown/observation: effect failures yield to ordinary reconciliation
or resume's gated teardown and do not enqueue the original policy decision
again. `agent_runtime_lifecycle_reconnect_test.go` verifies that a newer Connect
intent survives resume after logoff's first Down failed. This does not yet
establish ordering for every unresolved-policy/user-mutation interleaving.
Power transitions also retain whether their intent decision committed: a
same-transition retry after teardown or observation failure does not overwrite
a newer intent. Opposite power events start a new decision; unresolved policy
is still retried, and resume still refreshes authority before opening the gate.
`runtime_lifecycle_power_retry_test.go` covers failed suspend teardown,
failed suspend/resume observation, newer Connect preservation and the next
power cycle applying policy again. These process-local markers do not replace
the remaining crash-recovery and native event-ordering audit.
The Windows service also retries
one unresolved WTS owner per second, using round-robin selection within the
bounded registry. Resolved or departed sessions are not queried; an in-flight
lookup is not duplicated. Every completion checks the original binding before
storing SID, so a retry cannot overwrite a newer logon or resurrect logoff.
`windows_session_retry_test.go` covers recovery, persistent-failure fairness,
bounded calls, duplicate lookup suppression and replacement/departure races.
Source-health projection, recovery of an already missed unknown-owner logoff,
native lookup latency and complete interleaving evidence,
native callback ABI, SCM execution and
non-Windows lifecycle sources remain open for further implementation/audit and
the agreed platform qualification stage.

Linux/Darwin routers now retain a cleanup plan containing only failed or
unattempted steps. Down propagates failures; Configure cannot overwrite pending
cleanup; failed initial setup also retains its cleanup obligation. Successful
steps are not repeated. A failed interface-bound removal can complete only when
interface enumeration independently confirms absence; cancellation or failed
enumeration cannot establish that postcondition. Linux route removal uses
exact prefix, device and table selectors with
[ip route flush](https://man7.org/linux/man-pages/man8/ip-route.8.html), allowing
already absent selected routes without broadening removal to other prefixes.
`router_cleanup_test.go` covers partial completion, retry, cancellation and
absence/observation failure on every host. The OS-specific cleanup suites exercise
DNS, route and interface failures and blocked reconfiguration in Linux/macOS
short CI; Windows-local checks do not execute those OS-tagged tests. Actual
platform cleanup, Linux policy-rule absence and external deletion/replacement
of macOS routes still need qualification and reconciliation evidence.

Linux policy-rule cleanup now checks the family/table/mark-filtered JSON dump
after deletion, including when the delete command failed. Only confirmed absence
finishes the step; remaining duplicates, malformed/oversized dumps, failed
observation and cancellation retain it. The parser uses iproute2's
`suppress_prefixlen` JSON attribute for the main-table suppression rule, as
defined by the upstream [rule implementation](https://github.com/iproute2/iproute2/blob/main/ip/iprule.c).
The shared runner/parser tests cover both families and selectors locally.
Native policy-rule ownership and platform effect qualification remain open.
The a980455 push short run passed on Linux; macOS verification was skipped by
the old push gate. Push unit CI now runs one short pass on Linux, Windows and
macOS, including the nested IPC module, with no installer/system jobs enabled.
The 65bf9f5 push short run completed successfully on all three OS runners,
including the OS-tagged cleanup tests. This is unit evidence, not native
installer/networking or lifecycle acceptance.

The user accepted the runtime-start default on 2026-09-14: KEEP_INTENT,
with DISCONNECT when no explicit intent is saved. The agent now initializes
that durable baseline under its lifetime lock before engine creation, RPC workers
or network sync. Existing connected/disconnected intents keep their reason and
timestamp; malformed saved intents cannot imply Connect. The short test
`TestRuntimeStartPreservesExplicitIntentAndDefaultsDisconnected` checks initial
state and restart. Runtime-start now supports local KEEP_INTENT/CONNECT/DISCONNECT,
signed account/device baselines and locks, requested/effective/source projection,
selective reset and atomic patches with UI_QUIT or network preferences. Preference
mutation changes future startup behavior, not the current connection intent.
The real agent startup resolves the committed setting before network work;
unverifiable policy holds networking down while leaving local recovery available.
CONNECT cannot supersede a nonterminal operation's saved recovery intent.
`service_rpc_runtime_start_test.go` checks policy/lock/reset/replay, source failures,
owner denial and pending Disconnect; the network worker matrix includes runtime-start
commit/rollback. Logoff/suspend/resume inputs and platform qualification remain open;
startup refresh when the authenticated cached policy is absent/expired also needs
a control-plane recovery audit. These tests do not close UF-21 or UI-AC-21.
Startup additionally rejects a missing/mismatched active profile and holds an
enrolled identity with no cached map down instead of executing local CONNECT or
restoring a connected intent. The context regression checks persisted blocking
and restart without rewriting recovery records. Automatic signed-source refresh
now has a bounded real agent preflight: when reconnect is intended and the cached
authority is unusable, it fetches a full map over the producer stream, verifies
trust/signature and adopts only map fields if identity, intent, profile/recovery
state and prior authority are unchanged. It runs before engine creation and never
applies routes. A local HTTP fixture covers signed success, tampering, cancellation
and Disconnect during fetch. A private checkpoint in the blocked intent now keeps
the original KEEP_INTENT across an offline failure and restart. Recovery binds
profile, owner, identity, credentials and local startup choice, then applies the
fresh signed policy to the original intent. Tests cover restoration, explicit
Disconnect, identity/credential changes and a new managed DISCONNECT. The running
agent now retries source recovery from the confirmed-Down branch under its shared
effect lock and existing retry/backoff loop. Map authority and resumed intent
commit together after CAS and pending-operation checks, with one revision and
catalog/preference invalidations. The retry unit matrix covers success, signature
tampering, expiry, concurrent Disconnect, pending exit work, retained enrollment
recovery and cancellation;
connected intent does not imply applied network state. Full platform and
control-plane outage/recovery qualification remain open.
Missing-map reads now preserve requested intent but report unknown
effective value/source and temporary unavailability; set/reset reject the whole
patch without modifying durable settings. The context regression checks this
projection/admission parity. Unregistered profiles retain the accepted default.
Local vet/lint and the final short run passed. A preceding short run exhausted
the DNS TCP/UDP ephemeral-port pairing attempts on Windows (bind access denied);
the unchanged listener passed on repetition. This is not evidence of platform
binding reliability or lifecycle acceptance.

Exit selection admission now persists the authenticated map payload hash.
Preflight, completion and retry reject replacement maps even when recipient,
revisions and the selected grant still match. Queued work fails without dispatch;
dispatched work retains its guard until containment succeeds, including restart.
`TestExitSelectionBindsSignedMapAcrossRestartAndApply` covers both paths using
valid signed replacement maps and an injected executor. Clear remains possible
without a map. This does not supply the missing OS exit adapter or qualify
fail-closed behavior on a platform.

The Linux OS protection component `exit_guard_nft.go` now submits interface-scoped
nftables batches through the native executable. Output containment covers both
IP families, exempting loopback, marked TCP/UDP underlay and bounded host IPv6
Neighbor Discovery outside the guarded TUN; forwarding is
blocked. Opening permits output through the TUN. Rule replacement and repeatable explicit
release use single transactions, without global ruleset changes or automatic
cleanup after ambiguous errors. `TestLinuxExitGuardAtomicContainmentAndRelease`
and `TestLinuxExitGuardRejectsUnsafeIdentityAndCancellation` check command-boundary
ordering, scope, restart, rejection and cancellation. They do not execute Linux
netfilter. Transaction semantics follow the upstream [nftables documentation](https://wiki.iptables.org/wiki-nftables/index.php/Atomic_rule_replacement).
This component is not wired into the native exit worker and adds no capability.
The 2026-09-21 restart increment reconstructs its exclusively owned table in the
same transaction instead of flushing only rule lists. This removes retained
table flags and chain structure before recreating the expected output/forward
hooks with drop policies. The initial add handles an absent table; deletion is
never submitted alone during containment or opening. Unit assertions distinguish
atomic replacement from explicit release and preserve the no-cleanup-on-error
contract. Native transaction execution and firewall readback remain unqualified.
Integration still needs engine/route serialization, durable startup containment,
underlay control/relay/DNS and IPv6 neighbor discovery requirements, family/LAN
modes, kernel observations, packaging dependency checks and privileged CI.
Windows and Darwin outside-TUN protection also remain open.

Exit reconciliation now requires the adapter to supply the same effect mutex as
connection/profile/map application. It holds that lock through preflight, native
apply, durable completion and any late-context containment; callbacks must not
reacquire it. Missing locks reject without a dispatch checkpoint, and cancellation
is checked again after waiting for existing OS work. `service_rpc_exit_lock_test.go`
covers these boundaries and accepted Disconnect during Apply. This establishes
the executor locking contract, not native host wiring or platform acceptance.

`service_rpc_exit_worker.go` adds a service-owned exit reconciliation loop with
startup scanning, five-second retries, duplicate-worker rejection and joined
cancellation. Ambiguous Apply keeps its original operation and durable deadline;
temporary containment failures retry while unexpected reconciliation failures
stop the worker. Tests reopen an accepted operation from disk, exercise a real
timer retry without another request, and cancel an in-flight native callback
without dropping its journal. The native Serve path still needs startup OS
containment and a complete adapter before this worker may expose exit readiness.

Public SelectExitNode/ClearExitNode handlers now route admission through the
exit worker lifetime lock and wake only after durable acceptance. Without a
live worker they authenticate and resolve exact persisted replay before rejecting
new effects. `service_rpc_exit_public_test.go` covers pending/completed replay
across restart, cancelled worker lifetime, foreign/anonymous callers, payload
conflict under an existing UUID, fresh rejection and acceptance-before-wake.
The native host still supplies no exit adapter and does not advertise exit
mutation readiness; these handlers do not establish OS implementation/evidence.

Explicit engine route/UAPI projection now uses a shared peer transformation that
checks the original signed exit authority before deriving application routes.
Default prefixes are filtered after application projection as well. Adapter-only
builders accept an explicit selection and observation time; ordinary engine paths
and static export still cannot infer activation from persisted selection or `/0`.
OS route input rejects explicit selection when route acceptance/installation is
disabled. The exit TUN policy uses the same application-route projection while
the independent application filter still enforces its own expiry and ports.
`exit_projection_engine_test.go` checks both families, application route
preservation, original-map immutability, rejected authority and suppressed routes.
These are preflight builders; native guarded engine application remains open.

The engine now has a guarded explicit-select path: nftables containment precedes
preflight/TUN/routes/UAPI; the TUN exit filter commits before the guard opens its
interface. Transitioning from an ordinary TUN recreates that runtime under the
guard so filter attachment cannot race packet processing. Failed guarded apply
closes under protection instead of restoring previous exit authority. Endpoint
refresh uses the selected signed projection and contains invalid authority;
ordinary Configure cannot implicitly restore persisted exit intent or clear a
guard. `exit_guarded_engine_test.go` exercises userspace WireGuard with a channel
TUN and injected firewall/socket-mark boundaries, including route-stage failure
and grant withdrawal. This is not privileged kernel evidence. Native host adapter
wiring, complete underlay exceptions, verified clear/release, startup restoration,
observations, Windows/Darwin protection and platform acceptance remain open.

The engine now has an explicit post-clear protection release step. It requires
the owned guard and a stopped runtime with no retained route-cleanup object;
ordinary Down/Close still retain protection. A lost release result retains engine
ownership and attempts containment again with a bounded cancellation-independent
firewall call, so the adapter can retry instead of claiming confirmed cleanup.
`TestExitGuardReleaseRequiresCleanupAndRecoversUncertainRelease` covers failed
route removal, retry, cancellation, foreign guards and a lost kernel reply.
The guarded-engine test also rejects release while a real userspace TUN is live.
These use injected firewall/router boundaries. The native adapter must invoke
release only after durable clear under its shared effect lock, and must recover
that ordering across restart; host wiring and privileged OS evidence remain open.

Exit-worker recovery now retains its lifetime after missing or partial apply
observations when the durable context remains authorized. Previously completion
rejected that evidence correctly, but propagated STALE_STATE and stopped the
worker, requiring a restart to retry. The same operation and retry deadline now
remain active without committing selection; context loss still enters containment.
`TestExitWorkerResumesAndRetriesWithoutRequestReplay` reopens a pending clear from
disk and covers native errors, absent observations and incomplete IPv6 results,
retaining the previous selection until a later complete result. This is worker
recovery evidence with an injected adapter, not native clear qualification.

Containment also keeps the worker alive while its adapter cannot supply a bound,
complete proof. Rejected operation/profile/node/network identities, either
unconfirmed IP family and unconfirmed route cleanup retain the journal and return
UNAVAILABLE for retry. `TestExitContainmentRequiresBoundProofAndRecovers` covers
each proof field; `TestExitWorkerRetriesUnconfirmedContainmentAfterRestart`
resumes the containing phase from disk, retries without Apply or another RPC and
retains the original failure cause until complete evidence arrives. This does
not prove kernel containment or complete the native adapter.

Linux containment now permits only host Router Solicitation and Neighbor
Solicitation/Advertisement outside its TUN, with hop limit 255 and ICMP code 0.
Without these messages, the marked IPv6 UDP underlay cannot resolve or maintain
its next-hop neighbor. The constraints follow [RFC 4861](https://www.rfc-editor.org/rfc/rfc4861.html)
and use the [nftables ICMPv6 selectors](https://netfilter.org/projects/nftables/manpage.html).
The atomic-batch unit checks this exemption in contained/open/restarted states
while rejecting generic ICMP echo, router advertisements and redirects. Kernel
validation, multicast-listener/DHCP maintenance and authorized control/relay/DNS
underlay integration remain open; this does not enable native exit readiness.

The engine relay bridge now uses the application plan's firewall mark for TCP
and its Go resolver's UDP/TCP sockets. The native Linux setter is shared with
MagicBind; setter errors abort dialing, and non-Linux platforms reject a nonzero
mark. A mark change replaces the relay connection even without a new map or UDP
endpoint, while ordinary zero-mark connections keep their default resolver.
The nft guard permits TCP only under the same reserved mark as UDP.
`TestMarkedUnderlayCoversTransportAndResolverWithoutFallback` covers successful
socket setup and mark failures on all three paths;
`TestWireGuardRelayReplacesConnectionsWhenUnderlayMarkChanges` authenticates to
the reference relay before and after a mark change with an injected socket setter.
Native SO_MARK/routing/firewall evidence, a pre-exit DNS source (including system
stub forwarding), complete control-plane transport marking and startup restoration remain
open. These changes do not yet advertise an operational native exit adapter.

The 2026-09-21 DNS source audit confirms that the resolver in
`underlay_dialer.go` still receives the current system resolver address. A marked
socket to a local stub does not mark the stub's forwarded query;
`dnsproxy.go` forwards with ordinary dialers. Marking the whole proxy would
incorrectly grant user DNS the privileged underlay path. Linux needs a bound
snapshot of non-Client per-link resolver endpoints and domain/default routing,
captured before DNS changes, then direct marked resolution only for authorized
control/relay names. Aggregate resolv.conf loses split-domain/link information.
Recovery must revalidate the snapshot against current link/DNS state and cancel
old requests on a source change. The pinned exit grant has no bootstrap DNS
source; signed network DNS is overlay authority, not underlay evidence.
Native OS discovery is client-owned; a product-managed bootstrap/DoH/DoT source
would additionally require producer transport/security semantics. The relevant
OS contract is [systemd resolve1](https://www.freedesktop.org/software/systemd/man/247/org.freedesktop.resolve1.html).

Startup guard creation now receives `WireGuardRouteTable` and derives its mark
using the same Linux router selection as route application. This fixes recovery
with a custom numeric table previously replaced by the default 51820 mark.
`TestExitStartupRestoresContainmentBeforeControlAccess` covers forwarding the
custom table and guard reuse; `TestExitStartupGuardUsesRouterTableMark` checks
Linux guard construction without executing OS commands. Native recovery evidence
remains open.

The owned control context also binds `WireGuardRouteTable`. Recovery with a
different table re-establishes containment but cannot replace the original
binding or obtain marked control access. `TestExitRecoveryRetainsTableBindingAcrossRetry`
covers this after both successful and failed containment, followed by recovery
with the original table. This is an in-process ownership check; a durable OS
ownership ledger across external configuration changes remains open.

Pending exit operations now persist their admitted route table. Worker preflight
and completion reject a different current table; a dispatched operation retains
its journal for containment. Startup uses the journal's original table to restore
closed protection and denies control authority when configuration differs.
`TestRPCExitExecutorDurabilityAndRevalidation` checks disk persistence, changes
before dispatch and during apply, including retry after reopening the store.
`TestFirstExitRecoveryRequiresBoundRunningJournal` checks original-table startup
containment with changed configuration. Completed selections also retain the
admitted route table after the journal is cleared. Startup restores that table
and rejects a changed current configuration; route projection and new exit
admission enforce the same binding. The executor test reads the completed
selection back from disk, and the startup test exercises a fresh engine without
an operation journal. This persists the table association, not complete native
ownership: interface identity, policy-rule coexistence and the clear/release
crash window remain open.

Linux router configuration now rejects an error flushing either family's old
global interface addresses before adding new addresses, routes or DNS. Its
cleanup journal retains failed removals and blocks reapplication until cleanup
succeeds. `TestLinuxRouterRejectsUnclearedAddressesBeforeNewConfiguration` checks
both families, repeated failure and successful cleanup before reapplication with
an injected command runner. Linux execution remains part of platform CI evidence.

Linux default-route application also requires observed removal of old dedicated
and main-table suppression rules before installing new rules. An ambiguous
delete is acceptable only if the subsequent dump proves absence; retained rules
or observation errors abort application. The injected Linux test
`TestLinuxExitRouteRequiresObservedRuleRemovalBeforeAddingRules` covers both
families and both rule stages. Shared suppression-rule ownership remains open.

The resource/preferences worker also checks cancellation inside the durable
commit transaction, after policy revalidation. Cancellation there leaves the
previous values and resumable operation intact instead of publishing success or
misclassifying shutdown as an apply failure. The `commit_cancel` case in
`TestResourceWorkerRestartsApplyOrContainment` cancels during commit-time policy
validation, reopens the store and finishes the original operation with replay.

Runtime configuration now checks cancellation before starting and at the logical
state and filter commit boundaries. A stage returning nil after context
cancellation cannot commit a relaxed resource policy. The resource engine test
cancels an enable during path setup, verifies the old denial remains and no
enforcement observation is exposed, then verifies a fresh attempt can enable it.
This uses the real engine with injected TUN/router, not native OS effects.

Resource service compilation now has packet-level regression coverage for
independent TCP and UDP preferences sharing the same numeric port. The signed-map
test `TestResourceCompilerServiceDenialPreservesTransportPortAndHostScope`
compiles the preference, commits the filter and checks requests and replies:
only the disabled transport/port on the advertised host is denied. Other ports,
the other transport and unrelated hosts remain outside that resource denial.
This verifies filter scope, not route availability or end-to-end reachability.

The native exit command runner now caps combined stdout/stderr during capture
at 1 MiB, cancels on overflow and returns no truncated observation. A bounded
pipe wait also prevents inherited output pipes from delaying completion without
limit. `TestExitCommandOutputCancelsOnOverflowWithoutAcceptingTruncation` checks
single-chunk and incremental overflow, cancellation and retained-data bounds;
process termination behavior still needs platform qualification.

Route observation distinguishes an omitted main-table field from an explicitly
null or empty table value. The latter is invalid and cannot prove cleanup.
`TestExitRouteObservationDistinguishesOmittedMainTable` checks implicit and
explicit main-table routes against both the main and dedicated table; malformed
table values are covered by the ambiguous-output rejection test.

Linux exit clear now checks the actual IPv4 and IPv6 route dumps before releasing
its owned firewall guard. `exit_route_observation.go` queries all tables with
`ip -j -N` so a missing dedicated table is an empty observation, while command
errors, malformed output and cancellation cannot prove cleanup. Any remaining
route in the reserved table retains protection. The numeric string table format
follows [iproute2 table rendering](https://github.com/iproute2/iproute2/blob/main/lib/rt_names.c).
`TestExitRouteCleanupRequiresBothFamilyObservations` covers both families and
observation failures; `TestExitGuardReleaseRequiresCleanupAndRecoversUncertainRelease`
also verifies that failed observation and remaining routes prevent guard release.
This proves the injected observation/release boundary only: native command
qualification and the complete exit worker remain open.

Guard release also re-observes policy rules independently of the in-memory
router cleanup journal. For both families, any rule referencing the dedicated
table or suppressing the main table's default route prevents release. Read-only
table-filtered dumps reuse the bounded policy-rule parser; they never delete
potentially unrelated rules. `TestExitPolicyCleanupRequiresEveryRuleObservation`
exercises each of the four queries with success, remaining rules, command errors,
malformed output and cancellation. The engine release test checks retained guard
ownership when rules remain. Native ownership/coexistence qualification is open,
including the shared main-table suppression rule used by the current router.

Application connector reporting now uses an origin-scoped control HTTP client
with the pinned clientapi TLS policy and the engine's firewall mark. The transport
rejects unauthorized origins and Host overrides before dialing, closes rejected
request bodies, disables implicit environment proxies when marked and retains
the producer's no-redirect policy. Connector reports require HTTPS origins.
`TestControlUnderlayRejectsUnauthorizedRequestsBeforeDial` covers the authority
boundary; `TestControlUnderlayUsesMarkedTLSAndDoesNotFollowRedirects` exercises
HTTPS requests with an injected marker; `TestControlUnderlayMarkFailureHasNoFallback`
covers fail-closed dialing. Agent map/session transport wiring,
pre-exit DNS and native kernel evidence remain open. This is a connector transport
increment, not native exit readiness.

Marked control clients also clear inherited TLS-specific dial hooks: these hooks
otherwise bypass the marked `DialContext`. The HTTPS/redirect test installs both
TLS hook variants on the default transport and verifies neither is called while
the injected socket marker is used for the successful TLS request.

Flow policy/report RPCs also use the scoped marked HTTPS transport. A mark change
cancels and joins old HTTP response bodies and closes idle connections while
retaining the flow worker, its consent scope and pending queue. Identity changes
still stop and purge the old worker. `TestControlTransportRotationCancelsAndJoinsOldResponse`
checks cancellation and body ownership across replacement;
`TestTUNFlowProducerRetriesThroughTLSProtobuf` now replaces the transport between
an unacknowledged report and its retry and checks the same durable window is sent.
These unit observations do not establish native SO_MARK or route evidence.

`TestFlowConfigurationRestartsWorkerAfterStorageFailure` exercises the engine's
actual flow-worker lifecycle using an unreadable, non-discardable queue path.
Two identical configurations each attempt storage recovery, retain the same
consent scope and close the failed worker's HTTP transport before signalling
completion. This proves a completed worker is not reused as if it were running;
it does not claim that an unrepaired storage failure has been recovered.

Application connector context replacement and engine shutdown now cancel and
join the discovery worker before releasing its ownership. Old HTTP idle
connections close before worker completion is signalled. Cancellation during
DNS does not emit an empty-address withdrawal for the old identity.
`TestApplicationContextChangeJoinsDiscoveryAndReport` holds both DNS and HTTP
stages after cancellation and verifies replacement waits for completion; the
DNS case also proves no report is attempted. Server-side processing already
accepted before cancellation remains outside this local completion guarantee.

The main agent map iteration now propagates its runtime context through signing
trust, heartbeat and stream HTTP requests using the existing workflow transport.
Cancellation remains effective while reading response bodies and is checked
before saving the received projection; each iteration closes its idle HTTP
connections. `TestAgentIterationCancellationStopsControlResponseBody` cancels
blocked trust and map-stream bodies through `runAgentIteration`, verifies the
request ends without waiting for its 30-second timeout and checks the durable
configuration is unchanged. Main-agent underlay marking and startup restoration
are addressed separately from this runtime cancellation guarantee.

Map iterations with an engine now obtain their HTTP client from
`WireGuardEngine.ControlPlaneHTTPClient`. An owned exit guard supplies its mark
even after route cleanup; the saved successful engine context must match node,
network, credential and control origins. A saved exit without a restored guard,
or a guard without a bound successful context, rejects control access instead
of opening an unmarked socket. `TestEngineControlUnderlayRequiresOwnedExitIdentity`
covers these construction boundaries without native socket operations. The caller
is checked by `TestAgentDoesNotBypassRefusedEngineControlTransport`: an engine
refusal is returned before trust or map requests rather than bypassed. The caller
still must serialize runtime effects and cancel requests before identity changes.
Initial protected startup/recovery, credential renewal handover, session clients,
pre-exit DNS and native kernel evidence remain open.

Separate endpoint publication (manual and discovery paths) now uses the engine's
control client and runtime cancellation, closes idle connections and checks
cancellation before persisting the returned map. The control-body cancellation
test also blocks an endpoint response and verifies the saved configuration is
unchanged; the engine-refusal test checks endpoint publication cannot bypass a
refused protected transport. Remote endpoint commits that precede cancellation
still require reconciliation; cancellation is not evidence of remote rollback.

The private native-adapter primitive `restoreExitUnderlay` can now restore closed
containment for a durable selection before fetching a fresh map. It requires a
stopped engine, matching selection/node/network and valid control origins. The
pending identity survives failed containment so a retry cannot substitute another
credential or origin. Control access becomes available only after containment
and the final cancellation check; no routes or applied selection are published.
`TestExitUnderlayRestoreBindsOnlyAfterContainment` covers failure, cancellation,
retry and identity replacement. Host startup now calls this primitive through
`RestoreExitProtection`; the native apply executor remains unwired.

The same recovery primitive accepts a first selection that reached RUNNING but
has not committed `ExitSelection`. Its operation record must match the journal,
owner, active profile, primary origin, node, network and previous intent. Pending,
terminal or missing operations cannot authorize control access. Any exit journal
without an owned guard also blocks ordinary engine HTTP-client construction.
Ordinary engine `Configure` rejects that journal too, including cached bootstrap
before the first selection has committed; only protected reconciliation may apply it.
`TestFirstExitRecoveryRequiresBoundRunningJournal` checks these boundaries with
no cached map and verifies the requested selection remains unapplied. Crash
recovery of guard ownership across all clear/identity transitions remains open.

Startup policy refresh now runs after engine construction and obtains its HTTP
client from the engine. The shared fetch used by startup retry and resume follows
the same rule and closes idle connections. `TestStartupAndResumePolicyDoNotBypassEngineRefusal`
checks all three paths return a refused transport without committing policy or
changing durable state. Valid cached policy still needs no HTTP request. This
closes the pre-engine policy-fetch bypass.

Agent startup now invokes `RestoreExitProtection` before policy refresh and intent
initialization. Linux constructs the interface-scoped nft guard with the router's
reserved mark and restores closed containment; unsupported platforms fail before
control I/O. An undispatched PENDING operation with no previous selection waits
for the executor without creating a guard, while ordinary apply/control remains
blocked by its journal. `TestExitStartupRestoresContainmentBeforeControlAccess`
checks guard reuse and control access with an injected command runner;
`TestExitStartupSkipsOnlyUndispatchedJournal` checks pending versus uncertain
effects. `TestAgentStartupRestoresGuardBeforePolicyAndIntent` verifies host ordering,
failure and cancellation. Native nft/SO_MARK validation, pre-exit DNS, apply/clear
executor wiring and full startup/boot crash evidence remain incomplete.

Recovery now restores containment even when journal/identity validation denies
control authority. It retains the guard and any previously reserved identity,
clears control access and returns the validation error after the closed firewall
is installed. The journal and underlay recovery tests verify stale owner/profile/
origin/intent records cannot skip containment or acquire control access, and a
replacement credential cannot overwrite the reserved identity. An active engine
or a different guard owner is still rejected before changing OS state.

The shared workflow transport closes request bodies rejected because the runtime
was already cancelled, without reading the payload or recording a remote attempt.
`TestEnrollmentCancelledBeforeRequestClosesBodyWithoutAttempt` verifies body
ownership and that neither the underlying transport nor the outcome callback runs.

CI execution policy: branch pushes run only `go test -short ./...` in the Test
workflow, separately in the root and nested `clientipc` Go modules. The root
package pattern does not traverse nested modules. Full verification,
installation and contract matrices run on PR or
explicit manual dispatch. CodeQL and Protobuf contract checks no longer run on
push. Tag-triggered publication workflows are unchanged; publication still
requires exact-source full manual evidence, not a green short-test push. No PR
or manual integration run is created merely to bypass the implementation phase.

The generated handler interface is in
`clientipc/v0/clientipcconnect/service.connect.go`. `ClientRPCService` embeds its
unimplemented handler, but SelectExitNode and ClearExitNode now have explicit
overrides in `internal/client/service_rpc_exit_public.go`; GetExitNode is in
`service_rpc_exit_status.go`. Their missing production executor must not be
described as missing handlers or counted as an operational exit implementation.
The only callers of `startExitWorker` remain unit tests in
`service_rpc_exit_worker_test.go`. Fresh Select/Clear requests therefore fail
readiness admission in the production service, while durable replay remains
available. No agent call starts a native exit worker.

| Requirement area | Implemented client boundary | Missing operational implementation and evidence |
| --- | --- | --- |
| US-05 exit | Public Get/Select/Clear handlers, readiness gate, durable journal, revalidation/containment worker and injected executor tests | A production native adapter and agent worker startup; observed per-family route/firewall results; clear/release crash recovery; pre-exit DNS and complete ownership across restart; supported OS and LAN policy qualification |

Additional partial implementations must not be mistaken for complete domains:

2026-09-21 durable recovery increment: exit dispatch records the original OS
protection scope independently of operation retention. Clear first checkpoints
confirmed route cleanup with both families still blocked, then invokes a separate
repeatable release callback. The journal and protection marker survive ambiguous
release/cancellation; terminal containment retains the marker. Startup restores
blocking from the original interface/table without deriving control authority
from a cleanup marker. The original owner can explicitly clear retained artifacts
through the original profile, including when inactive; remove/forget is blocked
until release, and a replacement active selection cannot be cleared this way.
These state-machine callbacks still require the production native adapter and
verified firewall/route observations before worker readiness can be advertised.

Resource map transitions now reconcile saved local choices atomically with the
accepted map. Canonical choices missing from the new authenticated catalog move
to durable retired storage and return only under the current policy when their
ID reappears. This explicitly repairs already-orphaned choices without making
the runtime compiler ignore stale active entries. New-map signature, recipient,
expiry and revision checks remain required; an expired/old-key/missing previous
cache does not itself confer or deny new network authority. Active plus retired
choices are bounded together; full capacity rejects new local choices, not map
refreshes. Pending operations keep their original map binding and follow existing
stale-context containment. Retirement is local intent, not positive availability.

| Area | Source evidence | Remaining work |
| --- | --- | --- |
| Resources | Public `SetResourceEnabled` uses the durable worker; catalog projects policy, overlap and confirmed TUN denials, with observation events | Complete positive route/path/application observations, stale-choice reconciliation, failure/restart audit and OS effect qualification |
| Preferences/policy | Public Set/Reset supports inbound/DNS/routes and runtime_start/user_logoff/suspend/resume with durable worker state; UI_QUIT is resolved with signed policy. The network preference journal captures all lifecycle fields and resource choices | Complete per-method assertion audit, concurrency/recovery and actual OS effect evidence; implemented fields and policy resolution do not prove native lifecycle delivery |
| Diagnostics | `service_rpc_diagnostics.go` projects bounded OS route samples; missing samples remain explicitly unavailable and supplied samples remain incomplete | Qualify platform command execution later; extend route coverage beyond host-address sampling without substituting desired configuration for observed OS state |
| Updates | `service_rpc_update.go` reports `update_source_not_configured`; `service_rpc_update_test.go` exists | Bind an approved distribution source and verify its projection; unavailable is not up-to-date, and unavailable-path tests do not prove update discovery |

Next implementation work must close these operational paths rather than use
the component tests above as acceptance. In particular, the Linux exit guard,
route/rule absence checks and persisted table binding do not yet provide the
adapter's positive Apply observation, complete Clear transaction or startup
worker. Update discovery still has no configured verified source: the current
handler returns SOURCE_UNAVAILABLE unconditionally and cannot attest an installed
UI/core pair. Resource enforcement confirms the committed denial filter only;
positive path/route/application availability remains separate work.

The guarded Linux engine now queries the selected table's exact default route
for each requested address family before opening tunnel egress. Missing routes,
another interface, link-down flags, indirect/multipath routes, command failures
and cancellation retain containment. Injected unit observations cover the query
scope and guarded apply failure. Each requested family also requires the exact
unmarked-traffic table selector and main-table suppression rule, with suppression
before the exit-table lookup. Changed selectors, duplicates and reversed/equal
priorities fail closed. An unsuppressed main-table rule at or before the exit
lookup also prevents opening: otherwise its default route could preempt the exit
lookup despite a correctly ordered suppression rule. Injected engine coverage
asserts containment for this conflict; parser cases reject changed/duplicate
suppression selectors. These observations do not prove interference freedom
from other tables' rules, firewall readback or end-to-end path health, and the
production exit worker remains unwired. Single-family selection additionally
observes that the disabled family's dedicated table, table references and main
suppression rule are absent before opening. Both family directions have injected
absence/error/cancellation coverage; the engine test retains containment for a
leftover disabled-family route. This does not prove native packet blocking for
that family, which still requires firewall and platform qualification.

UI-quit managed-policy increment: `service_rpc_lifecycle_policy.go` resolves
KEEP_INTENT/DISCONNECT from the authenticated profile-recipient map. Reads,
Set/Reset and UI_QUIT execution share that resolution. An unlocked managed
value supplies the reset baseline; a locked value constrains an existing user
override and rejects a conflicting new override. Provenance and the responsible
administrator are projected. Invalid/expired/context-mismatched maps do not
silently supply a default; accepted request replay precedes current-policy
validation. Managed CONNECT still requires lifecycle connect execution and is
explicitly unsupported. Other lifecycle keys, network preferences and full OS
lifecycle implementation remain open. This increment does not close US-10/12.

Active map/policy catalog invalidation now runs both on accepted runtime status
observations and independently in the RPC host's one-second catalog clock.
Source changes and map/exit/application expiry invalidate owner-visible peers,
networks, exit, preferences, managed settings and resources even when Status is
unchanged. This does not implement resource effects or OS lifecycle events;
inactive-profile and complete cross-domain invalidation audit remains open.

Network acceptance increment: `network_preferences.go` persists optional DNS and
route intent in the profile Config and resolves signed managed baselines/locks.
The shared userspace router builder consumes this resolution: disabled DNS has
no proxy, resolver, search or split-domain configuration; disabled learned routes
retain ordinary host routes but omit subnets, explicitly managed single-IP
subnets and application routes. Signed sources are recipient-bound and checked
before preference resolution. This is used by the existing OS router adapters,
but the unit evidence is derived router configuration, not observed OS effects.
OS effect qualification remains missing; inbound public admission/projection is described below.
The worker increment below provides candidate apply and durable containment.
Checked static export now receives the full Config and uses
the same authenticated DNS/routes resolver; it cannot reintroduce a disabled
resource route or DNS setting. Static output is still not a live policy/expiry
enforcer. No additional
preference capability is advertised by this increment.

Durable admission preparation: `service_rpc_network_preferences.go` records an
atomic DNS/routes patch (optionally including UI-quit) separately from active
configuration, bound to owner, profile, node, network and signed map payload.
`TestNetworkPreferenceAdmissionIsDurableAndDoesNotApply` covers restart and
same-request replay ahead of CAS/conflict checks;
`TestNetworkPreferenceAdmissionRejectsWholePatch` covers authorization, signed
source tampering, revision mismatch, managed lock and unsupported/empty patches;
`TestNetworkPreferenceResetPreservesUnselectedOverrides` covers selective reset.
Public Set/Reset now routes patches containing DNS/routes through the ready
profile worker; UI-quit-only operations retain their immediate local semantics.
`service_rpc_network_preferences_worker.go` now runs in the profile worker's
startup scan under the shared agent driver lock. It authenticates the candidate
before apply and again at commit, rejects changed profile/owner/map/intent,
and commits network and UI-quit overrides together. Failure enters a durable
containment stage with disconnected intent before Down; a Down error is
resumable after restart and never retries the failed candidate. This rollback
retains prior preference values and disconnects; it does not restore a live
connection. `TestNetworkPreferenceWorkerApplyAndContainment` covers successful
candidate delivery, offline application, failure, concurrent Disconnect/map
tampering and shutdown. `TestNetworkPreferenceContainmentRecoversAfterDownFailure`
covers the persistent recovery stage. Driver callback success is unit evidence,
not OS observation. Containment now persists the original bounded failure
code/reason before Down: concurrent Disconnect yields CANCELLED and retains its
accepted intent reason; context drift yields STALE_STATE, invalid signed source
yields UNAVAILABLE, and driver errors yield APPLY_FAILED. Restart tests assert
that a later Down failure does not replace the original apply cause.
Resources/preferences now share the network-selection Stop confirmation rule:
nil error alone is insufficient when continuity is PRESERVED, UNSPECIFIED or
an unknown enum. Such results cannot commit offline preferences or finish
containment. UNKNOWN remains an accepted historical-continuity result from a
driver that confirms successful Down. `TestPreferenceAndResourceCleanupRequiresConfirmedStop`
covers both operation families and connected/disconnected candidates, durable
RUNNING containment, unchanged committed settings, disk recovery and exactly
one confirmed cleanup without repeating Apply. Persistent native cleanup errors
and automatic retry availability still need their own runtime qualification.
The profile worker now retains service availability for typed UNAVAILABLE,
DEADLINE_EXCEEDED or LIMIT_EXCEEDED returned during durable resource/preference
containment. It retries after five seconds or a wake, beginning again with
Disconnect reconciliation. It never retries an uncheckpointed candidate on this
path; unknown/permanent errors still terminate the worker for host recovery.
`TestPreferenceWorkerRetriesTemporaryCleanupWithoutReplay` exercises the actual
worker timer through a failed Apply, temporary Down and confirmed cleanup with
one Start, two Stops and the original APPLY_FAILED outcome. Classification units
exclude non-containment, raw, cancellation, permission and stale-state errors.
This is worker/unit evidence; native error mapping and platform qualification
remain open.
`service_rpc_network_preferences_public.go` projects authenticated committed
policy resolution separately from pending requested overrides, including reset
absence and managed provenance/locks. GetPreferences and ListManagedSettings
use that projection, and preference operations invalidate resources as well.
Network preference/reset and resource admission now perform authorization,
mutation validation and durable replay before rejecting an absent/stopped worker.
Only new work requires executor readiness. `TestPreferenceResourceReplayWithoutWorker`
checks pending and terminal replay after disk reopen for all three methods,
absent and cancelled workers, unauthenticated/foreign callers, changed payload
under the same request ID and new-work rejection without persistent mutation.
The rejecting admission callback cannot execute a candidate or create a plan.
PREFERENCES and RESOURCES capabilities now follow the live profile worker that
reconciles these operations. Native Start sends candidate configuration through
WireGuard.Configure, including network acceptance and resource packet filters;
this readiness is not a reachability or full platform-support assertion.
Existing capability tests now include both families in Bootstrap/opening snapshot,
worker cancellation, independent enrollment lifetime, replacement-worker safety
and host shutdown. Per-setting/resource policy restrictions still apply, and
full OS effects, recovery and BA/SA acceptance remain separately open.
Read-restriction audit: ListResources already preserves pending/conflict and
managed-policy precedence over worker absence. GetPreferences now applies
`preference_worker_unavailable` only to otherwise AVAILABLE boolean mutations;
it no longer hides inactive-profile or pending-patch reasons.
`TestPreferenceWorkerAbsencePreservesContextRestriction` covers idle, pending
and inactive reads for DNS, routes and inbound settings with no executor.
`TestNetworkPreferencePublicAdmissionWakesWorkerAndSeparatesPending` exercises
the readiness/acceptance bridge, worker wakeup and pending-to-committed reads;
`TestNetworkPreferenceProjectionManagedLockAndReset` covers lock precedence
and reset baseline restoration. This is not authenticated transport acceptance
or observed OS effects, both of which still need evidence.

Preference concurrency/readiness correction: UI-quit-only Set/Reset cannot
mutate a pending network patch; the generic journal still resolves previously
accepted retries before this conflict check. Pending UI-quit projection disables
mutation, and unlocked DNS/routes mutation availability follows the serving
profile worker, including cancellation. `TestUIQuitMutationCannotBypassPendingNetworkPatch`
checks rejection without persistent changes and accepted replay;
`TestNetworkPreferenceMutationProjectionTracksWorkerReadiness` checks missing,
live and cancelled worker states without altering preference values.

Inbound filter preparation: `inbound_filter.go` implements a bounded volatile
outbound-flow table for restricting new inbound traffic while preserving matched
TCP/UDP/ICMP replies. `TestInboundFilterTCPHandshakeExpiryAndPolicyChange` checks
unsolicited TCP denial, handshake ordering, expiry and policy-change clearing;
`TestInboundFilterUDPBindingBoundsAndReplyLifetime` checks port binding, fixed
reply windows, capacity/expiry recovery and malformed packets. Full IPv6
transport coverage and runtime/public integration still need evidence; allow_inbound
remains incomplete. The optional filter is now the final conjunction in TUN
Read/Write, so traffic denied by earlier ACL/application/sharing filters cannot
seed its flow table. `TestInboundTUNFilteringPreservesBatchesAndCannotSeedDeniedFlows`
checks the actual wrapper with batch offsets, unsolicited reply denial, matched
replies and an enabled preference that cannot bypass peer ACL.
`TestInboundFilterEchoFamiliesAndExtensionRejection` checks IPv4/IPv6 echo
identity/sequence, reply expiry and fragment rejection. Engine construction now
installs this filter on the real userspace TUN. The shared producer-policy
resolver includes optional AllowInbound, with locks taking precedence over local
overrides. Configure tightens before apply and only relaxes after success;
failure retains restriction. Identity changes and Down clear volatile flows.
Static export rejects a resolved inbound restriction rather than dropping it.
`TestInboundPolicyResolutionAndExportRestriction` covers signed baseline/reset,
locks and checked-export rejection. `TestInboundEngineAppliesPolicyAndClearsIdentityFlows`
exercises engine Configure with a test TUN/router, restriction and relaxation,
and filter identity reset. Public Set/Reset now includes AllowInbound in the
same durable atomic plan as DNS/routes and UI_QUIT, and GetPreferences plus
ListManagedSettings expose its signed baseline, override and lock. The existing
CLI `allow-inbound` key routes through these native methods.
`TestInboundPreferenceAtomicPatchRestartAndSelectiveReset` checks pending versus
committed values, disk restart/replay, atomic mixed commit and inbound-only reset.
`TestInboundPolicyLockRejectsWholeMixedPreferencePatch` checks all-or-nothing
lock rejection and device-policy provenance. Authenticated transport mutation
coverage, complete IPv6 coverage and OS qualification remain open; these units
are not system acceptance.

Existing test filenames above identify starting points for review, not assertions
that all listed scenarios are already covered.

Resource mutation identity preparation: `resource_identity.go` shares canonical
ID encoding with ListResources and resolves IDs against the authenticated current
recipient map. It rejects noncanonical encoding/ports, undisclosed targets,
default routes, ordinary single-IP host addresses masquerading as subnets and
applications for which the recipient is only a connector. Explicit managed
single-IP subnets remain valid. `TestResourceIdentityMatchesCatalogAndRejectsForgedTargets`
round-trips catalog IDs and tests forgery/tampering;
`TestResourceIdentityManagedSingleIPAndApplicationSource` covers subnet identity
and application role. This is preparatory validation: SetResourceEnabled,
durable operation intent/effects and overlap handling remain unimplemented.
`resource_preferences.go` now resolves a per-profile optional local choice over
the authenticated producer baseline/lock without claiming reachability. Service
ports share the producer service policy while local overrides remain per catalog
ID. `TestResourcePreferenceServicePolicyCoversPortsAndPreservesLocalChoice`
checks both ports, locked/unlocked precedence, disk persistence, absence/reset
and cloned reads. `TestResourcePreferenceFalsePresenceAndAuthenticatedSource`
checks explicit false across disk reopen and signature rejection. Public
SetResourceEnabled and complete effect qualification are still required.

Resource packet enforcement preparation: `resource_filter.go` is composed into
the optional TUN filter chain before inbound response tracking. It applies
prefix/port denials in both directions with deny precedence for overlaps,
retains old denials while tightening, and relaxes only on commit. Map expiry and
withdrawal close traffic. `TestResourceFilterDeniesOverlapsAndTransitionsInBothDirections`
checks port/protocol separation, overlap and staged changes, expiry and withdraw;
`TestResourceFilterRuleValidationAndOwnership` checks rule shape,
defensive copying and malformed packets. Public dispatch, the durable worker
and engine integration are described below; complete effects remain unqualified.
`resource_rules.go` now authenticates once and compiles local/managed choices
into bounded, deduplicated prefix/port denials for host, subnet, service and
source-role application resources. Service host matching includes the current
public key; application port targets remain port-specific. Default routes are
excluded. Stale local choices cause a compilation error and still need a
reconciliation policy. `TestResourceCompilerSubnetOverlapAndApplicationPort`
checks overlap deny precedence, application port scope and stale choice failure;
`TestResourceCompilerManagedServiceAndSignature` checks locked service ports and
tampered-map rejection. Engine integration now compiles from the supplied signed
map, stages denials before Configure, commits after success, and withdraws on
apply failure/Down. The real TUN retains the same filter object across updates;
resource-only changes are reported even without route changes. Static export
rejects compiled resource restrictions. `TestResourceEngineAppliesChangesAndRejectsStaticBypass`
exercises default/disable/enable, Changed reporting, map expiry, shutdown and
checked-export rejection with a test TUN/router. Public SetResourceEnabled,
durable resource operations and OS effect qualification remain open.

## Client-owned scenario map

The separate [headless per-IT trace](client-headless-requirement-map.md) retains
all 33 SA integration specifications as client implementation/unit obligations
and explicit external observations. It is an open audit, not integration results.

This maps all fourteen system-scenario families to the client boundary. It is
not yet the per-requirement BA/SA completion matrix. All rows remain open until
each applicable requirement and negative outcome has been individually audited.
Paths in the table are under `internal/client/` unless qualified otherwise.

| Scenario | Runtime / contract implementation | Unit starting point and verified limit | Remaining client work / external dependency |
| --- | --- | --- | --- |
| US-01 bootstrap | `service_rpc_host.go`, `service_rpc_capabilities.go`; protected transport in `clientipc/local` | `service_rpc_host_capabilities_test.go`; separate `go test -short ./...` in `clientipc` passed locally on Windows on 2026-09-13 and is included in push CI | Audit startup/identity/capability failure variants; local Windows results do not qualify Unix transport or all bootstrap requirements |
| US-02 enrollment | `service_rpc_enrollment_worker.go`, `service_rpc_enrollment_executor.go`, credential trust included in `copyEnrollmentFields` | `TestRPCEnrollmentWorkerRecoveryAndShutdown`, `TestEnrollmentCheckpointPersistsCredentialVerificationAuthority`, `TestEnrollmentCheckpointRejectsConcurrentCredentialTrustChange`, `TestNativeEnrollmentThroughRPCHostRetainsCredentialAuthority` | Credential trust survives RPC checkpoint/disk reopen and participates in checkpoint CAS. The native host unit now exercises public create/select/enroll, the real provider against a certificate-verified synthetic HTTPS server, worker completion, credential status and request replay without repeated registration or tunnel configuration. Direct provider tests alone had bypassed this callback. Remaining approval/denial/expiry/cancellation cases and deployed backend/platform evidence remain open. |
| US-03 connection | `service_rpc_connect.go`, `service_rpc_disconnect.go` | `TestRPCConnectDurabilityAndFailure`, `TestRPCDisconnectPreemptsApplyOnlyAfterAcceptance` | Audit remaining races and recovery boundaries; unit driver results are not OS traffic evidence |
| US-04 networks/peers | Network/peer handlers; isolated target preparation and checkpoints; native registration/approval refresh; `ReconcileNetworkSelectionRegistration` validates registration readiness without activating target | Preparation/CAS/checkpoint and native-provider units; `TestNetworkRegistrationCoordinatorResumesApprovalWithoutActivation`, `TestNetworkRegistrationCoordinatorRejectsFalseSuccess`, `TestNetworkSelectionReadinessRejectsMismatchedAuthority`; peer suites | Public SelectNetwork now admits different-network operations through the live native worker, validates installation keys and control origin, and retains durable same-current no-op/replay. Worker readiness drives capability and catalog restrictions; accepted replay remains readable after worker shutdown. TestNetworkSelectionAdmissionRejectsInvalidInstallationAndOrigin verifies rejection without journal/source changes for absent or malformed keys, missing fingerprint and missing/foreign control origin. Internal coordinator resumes pending approval from disk, reuses valid ready checkpoints and rejects expired credential/map, no-save success, tampered map and concurrent source change. Readiness binds account/network/node, signatures, revisions/hash and installation keys. Internal activation now journals DownStarted before driver.Stop, excludes ordinary agent apply, revalidates source and signed target after confirmed teardown, then atomically replaces active identity/map and profile configuration. TestNetworkActivationStopsSourceBeforeAtomicTargetAdoption, TestNetworkActivationRetainsBarrierOnUncertainOrStaleStop and TestNetworkActivationResumesUncertainStopFromDisk cover ordering, context/expiry races and interrupted teardown. Activation remains RUNNING; internal ReconcileNetworkSelectionApply now journals apply/cleanup, rechecks target authority before terminal success, resumes uncertain apply through Stop and retains the barrier on cleanup failure. TestNetworkApplyCompletesOnlyCurrentTargetAndReplays, TestNetworkApplyFailureResumesCleanupWithoutReapply, TestNetworkApplyContainsContextChangeDespiteProviderSuccess and TestNetworkApplyRecoversUncertainAttemptByStoppingFirst cover injected driver outcomes, including accepted RPC Disconnect cancelling in-flight apply even when the driver returns success. Native agentRPCCleanupNetworkTarget resolves a saved uncertain registration and revokes only its node without session logout; TestNetworkTargetCleanupRevokesOnlyItsNode covers target binding, exact signed request replay, response-checkpoint failure and remote revoke failure. Backend idempotent replay/revocation qualification remains external evidence. ReconcileNetworkSelectionAbort now persists cancellation/failure, permits isolated recovery checkpoints after source changes, rejects source/active/profile node revocation, and checkpoints TargetRevoked before local teardown. TestNetworkAbortBeforePreparationPreservesSource, TestNetworkAbortCheckpointsRecoveredTargetAfterSourceChange, TestNetworkAbortPersistsRevocationBeforeRetryingLocalStop and TestNetworkAbortRejectsActiveNodeAndRetainsUncertainCleanup cover terminal/retained plans and restart ordering. Native host now starts and joins StartNetworkSelectionWorker with real catalog/registration/cleanup providers; durable dispatch advances preparation, registration, activation, apply and abort. TestNetworkWorkerResumesAllPhasesFromDisk, TestNetworkHostJoinsRegistrationOnShutdown and TestNetworkRemoteCompensationDoesNotBlockDisconnect cover orchestration and lifetime boundaries. Remote compensation no longer holds the tunnel lock. Source CAS now permits verified monotonic endpoint/peer observations while preserving network/node policy and all non-map config bindings; TestNetworkSourceRefreshDoesNotCancelIsolatedRegistration and TestNetworkSourceRefreshRejectsAuthorityChangesAndInvalidMaps cover refresh during registration and restart handover plus policy/identity/trust/intent/hash/signature/rollback rejection. TestNetworkSelectionThroughNativeTransport covers authenticated local transport admission, missing-key rejection, pending/terminal replay, payload conflict, BUSY, successful target adoption, unauthorized catalog rejection and capability removal on shutdown, using injected domain providers and tunnel driver. Dispatcher retry classification now covers every durable phase, including resumed remote compensation and local apply cleanup. TestNetworkDispatchRetriesCompensationAcrossRestart and TestNetworkDispatchRetriesApplyCleanupWithoutReapplying cover UNAVAILABLE/DEADLINE_EXCEEDED/LIMIT_EXCEEDED, retained barriers, original failure preservation and no repeated registration/apply across disk restart. Native provider units now retry a validated temporary revoke and temporary map refresh against synthetic HTTP without registering again or logging out the shared session; malformed/rejected revoke remains unconfirmed. Validated registration authorization_denied now reaches native network RPC as PERMISSION_REQUIRED. TestNetworkRegistrationDenialPreservesUncertainAttempt covers complete producer rejection versus malformed/miscorrelated responses and confirms compensation retains/replays the exact uncertain request without unbound node cleanup or shared-session logout. A rejected replay does not prove a prior uncertain registration created no node; permanent compensation recovery remains open. Complete permanent-error classification, native backend workflow qualification and full recovery/platform acceptance remain required for UF-07/UBR-10/UI-AC-14. Injected Stop units do not qualify native OS route removal or selection acceptance. |
| US-05 exit | `service_rpc_exit_catalog.go` lists live grants from the authenticated active-profile map | `TestRPCExitCatalogUsesSignedBoundGrants` checks signature/identity/expiry/privacy/page binding; selection remains unavailable | Durable selection, platform family/LAN support, effective status and fail-closed effects remain unimplemented |
| US-06 trust/recovery | `service_rpc_trust_worker.go`, `service_rpc_trust_recovery.go` | `TestRPCTrustWorkerRecoveryAndIndependentDisconnect` | Audit exact authority tuple, replay and privilege outcomes; helper/OS integration remains later evidence |
| US-07 diagnostics | `service_rpc_diagnostics.go`, bundle worker/store/read handlers, `route_observations.go` | `TestRPCDiagnosticsRejectsChangedContextAndInvalidProvider`, `TestRPCAdministratorBundleScopeAndRestart`, injected route collector tests | Route sampling implemented but platform execution unqualified; inspect redaction, bounds and archive lifecycle coverage separately |
| US-08 profiles/logout | Profile, logout and forget handlers | `TestRPCProfileRemovalGuards`, `TestRPCForgetCancelsQueuedEnrollmentAfterRestart`, `TestRPCProfilePaginationBindingsAndPrivacy` | Audit all removal/cleanup/replay cases; remote revocation depends on backend result |
| US-09 session/renewal | Session reads, durable renewal/poll executor, dedicated host worker, public `RenewSession`, bound snapshot identity and generated backend transport | Session read/clock/store suites; `service_rpc_session_worker_test.go`, `service_rpc_session_executor_test.go`; C `service_rpc_session_transport_test.go` | Full concurrency/cleanup audit, additional approved browser origins and platform/backend qualification remain open; no seamless-renewal acceptance claim |
| US-10 preferences/policy | `service_rpc_uiquit.go`, network preference admission/worker/projection, inbound filter and router | `TestInboundPreferenceAtomicPatchRestartAndSelectiveReset`, `TestInboundPolicyLockRejectsWholeMixedPreferencePatch` and worker/filter tests described above | Remaining lifecycle keys, authenticated transport mutation and OS effects need evidence |
| US-11 resources | `service_rpc_resources.go` reads the authenticated active-profile map with bounded search and typed targets | `TestRPCResourcesAuthenticateFilterAndBindPages` covers authentication, privacy, filters, page binding and immutable source | Effective enablement, policy controls, overlap/conflict projection, runtime availability and mutation remain open |
| US-12 lifecycle | `service_rpc_uiquit.go` | `TestRPCUIQuitPreferencesAndExecution` | UI-quit support is not logoff/suspend/resume adapter implementation; audit each specified event |
| US-13 distribution/help | `service_rpc_update.go`, `service_rpc_handlers.go`; packaging and producer manifest workflows | `TestRPCUpdateInfoDoesNotInferReleaseOrPairing` | Verified update source remains absent; installation/release evidence deferred until implementation phase completes |
| US-14 presentation/privacy | Typed status/operation/log/diagnostics projections | `TestRPCObserverSnapshotExcludesPrivateState`, diagnostics suites | Audit producer reasons/actions and secret redaction; UI rendering, locale selection and assistive technologies belong outside this task |

The historical [runtime gap audit](native-runtime-gap-audit.md) reported nine
missing methods at its pinned source. The current count is four: `GetUpdateInfo`
has an explicit unavailable-source implementation and `GetSession` now performs
a backend read, `RenewSession` now has a runtime worker and transport, and
`ListExitNodes` reads authenticated live exit grants from the cached map, and
`ListResources` reads disclosed hosts/subnets/services and source-authorized
applications from that map.
This does not complete update discovery or session renewal acceptance.

US-12 unit increment: `TestRPCUIQuitReplayDoesNotApplyChangedPreference` checks
that an acknowledged keep-intent notification replays the original operation
after changing the preference to disconnect, both before and after reopening
the durable store. Replay must not change revision or connected intent. A new
request ID must still schedule disconnect using the new preference. This tests
local durable admission, not OS suspend/logoff integration or completed Down.

US-10 projection increment: `TestRPCPreferencesRequireActiveProfileForMutation`
checks that an inactive profile's UI-quit setting reports a temporary mutation
restriction with the stable `preference_requires_active_profile` reason, while
the active profile reports availability. The effective default, absence of an
override and snapshot revision are preserved. The same test checks that Set and
Reset reject the inactive profile without state changes. Managed settings reuse
this preferences projection; the other preference keys remain unimplemented.

`TestRPCPreferenceReadAuthorizationAndValidation` covers owner/administrator
reads, anonymous callers (including a claimed administrator with no identity),
and non-owner reads against existing, missing and malformed profile references.
Authorization must precede profile lookup so those references do not reveal
profile existence to observers. Authorized malformed/missing references return
typed validation/not-found errors. Failed reads expose no response, and every
case leaves the durable configuration unchanged. These are handler-domain unit
assertions, not evidence for OS peer authentication or backend policy behavior.

US-07 route-target increment: `WireGuardRouteTargetsForPeers` now includes every
host address of each peer, rather than only the first address. The unit test
`TestWireGuardRouteTargetsRetainEveryPeerAddress` covers a dual-stack peer,
additional host addresses, equivalent textual duplicates, invalid values and
subnet exclusions. Agent/CLI route-target consumers receive the complete host
list; the separate OS sampling implementation and its limits are described below.

The diagnostics service now accepts a separate `OSRoutes` observation list and
projects it only with a verified profile map. Desired `Tunnel.Routes` are never
used as OS evidence. `TestRPCDiagnosticsSeparatesObservedRoutesFromDesiredRoutes`
checks this boundary, interface comparison, failed observations, redaction,
copy isolation, address validation and entry limits. Partial observations retain
the incomplete marker.

`ObserveOSRoutes` is now wired into agent diagnostics after map verification.
It samples up to 32 unique literal host addresses with a shared three-second
deadline and a 16 KiB per-command output bound. Linux uses `ip route get`, macOS
uses `route -n get`, and Windows calls the pinned x/sys `GetBestInterfaceEx`
binding followed by interface lookup by the returned index. Windows no longer
starts a PowerShell process for each target. Untrusted address text is never
interpolated into a command. Subnets, scoped addresses, unsupported platforms,
lookup failures and exhausted collection budgets do not become successful route
claims. Responses remain marked partial: sampling peer host addresses is not
full routing-table or subnet/exit acceptance.

`route_observations_test.go` covers both Unix command/parser branches with an
injected executor, duplicate/invalid addresses, cancellation, target/output
limits and failure redaction. `TestWindowsNativeRouteObservation` covers the
native IPv4/IPv6 sockaddr binding, zero/error indexes, disappeared or mismatched
interfaces, invalid aliases and cancellation between native calls. The agent
test verifies that an unverified map does not invoke route collection. These
unit tests inject OS boundaries; platform execution qualification remains open.
The native lookup removes per-target process startup from the strict diagnostic
snapshot-consistency interval; it does not weaken the rejection of changed
configuration or prove all STALE_STATE failures in run 34881749817 resolved.

Snapshot intent audit: `snapshotLocked` clears provider-supplied intent and the
user-disconnected flag before projecting the durable configuration, including
when that configuration has no intent. This prevents a cleared intent from
reappearing through a stale observation. The regression
`TestRPCSnapshotDoesNotRestoreClearedIntentFromObservation` checks both owner
and observer snapshots. This proves projection precedence, not actual tunnel
shutdown or backend session revocation.

Startup policy recovery now projects the original requested intent while its
context-bound checkpoint remains valid. An unavailable/expired policy still
holds the runtime disconnected; this operational gate is not a user Disconnect.
Both native agent status and RPC snapshots use `RequestedConnectionIntent`.
No recovery hash or private checkpoint is serialized, and connection phase
remains independent. `TestRPCStartupRecoveryProjectsRequestedIntentWithoutStarting`
checks reopened state, owner/observer snapshots, opening events, immutable reads
and explicit Disconnect supersession. `TestRequestedIntentRejectsUnboundRecovery`
checks owner/profile/node/network/credential/session-token/preference changes and
invalid checkpoint reasons. Existing startup-recovery tests still require fresh
authenticated policy before restoring runtime admission. These units address
the misleading intent seen in cached-map/sharing restart CI failures; actual
traffic recovery and platform acceptance still need evidence.

Network activation resolves that same context-bound requested intent against
the still-current source, after confirmed source teardown and renewed target
authority validation. It no longer copies the source's temporary disconnected
gate and discards the saved Connect. The adopted target gets a detached intent
with no source startup checkpoint. `TestNetworkSelectionPreservesBoundRequestedIntentAcrossActivationRestart`
checks valid versus invalidated recovery and explicit Disconnect, disk restart
between activation/apply, verified target-only Start, and no repeated effects
after completion. This does not substitute requested intent for target policy
authorization or qualify the native OS/backend selection path.

`TestRPCRejectedObservationPreservesLastAcceptedStatus` exercises five config
changes during a probe without an RPC revision change: active profile, node,
network, owner and connection intent. The full-config fingerprint must reject
the late observation with `STALE_STATE`, retain the last accepted observation
and not increment the revision. This is a unit test of observation admission;
it does not exercise a real profile-switch worker or qualify cross-network
traffic isolation.

Event resource bounds: `subscribe` admits at most 16 subscriptions per runtime
and four per OS identity (using the same case-insensitive identity comparison
as authorization). Admission and removal share the mutation lock. Rejection
returns typed `LIMIT_EXCEEDED` before allocating a snapshot/queue; administrators
do not bypass the resource cap. Unsubscription releases the slot. Together with
the existing 8 MiB queue bound this caps accounted queued event payload at
128 MiB, not total process/transport memory. `TestRPCEventSubscriptionLimitsAndRelease`
checks both caps, identity casing, anonymous rejection before capacity errors,
slot reuse and snapshot-first sequencing. Real slow-consumer load qualification
remains deferred.

`TestRPCConcurrentEventSubscriptionAdmission` issues 32 concurrent subscription
attempts, first for one identity and then for distinct identities. It checks
exactly four or sixteen admissions respectively, typed capacity errors for all
remaining attempts, and zero retained registrations after unsubscription. No
subscription is released until every admission attempt has completed. This is
unit concurrency evidence, not a race-detector or transport-load run.

The native HC-052/HC-053 scenario previously attempted eight subscriptions from
one identity and discarded terminal RPC errors. Run 34881749817 consequently
reported an unexplained early stream end at its opening-snapshot assertion.
`TestControlPlaneIPCEvents` now fills the four-slot identity budget, requires
typed LIMIT_EXCEEDED for a fifth stream with no snapshot, and verifies both
Disconnect and Connect reach every admitted stream after that rejection. It
then retains the cancellation, slot reuse, independent delivery and host-restart
sequence checks. Unexpected termination reports only allowlisted transport and
domain codes. The updated native scenario still requires platform CI evidence;
the service limits and authorization rules have not been relaxed.

Session read increment (2026-09-14): `GetSession` authorizes the local caller,
selects the profile's user bearer, invokes the bounded TLS backend adapter and
validates the response with the producer's `ValidateSessionResponse`. Only state
and optional session deadlines are projected; backend IDs and renewal bearer
are never forwarded. Expiry/warning transitions use the session clock, not node
credentials. Missing bearer produces unauthenticated session state without a
backend request. Changed configuration, revoked owner and cancelled requests
cannot publish a successful response. The read now persists validated backend
session/renewal authority in the protected ConfigStore, bound to the user bearer
digest and control origin. Changed observations advance revision and invalidate
the session domain; identical protobuf responses do not rewrite state. Renewal
is explicitly unavailable until its durable execution workflow is implemented.
`TestRPCSessionReadProjectionAndContext` covers these
read-domain cases with an injected backend provider; actual backend deployment,
transport integration and platform/backend qualification remain open.

`rpcProjectSession` is shared by GetSession and snapshot/event projections.
Owner/admin snapshots only use stored authority matching the active profile
origin and current bearer binding; stale provider Session fields cannot override
it. Missing observations remain unknown, missing bearer is not-authenticated,
and observer snapshots exclude session data. `TestRPCSessionSnapshotUsesBoundAuthorityAndOwnClock`
checks those boundaries plus expiry independent of a valid node credential.
Fresh snapshots evaluate the session clock. The host now owns a one-second
session-clock worker for open subscriptions; it advances revision and publishes
status plus session invalidation only when a subscriber's projected state changes.
Observers retain the public status projection without session data/invalidation.
`TestRPCSessionClockPublishesOnlyTransitions` checks warning/expiry using controlled
time, no duplicate emissions, and unchanged tunnel intent. The cancellation test
checks worker termination. Host failures cancel/join this worker alongside other
workers. This does not renew a session or establish real idle-stream timing on
every supported OS.

`TestRPCSessionAuthorityPersistenceAndCleanup` checks private authority retention
across disk reopen, returned revision, identical-read stability, no bearer in IPC,
copy isolation, and durable removal on logout or bearer replacement/removal.
Config normalization also clears mismatched authority in inactive profiles;
inactive-profile execution coverage remains to be completed. The general
diagnostic redactor recognizes bearer, renewal-authorization and stored-session
keys (`TestDiagnosticsSessionAuthorityKeysAreSensitive`). Existing state
protection is reused without a format/version increase. This is not proof of
renewal success, fresh platform ACL qualification or backend deployment.

Renewal admission foundation: internal `renewSessionAs` writes an immutable
backend `RenewSessionRequest`, its renewal authorization, user/profile/origin
binding and local operation ID into protected RPC state before any network
effect. It uses producer request validation and permits an expired access
session only while its separate renewal grant remains valid. Public
At this earlier increment, `RenewSession` remained unimplemented until the execution/recovery worker existed;
no public caller can enqueue this unfinished workflow. The unit test
`TestRPCSessionRenewalAdmissionAndDurableReplay` checks owner denial without
mutation, pending-operation privacy, exact disk-backed replay and rejection of
a second operation. Backend dispatch, polling, response authority validation,
full token-rotation coverage, cancellation/cleanup and their negative coverage
remain required before exposing the RPC or claiming renewal functionality.
The retained renewal grant is now separate from the latest session observation:
the backend forbids issuing renewal authority in an expired/revoked response.
An expired observation preserves an earlier grant only when its user/session,
origin and bearer binding match and its own deadline is still valid. Revocation,
changed identity, grant expiry or an active response disabling renewal drops it.
Admission validates both the current observation and retained grant. The test
`TestRPCSessionGrantRetentionAfterExpiryObservation` covers all six cases through
read, disk reopen and renewal admission. This does not make the public renewal
RPC or execution worker complete. No legacy grant fallback is introduced.

Internal `completeSessionRenewal` now validates a successful backend result with
the producer validator and saved request/user/owner/profile/bearer bindings,
rejects expired replay/results, and atomically rotates the access bearer and
protected session while committing the public terminal operation and removing
the execution plan. It requires the operation to be RUNNING; no public RPC
dispatch is enabled yet. `TestRPCSessionRenewalResultAtomicRotationAndBinding`
checks successful rotation/restart/replay and six rejected result/context cases,
including unchanged durable state on rejection and preserved disconnected intent
and node registration. Network dispatch, polling operation-ID binding, browser
approval, ambiguous-failure retry and cleanup still require implementation.

Internal browser checkpointing now persists backend operation ID, private polling
authorization, replay deadline and next-poll time atomically with WAITING_FOR_USER.
The producer validator checks request binding and the configured control origin;
untrusted origins, elapsed actions, malformed polling authority and URLs carrying
known bearer material (including percent-encoded material) are rejected. Later
checkpoints and successful completion cannot switch a pinned backend operation
ID/replay deadline. `TestRPCSessionBrowserCheckpointPrivacyAndBinding` covers
checkpoint restart/privacy and these negative cases. The network polling worker,
additional approved authentication origins and public RPC admission remain open;
this internal checkpoint is not browser-flow acceptance.

`ReconcileSessionRenewal` now executes a single durable renewal/poll step using
producer DTOs and separate renewal/poll authorities. RUNNING and retry scheduling
are committed before dispatch; an ambiguous transport failure retains the same
request ID across restart. Server polling delays and replay deadlines are enforced.
Rejected, expired, malformed or stale-context outcomes clear private execution
authority with a durable typed failure; success rotates the token atomically.
`TestRPCSessionRenewalExecutorRestartPollingAndFailure` covers these paths,
cancellation before dispatch and token replacement during polling. The background
host worker, actual backend transport and public RPC/capability wiring remain open;
these unit checks do not establish end-to-end session renewal acceptance.

The session worker and public `RenewSession` are now wired into the native host;
the agent uses the generated producer client with separate renewal and polling
Authorization headers, HTTPS-only origins and bounded messages/timeouts. Capability
readiness follows worker lifetime. Session reads/snapshots expose a bound active
renewal operation ID and availability based on the current grant and worker.
`TestRPCSessionWorkerAdmissionReadinessAndShutdown` checks owner admission,
observer rejection, replay, duplicate-worker exclusion and cancellation/join.
`TestAgentSessionRenewalTransportUsesDedicatedAuthority` exercises generated
serialization against an in-memory handler (no external backend or socket).
Full cleanup/concurrency coverage, approved cross-origin browser approval and
real provider/platform acceptance remain open; seamless renewal is not claimed.

Confirmed local forget now durably requests cancellation of a same-profile
renewal, cancels an in-flight provider only after admission commits, and drains
the session executor before local cleanup. Cancellation removes private renewal
and polling authority and records a replayable CANCELLED operation without a
browser action. `TestRPCForgetCancelsSessionRenewalAndDrainsBeforeCleanup` covers
a late successful provider response, rejected unconfirmed forget and queued or
browser-waiting cancellation after restart. Logout/profile-switch concurrency
and the rest of the renewal cleanup matrix remain under audit.

`TestRPCSessionRenewalConflictsAndIndependentDisconnect` now verifies both
admission orders for renewal versus logout/profile switch: conflicts return BUSY
without changing durable state. A blocked renewal provider does not block
Disconnect, and later successful rotation preserves disconnected intent and node
authority. Session-clock invalidation now compares the entire public session
projection, not only its state enum. `TestRPCSessionClockPublishesRenewalGrantExpiry`
verifies that renewal grant expiry withdraws availability while access remains
ACTIVE, emitting one transition rather than silently leaving stale availability.
These are unit observations, not real traffic or platform qualification.

Exit catalog uses the pinned producer's signed `ClientPolicy.ExitNodes`, not
advertised/default-route inference or a nonexistent backend Exit RPC. The entire
cached map is validated and authenticated, with local node/network/revision
binding and live map/grant deadlines. Page tokens bind the signed payload and
visible catalog so grant expiry cannot continue a stale page. The list exposes
no selectable modes and reports `exit_executor_unavailable` until actual
platform-aware selection/application is implemented. No exit capability is
advertised by this read-only increment; Get/Select/Clear and traffic acceptance
remain open.

Exit routing foundation: `exit_routes.go` derives peer routes for an explicit
recipient/peer-key-bound local selection. It authenticates the selected grant,
checks exact family/LAN authorization and deadlines, and retains default routes
only for the selected peer and address family. No selection removes defaults
from the derived input; invalid selection returns an error, never a direct-route
fallback. `TestExitRouteProjectionRequiresExactLiveSelection` covers IPv4,
IPv6, dual stack, absent intent, expired/tampered grants, changed recipient/key,
denied mode/LAN and unchanged signed source/ordinary routes. This foundation is
not yet called by the live engine/router: packet enforcement, fail-closed
platform rules, durable admission and effective-status wiring remain required.

`exit_filter.go` adds an isolated TUN packet-enforcement layer using authenticated
recipient-bound maps and exact exit selection. Every packet checks map and grant
deadlines; changes permit only the old/new intersection before commit. Invalid
updates close the filter, and withdrawal cannot be undone by a stale commit.
`TestExitPacketFilterExpiryTransitionAndWithdrawal` exercises request/reply
expiry, IPv4/dual-stack expansion and restriction, clear, tampering, malformed
packets and withdrawal. This is an additional gate, not a replacement for ACL,
application or sharing checks. Engine/TUN wiring and OS protection against traffic
bypassing the TUN remain unimplemented; these units do not prove fail-closed exit.

The `applicationTUN` wrapper now accepts an exit filter and intersects it with
existing application/sharing checks in both directions and outbound peer ACLs.
`TestExitTUNEnforcesExpiryInBothDirectionsAndRetainsACL` uses an in-memory batch
device to verify live traffic, deadline withdrawal, ordinary-route preservation,
offset/batch compaction and independent ACL denial. The live engine does not yet
construct or activate this filter: engine lifecycle, durable selection, route
application and OS fail-closed protections still require implementation.

The live engine's shared UAPI renderer and OS-route builder now apply the
no-selection route projection: neither IPv4 nor IPv6 default routes activate
solely because they appear in a map. The shared UAPI path includes initial
configuration, endpoint refresh and rollback. `TestEngineDoesNotActivateImplicitExitRoutes`
checks both endpoint modes and configured states, ordinary-route preservation
and unchanged signed source. This intentionally removes implicit exit behavior;
it does not enable explicit Select/Clear, live exit-filter activation or OS
fail-closed protection, which remain open.

Internal exit admission now journals a recipient/peer-key-bound requested
selection (or clear), previous selection, owner, profile and control origin before
any routing effect. Exact supported family/LAN pairs are intersected with signed
grant authorization and current map revision. `TestRPCExitSelectionAdmissionBindingAndRestart`
covers invalid authority/policy/support, stale maps, privacy, pending conflicts,
offline clear and replay across restart without reevaluating previously accepted
support. `TestNodeCleanupRemovesBoundExitSelection` checks node/logout cleanup.
These internal methods are not public RPC overrides: no worker or ready exit
capability exists yet, and active routing is not changed by admission alone.

Exit completion now requires an exact per-family APPLIED observation, matching
requested/effective exit and LAN settings, and fail-closed proof for enabled
families. Disabled/cleared families must have absent selection IDs. The operation,
profile/owner/origin/node tuple, previous selection and current signed grant are
rechecked before atomic completion. `TestRPCExitResultRejectsPartialOrStaleApplication`
covers valid select/offline-clear completion and restart replay, partial/missing
family evidence, lost protection, wrong LAN, unexpected IPv6, expired grants and
changed node/selection. These injected observations are not real OS evidence.
The executor, partial-failure recovery, live effective-status provider and public
Select/Clear wiring remain open; this helper alone does not implement exit apply.

Static WireGuard export also strips implicit IPv4/IPv6 default routes before
rendering, preserving ordinary peer/subnet routes and the original signed map.
`TestStaticExportDoesNotActivateImplicitExit` covers both legacy LAN-block option
values and the internal sharing-enforcement flag: neither enables exit routes or
exit LAN hooks. Static export cannot enforce grant expiry or platform fail-closed
transitions and does not implement explicit exit selection.

`service_rpc_exit_executor.go` now serializes one internal apply attempt and
persists RUNNING/retry time before dispatch. It rechecks owner, profile, recipient,
previous selection, signed grant, map revision and exact platform family/LAN pair.
Invalid queued intent fails without OS dispatch; ambiguous, cancelled or partial
dispatched effects retain the durable guard and retry the same operation ID.
`TestRPCExitExecutorDurabilityAndRevalidation` covers these cases and disk restart;
`TestRPCExitExecutorSerializesConcurrentAttempts` checks duplicate dispatch.
The adapter is injected in unit tests only. Actual OS protection/application,
containment after context changes during an attempted apply, public Select/Clear,
GetExitNode observations and background worker lifecycle remain unimplemented.
A retained operation guard is concurrency protection, not an OS fail-closed rule.

Resource catalog increment: stable opaque IDs distinguish kinds, peer/subnet
tuples and individual service ports. Default routes are excluded from ordinary
subnets; applications require the local signed source identity. Search intersects
kind filters; page tokens bind caller/profile, signed payload, canonical query
and result digest. Expired, tampered, foreign-recipient and revision-mismatched
maps are unavailable. Enabled now reports authenticated committed policy
resolution, with pending Requested kept separate. Source, policy ID and locks
come from the producer policy; local choices retain USER provenance unless
locked. Worker absence and a pending configuration change disable mutation.
The catalog and admission share the same conflict check for every nonterminal
operation, including Disconnect without a preference plan. The check runs once
per catalog read. `TestResourceMutationControlMatchesDisconnectConflict` checks
that the catalog restriction matches BUSY admission, and disappears after Down
completes while worker absence remains a separate restriction.
`TestResourceCatalogCommittedPendingAndPolicy` checks these distinctions.
Availability still explicitly reports missing runtime observations; a policy
value does not prove reachability. Catalog overlap reporting now uses the same
packet scopes as denial compilation: host/subnet addresses, service transport
and port, and application target routes. Symmetric overlap IDs are computed
before filtering and pagination; defaults never enter those scopes. Work is
bounded by 8192 scopes, one million comparisons and 32768 emitted links, with
LIMIT_EXCEEDED instead of an incomplete overlap claim. Tests
`TestResourceOverlapProtocolPortAndFamily` and
`TestResourceCatalogOverlapSurvivesKindFilter` cover transport/family separation
and a service's overlap with a filtered-out host. Deny precedence remains in
the packet filter; these links do not establish actual reachability or OS
acceptance. No resources capability is advertised by this step.

## External dependencies and approvals

Exit events now invalidate EXIT_NODE on select/clear operation admission and
state transitions. GetExitNode projects the durable containment cause while
keeping the aggregate pending and per-family enforcement unknown. The test
`TestExitEventsAndContainmentReadShareCommittedRevision` checks profile/revision
binding and the distinction between a recovery reason and observed protection.
This adds operation events, not live OS path-loss observation or enforcement.

Exit context-loss containment now has a durable phase and a separate executor
callback. Dispatched operations that lose context enter containment before the
callback; current matching identity receives disconnected intent without
overwriting an accepted Disconnect or another identity's intent. Restart resumes
containment without Apply. Terminal failure/cancellation requires bound proof
that exit routes were removed and both families remain blocked. Callback error,
partial evidence or foreign operation proof retains the guard. The test
`TestExitContainmentRequiresBoundProofAndRecovers` checks checkpoint ordering,
store reopen, failed/partial/foreign proof and original Disconnect outcome.
This is executor orchestration, not an OS containment implementation. Real
adapters, shared OS serialization and privileged-platform qualification remain
required before public Select/Clear can be enabled.

Exit intent race: admission now snapshots ConnectionIntent, and preflight and
completion both require that snapshot to match. An accepted Disconnect during
Apply prevents stale success and preserves the dispatched operation's recovery
guard. `TestExitApplyCannotCommitAfterAcceptedDisconnect` checks late Disconnect,
nonterminal outcome, preserved user reason and rejection of redispatch after
store reopen. This closes the missing intent binding; OS containment still must
be implemented before that retained guard can safely become terminal.

Exit request read: public GetExitNode now exposes the profile's durable request
and pending select/clear intent, including per-family requested IDs and LAN
choice. With no OS observation provider, effective IDs remain absent, actual
fail-closed remains unconfirmed and observation-unavailable failures are
explicit. Saved requests survive grant withdrawal as intent, never as current
authorization. `TestExitReadSeparatesDurableRequestAndUnknownEnforcement`
checks absent/saved/pending/clear states, family separation and owner access.
This does not implement public Select/Clear, OS application or positive status.

Resource restart evidence: `TestResourceWorkerRestartsApplyOrContainment`
reopens the durable store after worker cancellation or failed Down. A cancelled
worker resumes its pending apply; persisted containment performs Down without
starting the failed candidate. Apply failure retains the previous explicit
choice, Disconnect retains its reason and cancellation outcome, and request-ID
replay after recovery returns the same terminal operation. This uses injected
runtime effects and a real store reopen; it does not establish OS crash safety.

Runtime policy ownership audit: `cloneRegisterNodeResponse` previously retained
the caller's ClientPolicy pointer in engine plans and rollback snapshots. It now
deep-copies resource policy entries, setting value pointers and exit family/LAN
constraint slices using producer DTOs. The signed-map regression
`TestEngineMapClonePreservesSignedPolicyAfterCallerMutation` verifies that caller
and rollback-copy mutations cannot invalidate the applied snapshot's signature.
`TestClientPolicyCloneOwnsExitConstraints` checks nested exit constraints and
absent policy preservation. This fixes memory ownership, not exit OS acceptance.

Resource read cancellation: overlap and applied-denial projection now check
request cancellation during iteration under the RPC read lock. Applied-denial
comparison is capped at one million with typed LIMIT_EXCEEDED, matching the
existing overlap work bound; no partial catalog is returned on either failure.
`TestResourceProjectionCancellationDuringObservation` checks cancellation by
the observation provider, cancellation before either calculation, and unchanged
durable revision. This does not qualify OS networking.

Resource enforcement observation: `WireGuardEngine.TryResourceEnforcement`
authenticates the requested configuration and confirms a configured device,
exact signed payload, current committed denial rules and unexpired authority.
It returns no confirmation during Configure/Down, a staged or withdrawn filter,
changed choices, or a different map with the same revisions. The existing
`TestResourceEngineAppliesChangesAndRejectsStaticBypass` checks map and choice
bindings, engine contention, closure and expiry with a test TUN/router.
The agent now wires this observation into ListResources. A confirmed denial
projects `resource_packet_restriction_applied` for each intersecting resource;
partial intersections remain restrictions, not claims that all its traffic is
blocked. `TestResourceAppliedDenialProjectionRequiresObservation` checks a host
and overlapping service, loss of confirmation, and rejection before provider
invocation for an unauthenticated map. No confirmation is cached across reads.
The host-owned one-second resource clock now invalidates RESOURCES when the
bound enforcement observation changes, even without a Status change. It
authenticates before observation, binds map/profile and local choices, checks
the current source again before committing a revision, and joins on shutdown.
`TestResourceObservationClockInvalidatesTransitions` checks confirmation/loss,
revision and profile binding, unchanged-state suppression and invalid-source
rejection; `TestResourceObservationClockStopsWithHost` checks cancellation.
Positive availability still needs route/path/application evidence. Neither an
applied filter nor a successful operation proves reachability.

Resource mutation worker increment: private `setResourceEnabledAs` admits a
durable resource choice into the same transaction worker as network preferences.
It authenticates the active map, resolves the canonical disclosed resource,
honors managed locks and rejects conflicting pending operations. Admission does
not publish the choice. The worker applies a candidate, rechecks map, profile,
intent and previous resource choices, then commits. Failure retains the previous
choice and enters durable disconnection containment; Disconnect supersedes apply.
`TestResourceChangeAdmissionAndRestart`, `TestResourceChangeRejectsAtomically`
and `TestResourceWorkerAppliesOrContains` cover admission/replay/restart, locks,
invalid sources, apply failure and concurrent changes with injected drivers.
Public `SetResourceEnabled` now requires a live profile worker, admits the
authenticated local caller and wakes reconciliation. Resource operations emit
resource-domain invalidation on admission and state transitions.
`TestResourcePublicTransportApplyReplayAndEvents` uses the actual local native
transport and an injected driver to check authentication, missing-worker denial,
pending-to-committed false choice, terminal replay and revision-bound resource
events. Runtime reachability observations, overlap reporting and OS effect
qualification remain open; resources capability is not yet advertised.

- `clientapi` owns backend DTOs, policy validation and session transport. On
  2026-09-14 the user explicitly approved consuming producer revision
  `e4fb0a95d2af577cda7425abec064a4ed49a3eae`. `client` now pins its resolved
  Go pseudo-version `v1.12.1-0.20260913120316-e4fb0a95d2af` in `go.mod`.
  This removes the dependency-update approval blocker for session and policy
  contract consumption; the missing runtime handlers listed above remain open.
  No other dependency version or producer repository is changed. Further
  version increases still require explicit approval. Do not copy backend DTOs,
  use a local replacement or rewrite a published version.
- Backend owners must implement the corresponding producer behavior. The
  existence of a producer contract is not evidence of deployed behavior.
- Distribution source selection and external platform/provider capabilities
  require their owners' input. Record each concrete dependency as requirements
  are audited; do not invent product links, sources or acceptance evidence.

These dependencies do not prevent work on independent client-owned functionality
and unit tests. They do prevent claiming the affected requirements complete.
