package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCProfileSwitchStopsBeforeChangingContext(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	request := rpcCreateRequest(t, m)
	request.ControlOrigin = "https://target.test"
	created, err := m.createProfileAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.PrivateKey = "installation-tunnel-key"
		cfg.IdentityPrivateKey = "installation-identity-key"
		profile := cfg.RPCState.Profiles[created.ProfileId]
		profile.Configuration.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		cfg.RPCState.Profiles[created.ProfileId] = profile
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	selected, err := m.selectProfileAs(peer, &ipc.SelectProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}})
	if err != nil {
		t.Fatal(err)
	}
	if m.store.Read().RPCState.ActiveProfileID != "" || selected.State != ipc.OperationState_OPERATION_STATE_PENDING {
		t.Fatal("acceptance switched live state before stopping")
	}
	order := []string{}
	m.observedStatus = &ipc.Status{ActiveProfileId: "old-observation"}
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		if m.store.Read().RPCState.ActiveProfileID != "" {
			t.Fatal("old context changed before stop")
		}
		order = append(order, "stop")
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}, Start: func(_ context.Context, cfg Config) error {
		if m.observedStatus != nil {
			t.Fatal("old-profile observation survived target activation")
		}
		if cfg.RPCState.ActiveProfileID != created.ProfileId || cfg.PrivateKey != "installation-tunnel-key" || cfg.IdentityPrivateKey != "installation-identity-key" || cfg.ControlURLs()[0] != "https://target.test" {
			t.Fatal("target context was not committed before apply")
		}
		order = append(order, "start")
		return nil
	}}
	if err := m.ReconcileProfileSwitch(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "stop" || order[1] != "start" {
		t.Fatal(order)
	}
	op, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: selected.Id}})
	if err != nil {
		t.Fatal(err)
	}
	if op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.GetSelection().SelectedId != created.ProfileId || op.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED {
		t.Fatal("incorrect terminal switch outcome")
	}
	if err := m.ReconcileProfileSwitch(t.Context(), driver); err != nil || len(order) != 2 {
		t.Fatal("completed switch ran again")
	}
}

func TestRPCProfileSwitchFailureDoesNotMixContexts(t *testing.T) {
	for _, failStop := range []bool{true, false} {
		m := newRPCStoreTest(t)
		peer := local.Peer{Identity: "uid:1000"}
		request := rpcCreateRequest(t, m)
		request.ControlOrigin = "https://target.test"
		created, err := m.createProfileAs(peer, request)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.store.Update(func(cfg *Config) error {
			p := cfg.RPCState.Profiles[created.ProfileId]
			p.Configuration.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
			cfg.RPCState.Profiles[created.ProfileId] = p
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		selected, err := m.selectProfileAs(peer, &ipc.SelectProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}})
		if err != nil {
			t.Fatal(err)
		}
		stops := 0
		starts := 0
		driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
			stops++
			if failStop {
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("private stop error")
			}
			return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
		}, Start: func(context.Context, Config) error { starts++; return errors.New("private apply error") }}
		if err := m.ReconcileProfileSwitch(t.Context(), driver); err != nil {
			t.Fatal(err)
		}
		op, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: selected.Id}})
		if err != nil {
			t.Fatal(err)
		}
		if op.State != ipc.OperationState_OPERATION_STATE_FAILED || op.GetFailure().Code != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED {
			t.Fatal("failed switch claimed success")
		}
		cfg := m.store.Read()
		if failStop {
			if starts != 0 || cfg.RPCState.ActiveProfileID != "" {
				t.Fatal("stop failure applied target")
			}
		} else if stops != 2 || starts != 1 || cfg.RPCState.ActiveProfileID != created.ProfileId || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
			t.Fatal("partial apply was not torn down with disconnected intent")
		}
	}
}

func TestRPCProfileSwitchRestart(t *testing.T) {
	for _, activated := range []bool{false, true} {
		t.Run(map[bool]string{false: "after_stop", true: "after_activation"}[activated], func(t *testing.T) {
			m := newRPCStoreTest(t)
			peer := local.Peer{Identity: "uid:1000"}
			request := rpcCreateRequest(t, m)
			request.ControlOrigin = "https://target.test"
			created, err := m.createProfileAs(peer, request)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.store.Update(func(cfg *Config) error {
				p := cfg.RPCState.Profiles[created.ProfileId]
				p.Configuration.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
				cfg.RPCState.Profiles[created.ProfileId] = p
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			selected, err := m.selectProfileAs(peer, &ipc.SelectProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}})
			if err != nil {
				t.Fatal(err)
			}
			// Inject process loss at a side-effect boundary, leaving only durable
			// state available to the replacement runtime.
			crash := errors.New("simulated process loss")
			func() {
				defer func() {
					if recovered := recover(); recovered != crash {
						t.Fatalf("unexpected crash: %v", recovered)
					}
				}()
				_ = m.ReconcileProfileSwitch(t.Context(), ClientRPCProfileDriver{Lock: &sync.Mutex{},
					Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
						if !activated {
							panic(crash)
						}
						return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
					}, Start: func(context.Context, Config) error { panic(crash) }})
			}()
			store, err := OpenConfigStore(m.store.path)
			if err != nil {
				t.Fatal(err)
			}
			restarted, err := NewClientRPCMutations(store)
			if err != nil {
				t.Fatal(err)
			}
			stops, starts := 0, 0
			if err := restarted.ReconcileProfileSwitch(t.Context(), ClientRPCProfileDriver{Lock: &sync.Mutex{},
				Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
					stops++
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
				},
				Start: func(_ context.Context, cfg Config) error {
					starts++
					if cfg.RPCState.ActiveProfileID != created.ProfileId {
						t.Fatal("wrong resumed target")
					}
					return nil
				},
			}); err != nil {
				t.Fatal(err)
			}
			op, err := restarted.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: selected.Id}})
			if err != nil {
				t.Fatal(err)
			}
			want := ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN
			if activated {
				want = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED
			}
			if stops != 1 || starts != 1 || op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.Continuity != want || store.Read().RPCState.ProfileSwitch != nil {
				t.Fatal("restart lost operation progress or historical continuity")
			}
		})
	}
}

func TestRPCProfileSwitchSameProfileAndBusy(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	r := rpcCreateRequest(t, m)
	r.ControlOrigin = "https://target.test"
	created, err := m.createProfileAs(peer, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.RPCState.ActiveProfileID = created.ProfileId; return nil }); err != nil {
		t.Fatal(err)
	}
	req := &ipc.SelectProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}}
	op, err := m.selectProfileAs(peer, req)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := m.selectProfileAs(peer, req)
	if err != nil || repeated.Id != op.Id {
		t.Fatal("pending retry not idempotent", err)
	}
	_, err = m.selectProfileAs(peer, &ipc.SelectProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: req.Profile})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
	if err := m.ReconcileProfileSwitch(t.Context(), ClientRPCProfileDriver{Lock: &sync.Mutex{},
		Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
			t.Fatal("same profile stopped tunnel")
			return 0, nil
		},
		Start: func(context.Context, Config) error { t.Fatal("same profile reapplied tunnel"); return nil },
	}); err != nil {
		t.Fatal(err)
	}
	final, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil || final.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED {
		t.Fatal("same profile continuity", err)
	}
}
