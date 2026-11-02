//go:build windows && integration

package localapi

import (
	"bytes"
	"context"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWindowsConnectionIPCIntentACKLossAndRuntimeRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("explicit authenticated pipe and protected intent fixture; no native network mutations")
	}
	s, fault, sid := nativeStore(t)
	now := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	cfg := config(&now)
	var err error
	cfg.RuntimeID, err = randomID()
	if err != nil {
		t.Fatal(err)
	}
	runner := &connectionRunner{store: s}
	cfg.Connections = runner
	cfg.ConnectionClock = connectionFixtureClock{epoch: func() string { return cfg.RuntimeID }, now: cfg.Now}
	c, err := New(s, &testRunner{store: s}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	client, stop := nativeClient(t, c, sid)
	defer func() { stop() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mutation := func(revision uint64) *clientv0.MutationContext {
		return &clientv0.MutationContext{ExpectedInstanceId: cfg.RuntimeID, ExpectedRevision: revision}
	}
	enroll := request(1)
	enroll.Mutation = mutation(1)
	if _, err := client.Enroll(ctx, enroll); err != nil {
		t.Fatal(err)
	}
	// Seed a local registered fixture; this does not claim remote authorization
	// or a native data plane. The injected worker checks durable dispatch only.
	if err := s.Update(ctx, 2, func(next *state.State) error {
		p := next.Profiles[defaultID]
		p.Attempt.Phase = enrollment.Registered
		p.Attempt.AccountID, p.Attempt.NetworkID, p.Attempt.NodeID = "account", "network", "node"
		p.Attempt.CredentialEnds = now.Add(time.Hour)
		p.Secrets.RegistrationID = requestID
		p.Secrets.RegistrationRequest = "fixture exact registration request"
		p.Secrets.NodeCredential = "enc_fixture"
		next.Profiles[defaultID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fault.lose.Store(true)
	connect := &clientv0.ConnectRequest{Mutation: mutation(3), Async: true}
	if _, err := client.Connect(ctx, connect); status.Code(err) != codes.Unavailable {
		t.Fatal("lost protected commit ACK claimed success", err)
	}
	if _, err := client.Connect(ctx, connect); status.Code(err) != codes.Aborted {
		t.Fatal("stale request dispatched", err)
	}
	runner.mu.Lock()
	calls := len(runner.calls)
	runner.mu.Unlock()
	if calls != 0 {
		t.Fatal("uncertain commit dispatched")
	}
	observed, err := client.GetStatus(ctx, &clientv0.GetStatusRequest{})
	if err != nil || observed.Status.Metadata.Revision != 4 || observed.Status.Intent.DesiredState != clientv0.DesiredState_DESIRED_STATE_CONNECTED || observed.Status.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED {
		t.Fatal("real IPC lost durable intent or claimed OS state", err)
	}
	accepted, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original := *accepted.Profiles[defaultID].Connection
	if original.Request.InstanceID != cfg.RuntimeID || original.Request.Revision != 3 {
		t.Fatal("command not bound to authenticated runtime CAS")
	}
	if _, err := client.Connect(ctx, &clientv0.ConnectRequest{Mutation: mutation(4)}); err != nil {
		t.Fatal(err)
	}
	oldMutation := mutation(4)
	stop()
	// Defer remains valid even if construction below fails.
	stop = func() {}
	cfg.RuntimeID, err = randomID()
	if err != nil {
		t.Fatal(err)
	}
	c, err = New(s, &testRunner{store: s}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	client, stop = nativeClient(t, c, sid)
	if _, err := client.Disconnect(ctx, &clientv0.DisconnectRequest{Mutation: oldMutation}); status.Code(err) != codes.Aborted {
		t.Fatal("old runtime command accepted", err)
	}
	if _, err := client.Disconnect(ctx, &clientv0.DisconnectRequest{Mutation: mutation(4)}); err != nil {
		t.Fatal(err)
	}
	after, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := after.Profiles[defaultID]
	if after.OwnerSID != sid || after.Revision != 5 || p.Connection.Intent != state.IntentDisconnected || p.Connection.Generation != 2 || p.Connection.Request.InstanceID != cfg.RuntimeID || !p.Connection.AcceptedAt.Equal(original.AcceptedAt) || !bytes.Equal(p.Keys.IdentityPrivate, before.Profiles[defaultID].Keys.IdentityPrivate) || !bytes.Equal(p.Keys.WireGuardPrivate, before.Profiles[defaultID].Keys.WireGuardPrivate) || p.Attempt.Phase != enrollment.Registered {
		t.Fatal("restart/disconnect reset identity, authority or generation")
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) != 2 || !runner.calls[0].wait || runner.calls[0].generation != 1 || runner.calls[1].intent != state.IntentDisconnected || runner.calls[1].generation != 2 {
		t.Fatal("real IPC did not preserve accepted command semantics")
	}
}
