# Signed service discovery

The WireGuard runtime installs its DNS proxy for each service DNS zone in the
current signed map. A/AAAA answers resolve approved host identities to their
overlay addresses. SRV queries use `_<service-name>._<protocol>.<service-dns-name>`;
each target is `<node-id-dns-label>.<service-dns-name>`. Answers use TTL zero so
DNS caches do not extend host authorization. UDP truncation supports TCP retry.

Every catalog query validates the map and verifies its signature against the
configured trust bundle, including expiry. An unknown host, changed WireGuard
key, missing approval or unknown name within a service zone never falls through
to upstream DNS. Map replacement refreshes the resolver; local runtime clones
detach applications, services and host bindings from caller-owned memory.

Host approval controls service advertisement and discovery. General IP/port
connectivity continues to require the network's routing and ACL policy; a DNS
answer is not a separate traffic authorization. Discovery does not scan host
ports, collect arbitrary endpoints, or assert application health.

The producer contracts are
[Client API `main`](https://github.com/endless-net/client-api/tree/main/clientapi)
and [Coordinator `main`](https://github.com/endless-net/coordinator/tree/main).
[Management `main`](https://github.com/endless-net/management/tree/main) owns the
manual approval decision and its account/role authorization. Coordinator
re-evaluates node identity, eligibility and live state on map rebuild.

Component checks cover signed A/AAAA/SRV discovery, a running UDP resolver,
runtime router configuration, signature tampering, expiry, untrusted maps,
revocation and detached cloning. They do not establish live overlay reachability
or production activation. Application connector transport remains a design gate;
the application catalog is foundation, not an implemented application proxy.
