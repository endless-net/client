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

Interrupted Disconnect now uses native mutation metadata, observes its RUNNING
operation by request ID at the held offline-response boundary, kills the process,
and requires the same operation to finish after restart. Exact replay retains the
original request/CAS tuple and must return the identical durable outcome. Connect
also waits for its native operation. Inspection exposed a missing runtime action:
the native Stop driver previously never notified control that the node went offline.
It now attempts that notification only after successful local tunnel teardown,
with a two-second request lifetime, without adopting the response map or writing
configuration. Remote rejection/timeout cannot undo successful local teardown.
Short driver tests cover ordering, rejection, timeout, cancellation, teardown
failure and unchanged configuration. The process-kill scenario remains guarded
for isolated CI and has not been executed locally.

The standalone DNS wire-recovery scenario now uses native Disconnect/GetOperation
and confirms retained enrollment/cache plus disconnected intent before starting
its separate DNS proxy. Its split-DNS isolation, response binding, A/AAAA changes
and UDP/TCP fallback checks are unchanged. Live DNS map readiness now requires
READY control state instead of accepting every state other than DEGRADED. Short
predicate tests reject absent/foreign identity, old revisions, missing cache or
agent, previous/mismatched agent snapshots, disconnected phase and ERROR or
unspecified control state. These checks do not prove OS resolver application;
the DNS process/wire scenarios still require isolated CI execution.

The native host now includes the executable's commit and build date alongside its
version in BuildIdentity. Previously these fields were lost at host construction.
The local-host short test checks version/commit/date/platform/architecture through
Bootstrap, CLI runtime-info and CLI support-info. The process negotiation scenario's
build check now reads native runtime-info and checks the CI source commit, native
target and exact protocol/version/descriptor digest. Its old range-negotiation
probes are still pending replacement; this partial migration is not successful
execution of that guarded scenario or installed-artifact pairing evidence.

The negotiation scenario is now fully native: eleven missing, duplicate or
mismatched protocol/version/digest variants use a generated gRPC client without
the normal header injector over the real platform endpoint. GetRuntimeInfo must
remain available for diagnosis; GetStatus and a fully formed Disconnect must
return typed CONTRACT_MISMATCH. A valid client then checks request-ID lookup is
NOT_FOUND and enrollment, intent and host identity remain unchanged. Restart
requires a new instance with the same build/contract; valid Disconnect/Connect
must still complete without another registration. The retired HTTP range and
overlap probes are removed. Short tests compile this guarded process scenario;
platform execution and installed-artifact pairing remain CI work.

Logout retry and explicit local-forget process scenarios now use native operations
and status. Unavailable control must produce FAILED/REMOTE_CLEANUP_REQUIRED while
retaining enrollment. Exact replay after restart/control recovery must retain that
failed outcome; a fresh request ID retries and confirms remote/local cleanup with
one original registration and one deletion. Explicit local-forget still requires
CLI confirmation, reports SUCCEEDED/REMOTE_UNCONFIRMED with local removal, retains
signing trust, and replays the identical result after restart. Clean-state checks
require explicit StoredState, no node/network/credential/token/cache/peers, and
disconnected intent. No automatic reenrollment is allowed in the bounded observation
window. These replace the legacy cleanup scenarios; local short tests compile
them, while process/restart and remote-effect acceptance remain isolated CI work.

The operator trust-confirmation process scenario now uses native profile-scoped
identity, mutation metadata and operations. CLI confirmation requires the complete
origin/key/announcement tuple instead of the retired yes flag. Wrong origin,
missing key or malformed announcement are rejected without an operation; a
well-formed wrong key/announcement reaches the worker and must fail STALE_STATE.
Pinned identity remains unchanged and a named signed-map update must still arrive.
Unchanged trust completes as a no-op while disconnected, with exact operation
replay after restart and no extra registration. This migrates confirmation
boundaries, not successful signing-key rotation; those separate scenarios still
need migration. Short tests compile this guarded test; process acceptance remains CI.

The four map-signing rotation scenarios are now native as well: connected and
disconnected, each with and without interruption. Stale-key confirmation must
fail STALE_STATE without adopting trust. Valid confirmation uses the exact
announcement and a durable operation. Transient registration failure must remain
retryable; after process kill/restart, request-ID lookup must identify the same
RUNNING operation. Clearing the fault must allow worker recovery without a new
mutation even while disconnected. Completion requires Changed=true, retained
node/profile/intent and a verified map; exact replay after restart is immutable.
The final named map must reach a current error-free agent snapshot, with one
original registration and renewal of the same node. These scenarios are compiled,
not executed, by local short tests; successful rotation remains unverified until
isolated CI runs them on the required platforms.

The process events scenario now uses native WatchEvents and protobuf-JSON CLI
records. Opening snapshot, contiguous stream-local sequence, nondecreasing
revision, valid timestamp and stable instance identity are checked. Eight
independent subscriptions, cancellation without affecting another subscriber,
connection-intent events, CLI listening timeout and termination on host shutdown
remain covered. Reconnection requires sequence one and the new host instance.
Short cursor tests reject gaps, duplicates, instance/revision changes, missing
timestamps, repeated snapshots, failure and empty events without advancing the
cursor. The guarded process test itself still needs isolated CI execution;
queue overflow and authorization boundaries have separate component tests.

The unary CLI timeout process scenario now serves native bootstrap and generated
GetStatus over local HTTP/2 gRPC. Faults delay status response headers or send an
incomplete length-prefixed protobuf frame; neither can be mistaken for a complete
result. It checks the native request method/path and protocol/version/digest,
deadline failure with empty stdout, and successful protobuf-JSON status at the
same endpoint once the fault is cleared. The old HTTP JSON fixture is removed.
Short tests compile this guarded scenario; actual process deadlines and socket/
named-pipe recovery still require isolated CI execution.

The CLI event-failure fixture is now native HTTP/2 gRPC too: bootstrap succeeds,
then WatchEvents times out before its snapshot, ends empty, returns malformed
protobuf, or starts with status instead of snapshot. All must fail with empty
result output; repairing the same endpoint must yield exactly one snapshot and
a normal listening deadline. The CLI now also rejects a repeated snapshot after
opening, with a short regression test proving that invalid record is not emitted.
The process fixture is compiled locally, not platform-executed acceptance evidence.

Join-token rotation and expiry scenarios now observe native status for IPv4 and
IPv6, including retention of existing node credentials and cached-map traffic
across restart during a control outage. The reference peer's client endpoint is
obtained from GetDiagnostics tunnel inspection, bound to node/profile and at least
the observed map revision, with valid nonzero uint16 port and no inspection failure.
Overlay family selection rejects malformed/mapped addresses rather than inventing
a fallback. Existing wire echo counters, old-token denial and retry of the same
failed candidate with a replacement token remain checked. Short tests verify
address selection and compile the scenarios; actual TCP/UDP and restart acceptance
still require isolated CI on the supported platforms.

The complete two-client Linux namespace peer fixture now uses native IPC for
status, diagnostics and local mutations, including direct peer withdrawal/restore,
ACL protocol/port replacement, fresh-snapshot headless DNS, connection intent and
credential retirement. Tunnel observations require node/profile/map binding;
handshake/counters must identify the expected remote node, and withdrawal must
remove the inspected peer before traffic denial is evaluated. Applied-map gates
require current, error-free agent observations with matching map and peer counts.
Short regression tests reject foreign identities, stale/missing snapshots,
inconsistent peer counts, agent failures and unknown connection phase. These are
local predicate checks plus process-scenario compilation, not Linux networking
acceptance or evidence for other operating systems.

The two-client connection-intent subscenario now captures native node/profile
baselines and issues typed connect/disconnect mutations with fresh CAS metadata
and distinct request IDs, waiting for successful terminal operations. Explicit
desired intent and actual connection phase must survive restart. Disconnect denies
TCP, UDP and incoming overlay ping while receiver applications remain alive;
connect and a repeated connect restore traffic without reenrollment. During a
control outage, current agent failure and retained verified cache are observed
independently of traffic. Recovery requires a current error-free applied map.
This guarded Linux namespace scenario is compiled by local short tests, not
executed; two-client networking acceptance remains pending in CI.

The two-client credential-retirement subscenario now captures its own native
status baseline rather than accepting legacy status DTOs. Terminal cleanup and
restart must clear native enrollment/cache while preserving the profile. The
receiver must retain a current applied map and an inspected tunnel peer explicitly
matching the retired sender. Existing TCP/UDP sockets and fresh overlay exchanges
must be denied while the same receiver applications remain reachable by underlay;
no new registration request is allowed. The enclosing Linux namespace peer/policy
fixture also uses native observations. Local short tests only compile this guarded
scenario; real traffic/restart evidence needs CI.

The user-session expiry scenario now uses native status and profile/map-bound
tunnel diagnostics in both IP families. Backend-observed headless user RPC denial
must coexist with the same enrolled node, active profile and current applied map;
TCP and UDP probes must produce fresh reference-peer receive/echo counts. Agent
restart before reauthentication must preserve that separation, and subsequent
headless login and restart must not create another node registration. This is
not evidence for native browser-login UI or renewal acceptance. Local short tests
compile the guarded scenario; real session/traffic/restart execution belongs to CI.

The cached-map expiry scenario now uses native status and profile/map-bound
tunnel diagnostics for both IPv4 and IPv6. Each successful TCP and UDP probe
must increment reference-peer receive/echo counters. Expiry during an outage
must invalidate cached authority without removing the node credential or changing
the connected intent; both protocols must fail before and after agent restart.
Offline headless sync must reject expired authority, and a fresh signed map must
restore traffic for the same node. Local short tests compile this guarded process
scenario; expiry, restart and real traffic evidence remain per-platform CI work.

The ephemeral lifecycle scenario now uses native status and profile/map-bound
tunnel diagnostics. A current agent snapshot must expose the ephemeral node and
peer before traffic; a successful TCP probe must increment the reference peer's
receive/echo counters. After process death and provider revocation, native status
must explicitly clear enrollment/cache and ephemeral state, and a new TCP probe
must fail. The replacement job must receive a distinct ephemeral node credential.
This tests the client response to an explicit provider cleanup decision, not the
provider's absence-detection timer. Local short tests compile this guarded network
scenario; per-platform traffic and process-restart evidence remain CI work.

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
