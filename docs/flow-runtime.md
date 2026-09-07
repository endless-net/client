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
expiry also discards pending windows by design. Packet drops and discarded-window
counters are currently local to the collector and are not yet surfaced through
diagnostics/metrics. Durable restart handling and observable loss reporting
remain required follow-up. OS/kernel dataplanes outside this wireguard-go TUN
wrapper are not claimed to produce flow logs.

Tests cover default-off behavior, aggregation, bounded capacity, immutable retry,
revision/expiry cleanup, and a packet passed through the actual TUN wrapper and
sent using TLS protobuf after a temporary receiver failure. Validation uses only
the repository-authorized goimports, vet, lint and short-test commands.
