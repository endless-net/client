# Native diagnostics runtime snapshot

The native bundle archive builder is now implemented independently of the retired
DTO builder. It takes only a native Diagnostics projection, clones it, strips
unknown fields/browser URLs and redacts strings before serialization, then emits
a single fixed-name diagnostics.json ZIP entry. Source and encoded JSON/archive
sizes are bounded to 5 MiB; partial/failure markers remain visible. Tests inspect
the archive content, nested/repeated redaction, source immutability and JSON size
expansion. CreateDiagnosticsBundle now durably accepts a profile-bound plan and
executes this builder on a dedicated native-host worker, independently of request
cancellation. Startup scans unfinished plans. Artifact UUID equals operation UUID;
an artifact saved before a crash is reused without recollection before publishing
the successful result. Owner/profile/cleanup changes reject stale collection.
Storage failures preserve the unfinished plan and stop the host; provider and
capacity failures become typed terminal operation failures.

A separate bundle store primitive now owns immutable archive bytes
and cloned metadata. It binds opaque UUID handles to owner/profile, expires them
after 15 minutes, supports bounded 64 KiB/default and 256 KiB/maximum reads, and
limits retention to 32 handles/20 MiB without evicting live handles silently.
Foreign/expired handles return NOT_FOUND; explicit revocation removes a profile's
entries. Tests cover binding, expiry, offsets, copy ownership and capacity. This
store is opened by the native host alongside its protected configuration. File-backed mode
atomically persists creation and revocation before publishing changes in memory,
restores immutable descriptors/bytes after restart, validates hashes, size, TTL,
identity and capacity, and ignores expired records. Archives stay outside the
main configuration. Files use mode 0600 and existing machine-DPAPI protection on
Windows. The atomic writer installs a protected Windows DACL for the process user,
SYSTEM and Administrators on the empty temporary file before writing any payload.
Restoration rejects unprotected/public ACLs, untrusted owners and reparse points.
Tests check Everyone-read rejection and protection failure before data write,
including temporary-file cleanup and preservation of the previous file.
The integrating runtime must still supply a private ACL-protected directory
and hold its single-writer agent lock. Tests cover restart, durable revocation,
expiry, malformed storage and failed-write rollback. The artifact commits before
the atomic operation result. Short tests recreate the coordinator and file store
at pending and artifact-saved boundaries, and cover replay, readback, cancellation,
capacity, provider failure and owner/profile changes. This is not installed-service
or process-kill acceptance evidence.

The native local-transport short test now exercises CreateDiagnosticsBundle,
GetOperation and chunked ReadDiagnosticsBundle together with the real worker and
file-backed store. It cancels the accepted request, checks unpublished-handle
denial, validates ZIP content/SHA-256/size, checks replay without recollection,
and revokes the current connection's access after an owner change. A separate
worker test interrupts collection, verifies the durable RUNNING plan survives,
then restarts the coordinator/worker and completes it without replaying a request.
These controlled tests do not validate UI rendering, installed service restart,
process-kill boundaries, or platform inspection completeness.

ReadDiagnosticsBundle now checks current owner admission and the durable successful
CreateDiagnosticsBundle outcome before copying a chunk. Its descriptor must match
the stored artifact exactly. Orphaned/unpublished archives, removed profiles and
handles predating logout/local-forget admission are inaccessible; an unrelated
profile's cleanup does not revoke the handle. Short tests cover these conditions,
expiry, cancellation, foreign operation ownership and descriptor mismatch. These
checks provide immediate logical revocation. The worker also durably sweeps expired,
revoked and orphaned bytes at startup, after execution and every five seconds;
unfinished valid plans retain their archives for recovery. Tests verify durable
orphan removal and expiry even after a read already pruned the in-memory entry.
DIAGNOSTICS remains unadvertised because OS inspection is still partial.

The retired HTTP Diagnostics and DiagnosticsBundle routes and agent callbacks
are removed. Their old paths must return 404, including POST of the former bundle
request. The native local transport test verifies owner-only collection,
current instance metadata, explicit partial/failure state, raw driver error
suppression and access revocation on an existing connection. The retired typed
HTTP diagnostics builder, conversions and OS-version wrappers are removed.
Native tests cover redaction and preservation of disconnected intent during busy
tunnel inspection. The old path-returning bundle
transport test is removed; native transport now checks immutable handle/chunk
delivery instead. The legacy seven-day JSON-file store and its path-returning,
minute-window reuse tests are removed. Native tests own bounded immutable storage,
TTL/replay, redaction, ACL and linked-file/parent rejection. Both open and persistence
validate path components; only the standard macOS /tmp and /var aliases to /private
are allowed. Symlink tests skip on hosts that cannot create symlinks.
tests/control_plane_diagnostics_test.go now uses native CLI protobuf JSON,
explicit retained request IDs/CAS, operation completion and handle-based ZIP
download. It covers control outages, disconnect/restart and exact replay/download
after process crash. It no longer reads server archive paths, changes mtimes or
repairs public export directories: those are retired contract semantics. File
failure and TTL checks remain in native component tests. The process scenario is
compiled but skipped by local short verification; CI execution is still required.
The shared test harness now waits for native runtime-info at startup and exposes
strict protobuf helpers without converting responses to HTTP DTOs.
The remaining control-plane lifecycle and cached-DNS projection scenarios now use
native diagnostics/status and terminal operation waits. Stream cursor recovery,
map changes, outage retry without re-enrollment, disconnect/restart and logout
cleanup checks are retained. Legacy diagnostics and bundle Go DTOs are removed
from ipc/v2/contract.go; the rest of the retired IPC package/specification still
requires cutover. These process scenarios are compiled, not run by local short
verification. Standalone headless diagnostic
output remains separate from service IPC. UI and
installed-service acceptance remain open; transport unit evidence does not prove these.

The two live DNS scenarios now use native status/diagnostics and explicit
connect/disconnect operations with terminal-result waits. Signed-map revision,
peer withdrawal/rename, exact A/AAAA address sets, UDP/TCP queries, listener teardown
and OS resolver checks are retained. The record-set helper has local tests for
ordering, missing/duplicate and obsolete addresses. These process/network scenarios
remain skipped by short verification and require CI execution. Other unmigrated
scenarios still use the separate legacy DNS-listener helper; no DTO translation
or fallback was added to the native scenarios.

The fresh-install stage now checks native runtime protocol/version/digest,
platform/architecture, status snapshot identity and unenrolled state. It no longer
expects profile-less diagnostics or network catalogs to return an empty report;
diagnostics without a profile must fail explicitly. Other installation stages
(disconnect/restart, enrolled reinstall/upgrade, uninstall and identity reset) still
use legacy helpers and require conversion. The fresh-install migration is compiled
by short tests, not installer acceptance evidence; no local installer was run.

Malformed control-response and temporary-outage scenarios now also use native
status. Malformed stream responses are observed through a CURRENT Agent.LastFailure,
not inferred from the separate control health probe. Recovery requires the named
post-fault map and a current error-free agent snapshot, avoiding acceptance of an
unrelated revision that already exceeded one. Temporary outage/restart still checks
retained identity, credential/cache presence and exactly one registration. Predicate
tests distinguish control health from current/previous agent failure; the process
scenarios remain unexecuted locally. The eight-case typed terminal-error matrix
now uses the same native fixture and status: unknown/revoked/expired credentials
must reach NEEDS_ENROLLMENT with an explicit StoredState projection showing no
credential or cache; invalid credentials, binding, session, policy and temporary
errors retain enrollment. Nonterminal recovery requires the named post-fault map
and a current error-free agent snapshot. This preserves the distinction between
terminal map-stream cleanup and the separate enrollment-renewal recovery flow.
Other legacy status consumers still need conversion; these process scenarios need
isolated CI execution before they count as acceptance evidence.

Registration response-binding (six faults) and lost-response recovery scenarios
now observe the agent through native status as well. They still exercise the
standalone headless `up` command, not native enrollment RPC acceptance. They check
that retries preserve a nonempty operation ID and signed request hash, commit
exactly one node, and recover that node with credential presence and a verified
cache. Changed pending input must not reach the registration endpoint. Rejected
responses and terminal cleanup use an explicit enrollment-absence predicate;
short tests reject missing projections, retained node/credential/cache and
inconsistent cache-valid flags. Process-level registration evidence still requires
isolated CI; short tests only compile those guarded scenarios.

Delta acceptance/rejection/cursor-resync, invalid signed-map rejection and browser
enrollment recovery now use native status. Delta teardown waits for the native
Disconnect operation to succeed before stopping the agent. Exact map revisions,
peer counts, node identity and disconnected intent remain checked; invalid maps
must leave an offline-verifiable cache, and revocation must clear enrollment.
The duplicate HTTP-status scenario fixture is removed: all control-plane fixtures
now bootstrap through native IPC. This does not migrate the remaining legacy
requests inside other scenarios (events, negotiation, validation, trust, network
selection, DNS, ownership and interrupted cleanup). Those still require conversion.
These process scenarios have not been executed locally and are not CI acceptance
evidence merely because their short-test compilation succeeds.

The single-agent ownership process scenario is now migrated to native status and
Disconnect/GetOperation. Canonical, lexical and symlink aliases still race for
one configuration; distinct endpoints rule out address-in-use as the rejection
cause. The live host must retain its instance ID after duplicate rejection, while
graceful successor and crash recovery must expose new instance IDs. Node, network,
active profile, credential/cache and connection intent remain bound across these
transitions, and the concurrent-start winner must remain available through native
IPC. Exactly one registration is required. This replaces the ownership scenario's
legacy requests mentioned above; interrupted cleanup and the other listed consumers
remain pending. Local short tests compile this guarded scenario, not execute its
process races, symlink handling or per-platform restart acceptance.

The agent now configures GetDiagnostics from engine Inspection and local interface
inspection. The native handler combines those observations with the current native
status, build/runtime metadata and profile-scoped transition log window. No HTTP
v2 handler or envelope is used. Windows OS version is available through the local
version probe; missing OS version on other platforms is explicit in failures.

The engine is global to the active profile: inactive-profile diagnostics returns
UNSUPPORTED, never another profile's engine state. The handler authorizes before
collection and reauthorizes/rechecks persistent configuration afterwards. A caller,
profile or revision change rejects the result. Cancellation is checked around
collection. Engine inspection uses TryInspection; a busy engine returns typed
BUSY without waiting for Configure/Down. Interface enumeration remains synchronous.
No mutation lock is held while querying the provider.

Tunnel/interface error text is replaced with fixed typed failures. Interface
collections, numeric ranges and final response size are bounded; returned slices
are copied. The RPC returns metadata/status from the same native snapshot and
does not infer connectivity from interface presence.

The agent verifies the cached map before adding DNS configuration/record metadata
and overlap conflicts between overlay and local interface prefixes. An absent or
invalid map produces an explicit unavailable-section failure and no DNS/conflict
data. These DNS values describe map configuration, not evidence of OS resolver
application or successful name resolution. Conflict reasons use a fixed key, not
arbitrary source error text; DNS output is cloned.

Peer identities/hostnames and overlay host addresses now come from the verified
map. Subnet routes and host routes outside the map's overlay CIDRs are not reported
as overlay addresses. Live WireGuard observations join strictly on public key;
unknown keys, ambiguous map identities and invalid handshake/keepalive values
produce an explicit failure rather than an invented peer. Successful inspections
provide copied counters, endpoint, allowed IPs and optional handshake timestamp.
An absent handshake remains absent, not Unix epoch; failed/busy inspections do
not expose live peer statistics. No handshake implies reachability or path choice.

Path-monitor observations are now read with TryPathStatus under a nonblocking
engine lock and must match the applied network/node/revision. Busy, disconnected
or mismatched-map observations produce an explicit unavailable-section failure.
The projection preserves monitor-selected direct/relay path, candidate health,
RTT and timestamps, but keeps the verified hostname. Invalid peer IDs, enums,
timestamps, numeric ranges or nonfinite RTTs reject the affected path projection.
Raw selection/probe errors are replaced by fixed reason keys. Monitor absence
never triggers a handshake-based or cached-status fallback.

This is a **partial runtime implementation**. OS route inspection and default
route presence are not collected; `truncated=true` and an explicit
unsupported-section failure prevent interpreting empty/default fields as a clean
report. Engine Inspection.Routes describes configured engine targets, not an
independent OS route-table observation, and is intentionally not relabeled as one.
Bundle creation/download and
full OS/runtime diagnostic acceptance remain open. The whole DIAGNOSTICS
capability remains unadvertised until these required providers are ready.

Short tests verify owner admission, active-profile isolation, coherent metadata,
raw-error suppression, copied interface data, log inclusion, partial status,
provider failure, cancellation, ownership/revision changes and size/range rejection.
Additional tests verify signed-versus-tampered map gating, busy nonblocking engine
inspection, pre-cancelled collection, DNS copy ownership and fixed conflict reasons.
Peer tests cover identity joins, duplicate/foreign/unmapped identities, host-prefix
filtering, absent/invalid handshakes, keepalive bounds, copied counters/addresses,
and exclusion of unverified or failed-inspection live data.
Path tests additionally cover applied-map binding, busy/disconnected engine,
nested-copy ownership, direct selection/RTT conversion and malformed observations.
They do not prove installed-service or per-OS inspection behavior. `client-ui`
owns combined real-host rendering and bundle/export tests after capability readiness.
