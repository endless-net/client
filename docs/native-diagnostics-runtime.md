# Native diagnostics runtime snapshot

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
