# Machine sharing packet enforcement

The Relay bridge sends the destination network ID from the peer map (an omitted
peer network denotes the credential's local network). Incoming frames must
identify both the expected peer and its network before any datagram reaches
WireGuard. A matching node ID from a different network is discarded.

The encrypted engine test now runs the same scenario through direct UDP and
the product TLS Relay dataplane: recipient-only TCP/UDP/ICMP initiation,
responses, lease expiry, renewed access, actual WireGuard key rotation and
withdrawal. Relay authorization in this test is a fixture. These additions
require the non-short CI result; they do not prove the full Management/TOTP
and Coordinator authorization chain. Earlier evidence below retains its
original, narrower scope.

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
Flow state cannot outlive the lease under which it was accepted. Shortening a
lease also caps existing state; renewing after expiry requires a fresh handshake
and cannot resurrect an expired established TCP flow.
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

The Linux CI job now runs
`TestSharingEncryptedWireGuardDirectionAndWithdrawal` outside short mode. Two
real WireGuard engines exchange packets over loopback UDP using channel TUNs and
signed sharing maps. It establishes recipient TCP SYN/SYN-ACK/ACK and reply
traffic before probing reverse NEW and an unauthorized destination port, then
applies signed withdrawal and checks established traffic is blocked in both
directions. Router changes use the test router; this is not OS routing or Relay
acceptance. At Client `f185975`, this direction/withdrawal scenario passed in
[Management release job 101783890469](https://github.com/endless-net/management/actions/runs/34135020494/job/101783890469)
on 7 September 2026. That job checks out the exact Client revision and uses
Management's existing private-module access. The log explicitly records the
encrypted test passing; it does not establish the whole release outcome.

The expiry extension shortens the signed lease, proves traffic still passes,
waits for expiry without removing peers or keys, and requires both directions
to stop. A new signed lease must restore a fresh TCP handshake on the same
transport before the existing withdrawal checks. Local short tests skip it.
Its first run at `b4ae115` failed on the
fresh handshake after renewal in
[job 101790199433](https://github.com/endless-net/management/actions/runs/34137001624/job/101790199433).
The filter retained established flow state past the old lease, rejecting the
new SYN. The fix at `97c7c54` passed both the component regression and the full
encrypted scenario in
[job 101792515111](https://github.com/endless-net/management/actions/runs/34137735475/job/101792515111)
on 7 September 2026 (explicit PASS, 5.07 seconds). This verifies expiry and fresh
handshake recovery with signed fixture maps, not backend map delivery or Relay.

The next encrypted extension exercises UDP and ICMP echo on the same live
engines: unsolicited replies deny, recipient requests and matched replies pass,
expiry blocks both directions, renewal requires fresh request state and
withdrawal blocks both again. At `32164b1`,
[job 101798673248](https://github.com/endless-net/management/actions/runs/34139686735/job/101798673248)
passed this expanded encrypted scenario on 7 September 2026.

`TestSharingBackendMapConsumer` accepts CI-supplied Coordinator base/delta
evidence and invokes the production CLI/agent cache consumer. It checks grant,
withdrawal, idempotent cache replay and unchanged cache after signature tampering.
It skips local short runs. The unchanged fixture captured by Coordinator run
34140193587 is checked in with its provenance and SHA-256 under
`cmd/endlessnet-client/testdata/`. At `9a5c44b`,
[job 101801802273](https://github.com/endless-net/management/actions/runs/34140701613/job/101801802273)
explicitly passed the consumer replay and the separate encrypted test on
7 September 2026. This does not yet establish engine configuration from backend events. Recorded events are
validated at their Coordinator-observed timestamps through the same cache helper;
normal runtime still supplies the current time. Historical replay does not claim
that a captured lease remains valid at the later CI execution time.

The first encrypted-test CI attempt at `890236a` did not execute the scenario:
[Linux job 101766343601](https://github.com/endless-net/client/actions/runs/34129622991/job/101766343601)
failed downloading private Coordinator and Management Go SDKs during vet. The
test workflow now requires the repository secret `PRIVATE_MODULES_READ_TOKEN`,
sets `GOPRIVATE=github.com/endless-net/*` and configures Git's credential helper
through GitHub CLI before Go setup. Use a read-only credential scoped to the
required private module repositories, currently Coordinator and Management;
ordinary repository `GITHUB_TOKEN` cannot read those separate repositories.
The secret was absent when checked on 7 September 2026. A trusted CI run after
configuration is required; fork PRs without secrets cannot run these downloads.
The separate Management release job above supplies the first encrypted result
without configuring this missing Client secret. Client release workflows still
need private-module access before release
validation; the change here configures the Test workflow only.
