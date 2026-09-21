package client

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNativeExitResourceAdmissionAppliesAndRestoresPacketChoice(t *testing.T) {
	r, n, m := nativeExitPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.NetworkPreferenceChange = nil
		cfg.RPCState.Operations = map[string]clientRPCOperationRecord{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	initial := m.store.Read()
	owner := local.Peer{Identity: initial.LocalOwnerID}
	profile := &ipc.ProfileRef{ProfileId: initial.RPCState.ActiveProfileID}
	id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, initial.CachedMap.Peers[0].ID)
	guard := n.engine.exitGuard
	packet := applicationTCPPacket("100.64.0.1", "100.64.0.2", 50000, 443)
	driver := ClientRPCProfileDriver{Lock: r.lock, Start: func(ctx context.Context, cfg Config) error {
		return r.ApplyPreferenceCandidateLocked(ctx, r.lock, cfg)
	}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		t.Fatal("valid resource transition stopped the exit")
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	for _, enabled := range []bool{false, true} {
		request := &ipc.SetResourceEnabledRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ResourceId: id, Enabled: enabled}
		accepted, err := m.setResourceEnabledAs(owner, request)
		if err != nil {
			t.Fatal(err)
		}
		if n.engine.resourceFilter.allows(packet, false, time.Now()) == enabled {
			t.Fatal("admission changed the packet filter before application")
		}
		if err := m.ReconcileNetworkPreferences(t.Context(), driver); err != nil {
			t.Fatal(err)
		}
		current := m.store.Read()
		deadline := time.Now().Add(time.Second)
		for !n.engine.TryResourceEnforcement(current, time.Now()) {
			if time.Now().After(deadline) {
				t.Fatal("committed resource enforcement was not observed")
			}
			time.Sleep(time.Millisecond)
		}
		value, exists := current.ResourcePreferences[id]
		if !exists || value != enabled || current.RPCState.NetworkPreferenceChange != nil || n.engine.exitGuard != guard || !reflect.DeepEqual(initial.ExitSelection, current.ExitSelection) || n.engine.resourceFilter.allows(packet, false, time.Now()) != enabled {
			t.Fatal("resource choice did not take effect independently of saved exit")
		}
		replayed, err := m.setResourceEnabledAs(owner, request)
		if err != nil || replayed.Id != accepted.Id || replayed.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
			t.Fatal("resource transition did not retain successful replay", err)
		}
		r.lock.Lock()
		err = n.maintain(t.Context(), current)
		r.lock.Unlock()
		if err != nil {
			t.Fatal("resource transition invalidated saved exit", err)
		}
	}
}
