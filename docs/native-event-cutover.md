# Native agent event cutover

The agent no longer contains the retired HTTP IPC hello/status polling producer
(`streamAgentIPCEvents`, its metadata/error envelope constructors or the Events
callback in the old handler collection). It serves native WatchEvents through
`startAgentRPC`. Remaining legacy status/mutation/diagnostic handlers and the
ipc/v2 module are still pending removal; this is not whole-client cutover evidence.

The old `TestAgentIPCEventsStreamSendsHelloAndStatus` and its capturing HTTP event
writer are removed. Native replacements in `service_rpc_events_test.go` cover
snapshot-first sequence/revision, initial owner claim and refreshed role, typed
operation and domain invalidation events, fresh sequence on reconnection,
observer filtering, overflow and cancellation. The authenticated local transport
test in `service_rpc_transport_test.go` exercises those producer events alongside
actual native mutations, not a canned hello/status fixture.

Additional regression tests cover cancellation with an already queued snapshot
and owner revocation while private events are queued or a send is active. After
revocation is published, the old stream terminates with typed OWNER_REQUIRED;
it must not drain old owner-visible events. An active send is aborted via the
transport cancellation hook. Reattachment begins at sequence 1 with the current
observer-filtered snapshot. Initial observer-to-owner claims continue normally.
Bytes already delivered before revocation cannot be recalled.

These are short local tests. Installed-service lifecycle, OS transport behavior
on each supported platform, and release/system acceptance remain separate work.
`client-ui` owns verification of journal retention and fresh observer bootstrap
after permission-driven stream termination; do not infer that from producer tests.
