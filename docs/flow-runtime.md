# Flow collection runtime

Status on 2026-09-07: in-process producer and retry loop implemented with
component tests. This is not production activation or full system acceptance.

The wireguard-go TUN wrapper observes inbound and outbound IP packets after
application access evaluation. Allowed traffic is labelled `observed`; it does
not claim final WireGuard delivery or policy enforcement. Application rejections
are labelled `deny`. Records contain source/destination IP, destination port,
protocol, counters and timestamps, never payload, source port or credentials.
The existing parser excludes fragments and IPv6 extension headers; unsupported
packets are counted as drops. No protocol guessing is performed for them.

Collection is disabled until an HTTPS protobuf policy response grants it. The
current Client rejects policies expiring more than one minute in the future.
The published protobuf describes the consent interval without that maximum;
compatibility with longer grants remains an explicit contract gap. Native tests
use a 50-second grant and do not qualify longer grants.
The worker refreshes policy every 15 seconds; TUN observations check the expiration
on every packet. Changed revision, revocation, expiry, reconfiguration or shutdown
clears buffered metadata. Shutdown cancels and joins the worker before reusing
the collector, preventing stale policy responses from re-enabling collection.

Windows last at most ten seconds and are immutable once queued. The queue and
active aggregates share a 1024-window limit. Network I/O runs outside the TUN
path. Reports retain window ID and consent revision across retries, use five
second request deadlines and exponential backoff with jitter capped at 22.5
seconds. The worker tries configured HTTPS control origins; failed consent or
authorization stops collection. Plain HTTP is not used for flow metadata.

The producer-owned [Client API contract, main](https://github.com/endless-net/client-api/blob/main/clientapi/proto/client/v1/client.proto)
owns RPC DTOs. Coordinator resolves account ownership; the Client sends only
node identity, consent revision and the flow window.

The agent stores sealed windows in `<config path>.flow-queue`, under its existing
exclusive agent lock. AES-GCM binds ciphertext to the producer scope and current
node credential. The disk queue is bounded to 1 MiB and 1024 windows; snapshots
are atomically written and synced before the first report. An ACK updates the
queue atomically. Unchanged snapshots do not rewrite the file. A crash after
remote commit but before local ACK can replay the same immutable ID/content;
Coordinator and Management enforce idempotency and content conflicts.

After restart, loaded windows are quarantined until fresh policy confirms the
same revision and collection start boundary. Restart never extends the stored
lease before this check. Revocation, revision changes, expiry while running,
normal shutdown and disconnect erase queued data. When the process is stopped,
expired ciphertext is deleted on the next startup. Credential/scope changes and
corruption fail closed. Storage errors stop the worker and surface in status;
no uncheckpointed sealed window is sent. Active windows not yet sealed (up to ten
seconds) can still be lost on an abrupt crash. Embedded engines without an
explicit FlowSpoolPath use memory-only buffering; the shipped agent sets it.

`WireGuardEngine.FlowLogStatus` and structured `flow log runtime` records expose
queue sizes, packet-drop categories, discarded windows, report attempts/failures,
ACKs, policy failures, storage failures, corrupt spools and restored windows.
Counters reset on process restart. Logs emit changed snapshots at most every
15 seconds and a final stopped snapshot, without IPs, identifiers, credentials,
URLs or raw errors. User-facing diagnostics and central metrics remain follow-up.
OS/kernel dataplanes outside this wireguard-go TUN wrapper are not covered.

Tests cover default-off behavior, aggregation, bounded capacity, immutable retry,
revision/expiry cleanup, and a packet passed through the actual TUN wrapper and
sent using TLS protobuf after a temporary receiver failure. Validation uses only
the repository-authorized goimports, vet, lint and short-test commands.

The CI-only `TestControlPlaneNativeFlowConsent` now drives real IPv4 UDP traffic
through the native Client and a reference WireGuard peer. It checks default-off,
consented metadata, immutable retry after an accepted-window acknowledgement
failure, live revocation and renewed consent over HTTPS protobuf.
Hosted qualification is pending; see the [coverage ledger](headless-test-coverage.md).
This increment does not establish crash-spool replay, producer idempotency,
IPv6, denied-flow reporting or production activation.
