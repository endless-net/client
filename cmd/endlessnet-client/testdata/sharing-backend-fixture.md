# Captured Coordinator sharing events

`sharing-backend-fixture.json` is copied unchanged from the `sharing-ydb-evidence`
artifact of [Coordinator run 34140193587](https://github.com/endless-net/coordinator/actions/runs/34140193587),
at Coordinator main commit `dcb597f83895b921aa2ac10564842ee05c01fc57`.
The successful isolated YDB test used real lifecycle commits, Topic delivery and
the map-stream handler to produce the base and grant/withdrawal deltas.

SHA-256: `42df8c3b7f1ff006841477a05559550140cc149496f97f3aa845f084938082bb`.
The two observation times are 2026-09-07T15:52:17.80649821Z and
2026-09-07T15:52:17.904394731Z. The test verifies signatures and leases at these
recorded times. It does not re-sign events or assert that their leases are still
valid now. The trust contains a test public key, not a private key or production
credential.

The Client cache consumer checks grant, withdrawal, idempotent replay and
rejection of tampered signatures. This replay does not prove live connectivity,
WireGuard configuration from these events, Billing/Identity integration or
production acceptance. Never regenerate the file locally as a substitute for
captured backend output; replacement evidence must identify its producer run.
