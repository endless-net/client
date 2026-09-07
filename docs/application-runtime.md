# Signed L3 application runtime

The WireGuard engine consumes effective application identities, source node/key
bindings, connector node/key bindings and leased destinations from the signed
network map. Client API owns validation; Coordinator owns compilation and
membership checks. The client never expands user or group selectors.

## Routing and discovery

Sources run on the client's supported operating systems. Connector discovery
and automatic IP forwarding/NAT currently run on Linux, using `ip`, `sysctl`,
`iptables` and `ip6tables`. A Windows or macOS connector does not publish route
leases. Use dedicated Linux connector nodes: once a node acts as an application
connector, its forwarded traffic requires an application grant, including when
it also has generic subnet advertisements.

The connector resolves exact domain names through its local resolver every
15 seconds. Resolution follows the OS resolver's CNAME behavior. Wildcards are
not accepted. Up to 64 unique A/AAAA destinations are reported through the
generated native protobuf `ConnectorService` client at the control API origin,
using the node credential. Redirects are disabled. Resolution failure or an
oversized answer reports an empty address set to withdraw the observation.
CIDR targets renew liveness without DNS lookup.

The requested observation lease is 60 seconds. This is an authorization lease,
not an assertion about the DNS record's TTL: the OS resolver does not expose
that TTL. Coordinator caps and signs the accepted expiry. Stopping the client
cancels the worker; loss of control connectivity cannot extend the lease.
Renewal can be idempotent, so a successful report does not always change the
map revision. Revocation is effective when the new signed map is applied, or
when the previous lease expires if updates cannot arrive.

On a source, active destinations are added to the selected connector's
WireGuard AllowedIPs and OS routes. The smallest live connector node ID wins
for an identical destination prefix. A later signed update changes the choice
after connector withdrawal. Existing connections need not survive failover.
The DNS proxy publishes signed A/AAAA answers with TTL zero for enabled exact
application domains. Connectors do not install those application DNS rules,
which prevents discovery from querying its own previously signed answers.

## Enforcement

A TUN wrapper checks both packet directions before packets cross between
WireGuard and the OS. Connector grants require an effective source overlay
address bound to its node/key, the authorized destination, and an unexpired
route and map. HTTP(S) origin targets permit their TCP port; domain and CIDR
targets permit IP traffic within their declared destination scope. URL paths,
queries and credentials are rejected by the producer's validation.

Every protected packet is checked, including packets of established flows.
Fragments, unsupported IPv6 extension headers and malformed protected packets
fail closed. Withdrawn destinations remain protected for the engine lifetime;
after 8192 distinct destinations, the filter conservatively protects all
destinations. Fresh engines deny forwarding that has neither a current
application grant nor an explicit generic subnet advertisement. Stale OS NAT
rules therefore do not grant application forwarding after a restart.

The filter's authorization is replaced before route changes. A failed route
apply may restore previous routing, but cannot restore revoked application
permissions. Linux conntrack and NAT do not bypass the TUN check. Derived peer
routes never replace the original signed map used for verification and DNS.

L3 does not isolate HTTP Host, SNI, URL paths or separate applications sharing
the same IP and permitted port. Grants for the same destination are unioned;
an IP grant can reach another virtual host there. Keep overlapping resources
on a compatible connector set. Reserved ranges and destinations overlapping
the overlay network are rejected. Private LAN destinations are allowed because
reaching them is the connector's purpose.

## Component validation and remaining acceptance

Short tests cover native protobuf reports and node authentication headers,
negative DNS withdrawal, signed DNS, route application to a running
wireguard-go device with an in-memory TUN, packet batches in both directions,
source/port denial, malformed packets, expiry, revocation and restart denial.
Vet and golangci-lint cover the local build. These checks do not execute
privileged Linux forwarding/NAT, prove live overlay reachability, or establish
production activation. Linux IPv4/IPv6 forwarding and source behavior on each
supported OS require owning-repository CI acceptance.

Producer sources: [Client API, main](https://github.com/endless-net/client-api/tree/main/clientapi),
[Coordinator, main](https://github.com/endless-net/coordinator/tree/main),
[Management, main](https://github.com/endless-net/management/tree/main).
The cross-service decision and evidence belong to
[architecture, main](https://github.com/endless-net/architecture/blob/main/docs/ru/application-runtime-design-gate.md).
