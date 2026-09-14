package client

import (
	"reflect"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCStartupRecoveryProjectsRequestedIntentWithoutStarting(t *testing.T) {
	m, peer, _ := rpcPreferenceFixture(t)
	original := &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "user_connect", UpdatedAt: "2026-09-14T00:00:00Z"}
	if err := m.store.Update(func(cfg *Config) error { cfg.ConnectionIntent = original; cfg.CachedMap = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := NewConnectionIntentStore(m.store).InitializeRuntimeIntent(); err != nil {
		t.Fatal(err)
	}
	m.store = reopenRPCStoreFromDisk(t, m.store)
	before := m.store.Read()
	if before.ConnectionIntent.StartupRecovery == nil {
		t.Fatal("missing recovery checkpoint")
	}
	_, disconnected, err := NewConnectionIntentStore(m.store).Disconnected()
	if err != nil || !disconnected {
		t.Fatal("projection fixture bypassed runtime gate")
	}
	for _, caller := range []local.Peer{peer, {Identity: "observer"}} {
		snapshot, err := m.snapshotAs(caller, &ipc.BuildIdentity{})
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Status.UserDisconnected || snapshot.Status.GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_CONNECTED || snapshot.Status.ConnectionPhase == ipc.ConnectionPhase_CONNECTION_PHASE_CONNECTED {
			t.Fatal("requested intent confused with network effects")
		}
		sub, err := m.subscribe(caller, &ipc.BuildIdentity{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		event, err := sub.next(t.Context())
		m.unsubscribe(sub)
		if err != nil || event.GetSnapshot().GetStatus().GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_CONNECTED || event.GetSnapshot().GetStatus().UserDisconnected {
			t.Fatal("opening stream lost requested intent")
		}
	}
	projected := RequestedConnectionIntent(before)
	if !reflect.DeepEqual(projected, original) || projected.StartupRecovery != nil {
		t.Fatal("projection lost original timestamp or leaked checkpoint")
	}
	projected.DesiredState = ConnectionIntentDesiredDisconnected
	if !reflect.DeepEqual(before, m.store.Read()) {
		t.Fatal("read projection changed durable runtime gate")
	}
	if err := NewConnectionIntentStore(m.store).SetDisconnected("user_disconnect"); err != nil {
		t.Fatal(err)
	}
	after, err := m.snapshotAs(peer, &ipc.BuildIdentity{})
	if err != nil || !after.Status.UserDisconnected || after.Status.GetIntent().GetDesiredState() != ipc.DesiredState_DESIRED_STATE_DISCONNECTED {
		t.Fatal("new user disconnect did not supersede saved intent")
	}
}

func TestRequestedIntentRejectsUnboundRecovery(t *testing.T) {
	for _, mode := range []string{"owner", "profile", "node", "network", "credential", "token", "reason", "previous", "preference"} {
		t.Run(mode, func(t *testing.T) {
			m, _, _ := rpcPreferenceFixture(t)
			cfg := m.store.Read()
			cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "runtime_start_policy_unavailable", StartupRecovery: &clientRuntimeStartRecovery{Context: runtimeStartRecoveryContext(cfg), PreviousState: ConnectionIntentDesiredConnected}}
			switch mode {
			case "owner":
				cfg.LocalOwnerID = "other"
			case "profile":
				cfg.RPCState.ActiveProfileID = "other"
			case "node":
				cfg.NodeID = "other"
			case "network":
				cfg.NetworkID = "other"
			case "credential":
				cfg.NodeCredential = "other"
			case "token":
				cfg.Token = "other"
			case "reason":
				cfg.ConnectionIntent.Reason = "user_disconnect"
			case "previous":
				cfg.ConnectionIntent.StartupRecovery.PreviousState = ConnectionIntentDesiredDisconnected
			case "preference":
				profile := cfg.RPCState.Profiles[cfg.RPCState.ActiveProfileID]
				value := ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT
				profile.RuntimeStart = &value
				cfg.RPCState.Profiles[profile.ID] = profile
			}
			if got := RequestedConnectionIntent(cfg); got.DesiredState != ConnectionIntentDesiredDisconnected || got.StartupRecovery != nil {
				t.Fatal("old requested intent crossed changed context")
			}
		})
	}
}
