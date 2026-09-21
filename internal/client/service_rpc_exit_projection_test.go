package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitReadProjectionTracksWorkerAndAdmission(t *testing.T) {
	for _, scenario := range []string{"ready", "absent", "stopped", "unsupported", "pending", "offline_clear", "stale_scope"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			trusted, networkMap, key := signedApplicationFixture(t, false)
			networkMap.Peers[0].AllowedIPs = append(networkMap.Peers[0].AllowedIPs, "0.0.0.0/0")
			host := api.ServiceHost{NodeID: networkMap.Peers[0].ID, PublicKey: networkMap.Peers[0].PublicKey}
			networkMap.Network.ClientPolicy = &api.ClientPolicy{ExitNodes: []api.ExitNodeGrant{{ID: "exit", Name: "Exit", Host: host, ExpiresAt: time.Now().Add(time.Minute), AllowedFamilyModes: []api.ExitFamilyMode{api.ExitFamilyIPv4Only}, AllowedLANAccess: []api.ExitLANAccess{api.ExitLANBlock}}}}
			resignApplicationMap(t, &networkMap, key)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NetworkID = networkMap.Node.ID, networkMap.Network.ID
				cfg.MapRevision, cfg.MapGlobalRevision = networkMap.Network.Revision, networkMap.Revision.Global
				cfg.CachedMap, cfg.MapSigningTrust = &networkMap, trusted.MapSigningTrust
				if scenario == "pending" {
					cfg.RPCState.ExitChange = &clientRPCExitChange{ProfileID: profile.ProfileId}
				}
				if scenario == "offline_clear" {
					cfg.CachedMap = nil
				}
				if scenario == "stale_scope" {
					cfg.RPCState.ExitProtection = &clientRPCExitProtection{ProfileID: "other"}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario != "absent" {
				s.exitWorker = &clientRPCProfileWorker{ctx: ctx}
				s.exitModes = []clientRPCExitMode{{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}, {Family: api.ExitFamilyDualStack, LAN: api.ExitLANAllow}}
			}
			if scenario == "stopped" {
				cancel()
			}
			if scenario == "unsupported" {
				s.exitModes = []clientRPCExitMode{{Family: api.ExitFamilyIPv6Only, LAN: api.ExitLANBlock}}
			}
			before := clonePersistentConfig(m.store.Read())
			status, err := s.exitNodeAs(t.Context(), owner, &ipc.GetExitNodeRequest{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			wantControl := scenario == "ready" || scenario == "unsupported" || scenario == "offline_clear"
			if (status.Control.Mutation.Availability == ipc.Availability_AVAILABILITY_AVAILABLE) != wantControl {
				t.Fatalf("control availability does not match admission: %v", status.Control)
			}
			if status.ApplyState == ipc.ApplyState_APPLY_STATE_APPLIED || status.EffectiveExitNodeId != nil {
				t.Fatal("worker readiness became applied evidence")
			}
			catalog, err := s.exitNodesAs(t.Context(), owner, &ipc.ListExitNodesRequest{Profile: profile})
			if scenario == "offline_clear" {
				if err == nil {
					t.Fatal("offline catalog accepted")
				}
			} else {
				if err != nil || len(catalog.ExitNodes) != 1 {
					t.Fatal("catalog unavailable", err)
				}
				item := catalog.ExitNodes[0]
				if (item.Selection.Availability == ipc.Availability_AVAILABILITY_AVAILABLE) != (scenario == "ready") {
					t.Fatalf("invalid selection projection: %v", item)
				}
				if scenario == "ready" {
					if !reflect.DeepEqual(item.AllowedFamilyModes, []ipc.ExitFamilyMode{ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY}) || !reflect.DeepEqual(item.AllowedLanAccess, []ipc.LanAccess{ipc.LanAccess_LAN_ACCESS_BLOCK}) {
						t.Fatal("policy or executor restrictions widened", item)
					}
				} else if len(item.AllowedFamilyModes) != 0 || len(item.AllowedLanAccess) != 0 {
					t.Fatal("unavailable selection retained selectable modes")
				}
			}
			if !reflect.DeepEqual(before, clonePersistentConfig(m.store.Read())) || len(m.capabilityWorkers) != 0 {
				t.Fatal("read projection mutated state or advertised runtime readiness")
			}
			if scenario == "ready" {
				item := catalog.ExitNodes[0]
				_, err := m.selectExitNodeAs(owner, &ipc.SelectExitNodeRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ExitNodeId: item.Id, FamilyMode: item.AllowedFamilyModes[0], LanAccess: item.AllowedLanAccess[0]}, s.exitModes)
				if err != nil {
					t.Fatal("advertised mode rejected by actual admission", err)
				}
			}
		})
	}
}

func TestExitWorkerReadinessRebootstrapsStreamsWithoutCapability(t *testing.T) {
	m, owner, profile := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	old, err := m.subscribe(owner, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(old)
	before := m.Metadata().Revision
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	worker, err := s.startExitWorker(ctx, clientRPCExitExecutor{
		InterfaceName: "endlessnet", Lock: &sync.Mutex{},
		Apply: func(context.Context, string, Config, *ClientExitSelection) (*ipc.ExitNodeStatus, ipc.ConnectionContinuity, error) {
			return nil, 0, errors.New("unused apply")
		},
		Contain: func(context.Context, clientRPCExitChange) (clientRPCExitContainment, error) {
			return clientRPCExitContainment{}, errors.New("unused containment")
		},
		Release: releaseExitTestCallback,
		Observe: func(context.Context, Config) (*ipc.ExitNodeStatus, error) {
			return nil, errors.New("unavailable native observation")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertClosed := func(sub *rpcSubscriber) {
		t.Helper()
		sub.mu.Lock()
		closed, failure := sub.closed, sub.err
		sub.mu.Unlock()
		if !closed {
			t.Fatal("readiness transition retained old stream")
		}
		assertRPCFailure(t, failure, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	assertClosed(old)
	active, err := m.subscribe(owner, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(active)
	status, err := s.exitNodeAs(t.Context(), owner, &ipc.GetExitNodeRequest{Profile: profile})
	if err != nil || status.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_AVAILABLE {
		t.Fatal("live worker control not available", err)
	}
	cancel()
	select {
	case <-worker:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not join")
	}
	assertClosed(active)
	if m.Metadata().Revision != before || len(m.capabilityWorkers) != 0 {
		t.Fatal("readiness rebootstrap persisted a revision or advertised capability")
	}
	status, err = s.exitNodeAs(t.Context(), owner, &ipc.GetExitNodeRequest{Profile: profile})
	if err != nil || status.Control.Mutation.Availability == ipc.Availability_AVAILABILITY_AVAILABLE {
		t.Fatal("stopped worker retained available control", err)
	}
}

func TestExitReadControlRechecksWorkerAfterObservation(t *testing.T) {
	for _, replace := range []bool{false, true} {
		m, owner, profile := rpcConnectFixture(t)
		s := NewClientRPCService(m, nil)
		ctx, cancel := context.WithCancel(t.Context())
		s.exitWorker = &clientRPCProfileWorker{ctx: ctx}
		s.exitObservation = &clientRPCExitObservationSource{ctx: ctx, lock: &sync.Mutex{}, observe: func(context.Context, Config) (*ipc.ExitNodeStatus, error) {
			cancel()
			if replace {
				s.exitMu.Lock()
				s.exitWorker = &clientRPCProfileWorker{ctx: t.Context()}
				s.exitObservation = nil
				s.exitMu.Unlock()
			}
			return nativeExitClearStatus(profile.ProfileId, false), nil
		}}
		status, err := s.exitNodeAs(t.Context(), owner, &ipc.GetExitNodeRequest{Profile: profile})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if (status.Control.Mutation.Availability == ipc.Availability_AVAILABILITY_AVAILABLE) != replace {
			t.Fatal("control retained former worker readiness", status.Control)
		}
		if status.ApplyState == ipc.ApplyState_APPLY_STATE_APPLIED {
			t.Fatal("former worker observation escaped replacement")
		}
	}
}

func TestExitCatalogModesNeverWidenPairs(t *testing.T) {
	for _, allowOnly := range []bool{false, true} {
		item := new(ipc.ExitNode)
		modes := []clientRPCExitMode{{Family: api.ExitFamilyIPv6Only, LAN: api.ExitLANAllow}}
		if !allowOnly {
			modes = append(modes, clientRPCExitMode{Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock})
		}
		exitCatalogModes(item, modes, func(clientRPCExitMode) bool { return true })
		family, lan := ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV4_ONLY, ipc.LanAccess_LAN_ACCESS_BLOCK
		if allowOnly {
			family, lan = ipc.ExitFamilyMode_EXIT_FAMILY_MODE_IPV6_ONLY, ipc.LanAccess_LAN_ACCESS_ALLOW
		}
		if !reflect.DeepEqual(item.AllowedFamilyModes, []ipc.ExitFamilyMode{family}) || !reflect.DeepEqual(item.AllowedLanAccess, []ipc.LanAccess{lan}) {
			t.Fatalf("non-rectangular modes widened: %v", item)
		}
	}
}
