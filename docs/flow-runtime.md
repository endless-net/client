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
worker refreshes policy every 15 seconds; TUN observations check the expiration
on every packet. Changed revision, revocation, expiry, reconfiguration or shutdown
clears buffered metadata. Shutdown cancels and joins the worker before reusing
the collector, preventing stale policy responses from re-enabling collection.

Windows last at most ten seconds and are immutable once queued. The queue and
active aggregates share a 1024-window limit. Network I/O runs outside the TUN
path. Reports retain window ID and consent revision across retries, use five
second request deadlines and exponential backoff with jitter capped at 22.5
seconds. The worker tries configured HTTPS control origins; failed consent or
authorization stops collection. Plain HTTP is not used for flow metadata.

The pinned [`coordinatorapi/v1.20.0` contract](https://github.com/endless-net/coordinator/blob/main/proto/coordinator/v1/flow.proto)
owns RPC DTOs. Coordinator resolves account ownership; the Client sends only
node identity, consent revision and the flow window.

The queue is memory-only. Process termination loses unacknowledged windows;
expiry also discards pending windows by design. `WireGuardEngine.FlowLogStatus`
and structured `flow log runtime` records expose enabled state, active/pending
windows, discarded windows, packet drops split by unsupported input, capacity
and backward clock movement, report attempts/failures, acknowledged windows and
policy failures. Counters are cumulative for the engine lifetime and reset on
process restart. Status refresh also expires idle buffers without packet input.
Logs emit changed snapshots at most every 15 seconds and a final stopped snapshot;
they contain no IPs, node/window IDs, URLs, credentials or raw RPC errors.
Durable restart handling, user-facing diagnostics integration and central metrics
remain required follow-up. OS/kernel dataplanes outside this wireguard-go TUN
wrapper are not claimed to produce flow logs.

An encrypted spool primitive is component-tested but is not yet connected to
the worker or agent configuration. It atomically saves sealed protobuf windows,
preserves window IDs/revision/lease across reopening, binds AES-GCM ciphertext
to the producer scope and credential, bounds reads to 1 MiB, rejects tampering
and deletes expired ciphertext. Its load result is quarantined data: future
worker integration must obtain fresh matching consent before importing/sending,
persist before the first send, persist acknowledgements, purge on revocation
and account for storage failures. The running queue remains memory-only until
that integration is implemented and verified.

Tests cover default-off behavior, aggregation, bounded capacity, immutable retry,
revision/expiry cleanup, and a packet passed through the actual TUN wrapper and
sent using TLS protobuf after a temporary receiver failure. Validation uses only
the repository-authorized goimports, vet, lint and short-test commands.
