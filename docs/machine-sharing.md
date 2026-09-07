# Machine sharing packet enforcement

The managed WireGuard engine pins ClientAPI `v1.11.0` and consumes signed
`SharePeerGrant` permissions. The TUN wrapper checks sharing on plaintext reads
and writes, independently of direct UDP or Relay transport. Exact endpoint
keys, networks, host prefixes, rights and grant leases are validated before
configuration. The map must authenticate to the current node's signing trust.

Only the recipient can start a TCP handshake, UDP exchange, or ICMP echo.
Reverse packets require matching flow state. TCP checks SYN/SYN-ACK/ACK order
and rejects reverse SYN even when a flow already exists. UDP and echo use a
30-second idle window; TCP uses two minutes. Every packet also checks its
current grant expiry, so established traffic stops without a stream update.
Fragmented packets, IPv6 extension headers and non-echo ICMP are currently
denied for shared hosts; related ICMP error support remains follow-up work.

Protected remote hosts remain denied after withdrawal for the engine lifetime.
New routes and keys install while sharing is suspended. A failed apply leaves
sharing withdrawn even when the engine restores its previous device state.
Successful renewal can preserve flow state only for the same grant identity,
revision and rights. Changed keys, addresses, rights or revision discard that
state. Static WireGuard configuration export refuses sharing maps because it
cannot supply this enforcement.

Local short tests cover packet allow/deny, lease boundaries, withdrawal,
revision changes, dual-stack hosts, forwarding denial, both TUN batch
directions and configuration suspension. Vet and lint also pass. These tests
do not establish a real encrypted two-client or Relay session. Coordinator
grant resolution/projection, Management reconciliation, Relay upstream
authorization and component CI network acceptance remain necessary before
claiming end-to-end ShareMachine enforcement.
