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
collection; engine/interface enumeration itself remains synchronous. No mutation
lock is held while querying the provider.

Tunnel/interface error text is replaced with fixed typed failures. Interface
collections, numeric ranges and final response size are bounded; returned slices
are copied. The RPC returns metadata/status from the same native snapshot and
does not infer connectivity from interface presence.

This is a **partial runtime implementation**. Routes, route conflicts, default
route presence, DNS and peer-map joins are not collected; `truncated=true` and an
explicit unsupported-section failure prevent interpreting their empty/default
fields as a complete clean report. Tunnel peer identities require a verified map
join rather than a guess from endpoint/key strings. Bundle creation/download and
full OS/runtime diagnostic acceptance remain open. The whole DIAGNOSTICS
capability remains unadvertised until these required providers are ready.

Short tests verify owner admission, active-profile isolation, coherent metadata,
raw-error suppression, copied interface data, log inclusion, partial status,
provider failure, cancellation, ownership/revision changes and size/range rejection.
They do not prove installed-service or per-OS inspection behavior. `client-ui`
owns combined real-host rendering and bundle/export tests after capability readiness.
