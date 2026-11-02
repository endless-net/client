//go:build windows && integration

package clientapp

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
)

// This exercises the real Fx, authenticated pipe, DPAPI Store, command clock and
// connection worker. Only the per-generation native owner is injected: it does
// not establish a real tunnel or mutate network configuration.
func TestWindowsRuntimeConnectionCommandsReachMandatoryWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("separate isolated DPAPI/pipe composition fixture; native owner is injected")
	}
	c := stateFixtureConfig(t)
	original := seedState(t, c)
	build, digest := runtimeMetadata(t)
	r, err := NewWindows(c, build, digest, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	h := &workerHarness{created: make(chan *workerSession, 4)}
	h.setup = func(s *workerSession) {
		s.local = connectivity.LocalObservation{Known: true, Applied: true, Permission: connectivity.Permission{NewFlows: true, ExistingFlows: true}, DirectPaths: 1, MapRevision: 7}
	}
	r.connection.factory = h.factory
	if err := r.App.Start(budget(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.App.Stop(budget(t)); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.state.Store().Update(ctx, original.Revision, func(next *state.State) error {
		p := next.Profiles[c.DefaultProfileID]
		p.Attempt.Phase = enrollment.Registered
		p.Attempt.AccountID, p.Attempt.NetworkID, p.Attempt.NodeID = "account", "network", "node"
		p.Attempt.CredentialEnds = time.Now().Add(time.Hour)
		p.Secrets.RegistrationID = p.Attempt.RequestID
		p.Secrets.RegistrationRequest = "fixture exact registration request"
		p.Secrets.NodeCredential = "enc_fixture"
		next.Profiles[c.DefaultProfileID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	pipe, err := localipc.Connect(ctx, r.PipeConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pipe.Close() })
	client := clientv0.NewClientServiceClient(pipe)
	before, err := r.state.Store().Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mutation := func(revision uint64) *clientv0.MutationContext {
		return &clientv0.MutationContext{ExpectedInstanceId: r.pipe.InstanceID, ExpectedRevision: revision}
	}
	if _, err := client.Connect(ctx, &clientv0.ConnectRequest{Mutation: mutation(before.Revision)}); err != nil {
		t.Fatal("composed IPC did not reach connection worker", err)
	}
	owner := awaitWorkerSession(t, h)
	status, err := client.GetStatus(ctx, &clientv0.GetStatusRequest{})
	if err != nil || status.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_CONNECTED || status.Status.Agent.GetDirectPathCount() != 1 || status.Status.MapRevision != 7 {
		t.Fatal("runtime did not project injected native observation", status, err)
	}
	watchContext, stopWatch := context.WithCancel(ctx)
	watch, err := client.WatchEvents(watchContext, &clientv0.WatchEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := watch.Recv()
	stopWatch()
	if err != nil || first.Sequence != 1 || first.GetSnapshot().GetStatus().GetServiceState() != clientv0.ServiceState_SERVICE_STATE_CONNECTED || first.Metadata.InstanceId != r.pipe.InstanceID {
		t.Fatal("real IPC event stream missed current initial snapshot", first, err)
	}
	call := <-owner.connectCalls
	after, err := r.state.Store().Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := after.Profiles[c.DefaultProfileID]
	// The context deadline is reconstructed from conservative native remaining
	// time; the immutable operation receipt, rather than nanosecond equality of
	// two wall-clock conversions, enforces the exact accepted budget.
	if call.restore || owner.generation != 1 || p.Connection == nil || p.Connection.Request.ClockEpoch != r.pipe.InstanceID || call.deadline.After(p.Connection.Deadline.Add(time.Millisecond)) || !bytes.Equal(p.Keys.WireGuardPrivate, original.Profiles[c.DefaultProfileID].Keys.WireGuardPrivate) {
		t.Fatal("runtime replaced command clock, generation, deadline or identity")
	}
	if err := pipe.Close(); err != nil {
		t.Fatal(err)
	}
	if view := r.connection.Observe(); view.Phase != ConnectionConnected || owner.lifetime.Err() != nil {
		t.Fatal("IPC/UI closure disconnected daemon-owned intent")
	}
	second, err := localipc.Connect(ctx, r.PipeConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	if _, err := clientv0.NewClientServiceClient(second).Disconnect(ctx, &clientv0.DisconnectRequest{Mutation: mutation(after.Revision)}); err != nil {
		t.Fatal("composed Disconnect did not confirm owner cleanup", err)
	}
	view := r.connection.Observe()
	if view.Phase != ConnectionDisconnected || view.Generation != 2 || view.CleanupPending || owner.lifetime.Err() == nil {
		t.Fatal("composed Disconnect left old generation alive or cleanup unknown")
	}
	status, err = clientv0.NewClientServiceClient(second).GetStatus(ctx, &clientv0.GetStatusRequest{})
	if err != nil || status.Status.ServiceState != clientv0.ServiceState_SERVICE_STATE_DISCONNECTED || status.Status.Intent.DesiredState != clientv0.DesiredState_DESIRED_STATE_DISCONNECTED || status.Status.ConnectionPhase != clientv0.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED {
		t.Fatal("confirmed cleanup missing from composed IPC snapshot", status, err)
	}
}
