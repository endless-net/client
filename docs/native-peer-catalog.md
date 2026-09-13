# Native peer catalog

The agent now supplies `ListPeers` directly through client.v0. The active profile's
cached map must pass signature and boundary verification, and the engine must
provide nonblocking path observations for that exact network, node and map
revision. A busy engine, unapplied map or invalid path observation returns a typed
failure, not a cached/legacy fallback. Inactive profiles remain unsupported.

Peer identity, hostname and overlay host addresses come from the verified map;
path candidates and selection use the same native mapper as diagnostics. CURRENT
means the map matches the applied path-monitor context, not that every peer is
reachable. Missing individual observations do not manufacture reachability.

Owner authorization is checked before collection and again afterward. A changed
profile/configuration invalidates the result. Search matches ID, hostname or
overlay address, case-insensitively. Results are sorted by ID; pagination is bound
to caller, profile, instance, configuration revision, normalized search and the
complete filtered result. Changed observations require a fresh query, not silent
continuation. Queries and response sizes are bounded; responses own cloned data.

Short tests cover search, pagination, changed observations, duplicate identities,
owner/config/profile changes, cancellation, invalid queries, map tampering,
unavailable engine observations and foreign-peer paths. These are controlled
producer tests, not installed-agent or cross-platform acceptance. The full peer
capability is not advertised: peer probe actions and complete UI/system scenario
validation remain follow-up work in client and client-ui. No proto or version
change is required for this handler.
