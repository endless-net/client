# Native account network catalog

The agent configures ListNetworks using typed UserService ListNetworks for the
requested profile's active account and user session. This is not the retired IPC
callback that reported only the cached map's current network; that callback is
removed. No cached-map fallback is used. Profiles without account context return
NEEDS_ENROLLMENT; missing or rejected user sessions return NEEDS_LOGIN. Node
credentials alone do not authorize this user query.

The producer rechecks caller authorization and unchanged persistent configuration
after collection. Its provider receives only profile control origin, account ID
and session token (trusted in-process input, never a response). Cross-account,
duplicate or invalid IDs fail. The adapter reads at most five 200-entry backend
pages with cancellation and rejects malformed/empty/repeated continuations.
The native response is sorted by ID and uses caller/profile/instance/revision/
content-bound pagination. A selected network missing from the catalog is stale
state, not permission to pick a replacement. Failed reads return no partial catalog.

The pinned upstream Network message exposes ID/name only. CIDRs are not invented.
Account IDs come from the scoped request. Selection restrictions remain UNSUPPORTED
until the native switch provider exists; the whole network-selection capability
is not advertised. Native SelectNetwork
execution/rollback, per-network details and installed-agent acceptance remain open.

Short tests cover request authorization/account/page binding, deterministic native
pagination and changed-catalog rejection, immutable response ownership, missing
selection, caller denial, malformed provider records, cancellation and owner/config
changes during collection. Backend calls use controlled typed readers in these
tests, not production membership/permission evidence. `client-ui` owns combined
real-host catalog selection tests once the switch provider is implemented.

Backend error categories now distinguish user login, permission denial, missing
account/resource, rate/size limit, deadline/cancellation and temporary failure.
Native Failure details are reconstructed; arbitrary upstream text/details are
not forwarded. A query failure does not clear credentials or retry registration.
Table-driven producer tests verify the categories and absence of partial results,
retries and secret-bearing diagnostics.

`service_rpc_networks_transport_test.go` additionally exercises the generated
UserService client and handler over a local HTTP test server: exact account,
bearer authorization (never node credentials), opaque next-page token, successful
collection and second-page login/permission/temporary failure. This verifies the
typed backend adapter transport, not public gateway TLS or actual membership.
