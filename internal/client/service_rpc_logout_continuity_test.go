package client

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCLogoutRequiresConfirmedStopAcrossRestart(t *testing.T) {
	for name, continuity := range map[string]ipc.ConnectionContinuity{
		"preserved": ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED,
		"invalid":   ipc.ConnectionContinuity(12345),
	} {
		t.Run(name, func(t *testing.T) {
			m, peer, enroll := enrollmentAdmissionTest(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NetworkID, cfg.NodeID, cfg.NodeCredential, cfg.Token = "original-network", "node", "synthetic-credential", "synthetic-session"
				cfg.CachedMap = &clientapi.RegisterNodeResponse{Node: clientapi.Node{ID: "node"}}
				profile := cfg.RPCState.Profiles[enroll.Profile.ProfileId]
				profile.Configuration = Config{NodeID: cfg.NodeID, Token: cfg.Token, NodeCredential: cfg.NodeCredential}
				cfg.RPCState.Profiles[profile.ID] = profile
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			op, err := m.logoutAs(peer, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile})
			if err != nil {
				t.Fatal(err)
			}
			// Model restart after a prior Down began. Continuity normalization on
			// recovery must not turn a rejected Stop result into confirmation.
			if _, err := m.ReconcileOperation(op.Id, func(cfg *Config, op *ipc.Operation) error {
				op.State = ipc.OperationState_OPERATION_STATE_RUNNING
				cfg.RPCState.Logout.DownStarted = true
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			if plan := m.store.Read().RPCState.Logout; plan.NetworkID != "original-network" || !logoutPlanBound(m.store.Read(), plan) {
				t.Fatal("restart lost admitted logout context")
			}
			stops := 0
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return continuity, nil
			}}
			if err := m.ReconcileLogout(t.Context(), driver, func(_ context.Context, _ Config, _ ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
				return "", checkpoint(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true})
			}); err != nil {
				t.Fatal(err)
			}
			m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
			if err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil || result.GetState() != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_APPLY_FAILED || stops != 1 {
				t.Fatal("unconfirmed stop did not fail durably", err)
			}
			cfg := m.store.Read()
			profile, exists := cfg.RPCState.Profiles[enroll.Profile.ProfileId]
			if !exists || cfg.Token != "synthetic-session" || cfg.NodeCredential != "synthetic-credential" || cfg.NodeID != "node" || profile.Configuration.Token != cfg.Token || profile.Configuration.NodeCredential != cfg.NodeCredential || profile.Configuration.NodeID != cfg.NodeID {
				t.Fatal("unconfirmed stop removed retained registration")
			}
			if profile.LogoutConfirmation == nil || profile.LogoutConfirmation.NetworkID != "original-network" || !profile.LogoutConfirmation.Progress.NodeRevoked || !profile.LogoutConfirmation.Progress.SessionRevoked {
				t.Fatal("confirmed remote progress was lost on restart")
			}
			retry, err := m.logoutAs(peer, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile})
			if err != nil {
				t.Fatal(err)
			}
			driver.Stop = func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
			}
			providers := 0
			if err := m.ReconcileLogout(t.Context(), driver, func(_ context.Context, retained Config, progress ClientRPCLogoutProgress, _ func(ClientRPCLogoutProgress) error) (string, error) {
				providers++
				if !progress.NodeRevoked || !progress.SessionRevoked || retained.Token != "synthetic-session" || retained.NodeCredential != "synthetic-credential" {
					t.Fatal("retry lost confirmed steps or original authority")
				}
				return "", nil
			}); err != nil {
				t.Fatal(err)
			}
			result, err = m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: retry.Id}})
			if err != nil || result.GetState() != ipc.OperationState_OPERATION_STATE_SUCCEEDED || !result.GetCleanup().GetLocalRegistrationRemoved() || stops != 2 || providers != 1 {
				t.Fatal("confirmed retry did not finish cleanup", err)
			}
			cfg = reopenRPCStoreFromDisk(t, m.store).Read()
			profile = cfg.RPCState.Profiles[enroll.Profile.ProfileId]
			if cfg.Token != "" || cfg.NodeCredential != "" || cfg.NodeID != "" || cfg.CachedMap != nil || cfg.RPCState.Logout != nil || profile.LogoutConfirmation != nil || profile.Configuration.Token != "" || profile.Configuration.NodeCredential != "" || profile.Configuration.NodeID != "" {
				t.Fatal("confirmed cleanup retained stale registration or progress")
			}
		})
	}
}

// WithCancel consults the parent's Done after RUNNING admission. This hook
// changes persistent context at that boundary without scheduler-dependent races.
type logoutDispatchContext struct {
	context.Context
	onDone func()
}

func (ctx logoutDispatchContext) Done() <-chan struct{} {
	ctx.onDone()
	return ctx.Context.Done()
}

func TestRPCLogoutRejectsChangedDispatchContext(t *testing.T) {
	for _, scenario := range []string{"network_before_restart", "authority_before_restart", "network_after_running"} {
		t.Run(scenario, func(t *testing.T) {
			m, peer, enroll := enrollmentAdmissionTest(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NetworkID, cfg.Token = "original-network", "synthetic-session"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := m.logoutAs(peer, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile}); err != nil {
				t.Fatal(err)
			}
			change := func() {
				if scenario == "network_after_running" {
					id := m.store.Read().RPCState.Logout.OperationID
					op, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: id}})
					if err != nil || op.GetState() != ipc.OperationState_OPERATION_STATE_RUNNING {
						t.Fatal("dispatch hook did not run after admission", err)
					}
				}
				if err := m.store.Update(func(cfg *Config) error {
					if scenario == "authority_before_restart" {
						cfg.Token = "replacement"
					} else {
						cfg.NetworkID = "other-network"
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			ctx := t.Context()
			if scenario == "network_after_running" {
				var once sync.Once
				ctx = logoutDispatchContext{Context: ctx, onDone: func() { once.Do(change) }}
			} else {
				change()
				var err error
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
			}
			err := m.ReconcileLogout(ctx, ClientRPCProfileDriver{Lock: new(sync.Mutex), Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				t.Fatal("changed context reached Stop")
				return 0, nil
			}}, func(context.Context, Config, ClientRPCLogoutProgress, func(ClientRPCLogoutProgress) error) (string, error) {
				t.Fatal("changed context reached remote dispatch")
				return "", nil
			})
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			cfg := reopenRPCStoreFromDisk(t, m.store).Read()
			if cfg.Token == "" || cfg.RPCState.Logout.NetworkID != "original-network" || cfg.RPCState.Logout.Progress.NodeRevoked {
				t.Fatal("rejected dispatch changed registration or admitted context")
			}
		})
	}
}

func TestRPCLogoutConfirmationCannotCrossNetworks(t *testing.T) {
	m, peer, enroll := enrollmentAdmissionTest(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NetworkID, cfg.Token = "original-network", "synthetic-session"
		cfg.NodeID, cfg.NodeCredential = "node", "synthetic-credential"
		cfg.CachedMap = &clientapi.RegisterNodeResponse{Node: clientapi.Node{ID: "node"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.logoutAs(peer, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile})
	if err != nil {
		t.Fatal(err)
	}
	driver := ClientRPCProfileDriver{Lock: new(sync.Mutex), Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		t.Fatal("partial revocation invoked Stop")
		return 0, nil
	}}
	var late func(ClientRPCLogoutProgress) error
	if err := m.ReconcileLogout(t.Context(), driver, func(_ context.Context, _ Config, _ ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
		late = checkpoint
		if err := checkpoint(ClientRPCLogoutProgress{NodeRevoked: true}); err != nil {
			return "", err
		}
		return "original-request", errors.New("session revocation failed")
	}); err != nil {
		t.Fatal(err)
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	if rpcLocalCleanupRequestID(m.store.Read(), enroll.Profile.ProfileId) != "original-request" {
		t.Fatal("restart lost correlation")
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.NetworkID = "other-network"; return nil }); err != nil {
		t.Fatal(err)
	}
	before := m.store.Read()
	if rpcLocalCleanupRequestID(before, enroll.Profile.ProfileId) != "" {
		t.Fatal("foreign network reused correlation")
	}
	// Connect must still reject the revoked credential, even on another network.
	_, err = m.connectAs(peer, &ipc.ConnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
	if _, err := m.logoutAs(peer, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile}); err != nil {
		t.Fatal(err)
	}
	cfg := m.store.Read()
	if cfg.RPCState.Logout.OperationID == op.Id || cfg.RPCState.Logout.NetworkID != "other-network" || cfg.RPCState.Logout.Progress != (ClientRPCLogoutProgress{}) {
		t.Fatal("foreign network reused remote progress")
	}
	// A callback retained by the old runtime cannot checkpoint a replacement plan.
	assertRPCFailure(t, late(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true}), ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if !reflect.DeepEqual(cfg, m.store.Read()) {
		t.Fatal("late confirmation modified new plan")
	}
}

func TestRPCLogoutAndForgetStopContract(t *testing.T) {
	for _, forget := range []bool{false, true} {
		for name, continuity := range map[string]ipc.ConnectionContinuity{"unknown": ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, "absent": ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, "interrupted": ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, "preserved": ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED, "invalid": 12345} {
			t.Run(fmt.Sprintf("forget_%t/%s", forget, name), func(t *testing.T) {
				m, peer, enroll := enrollmentAdmissionTest(t)
				if err := m.store.Update(func(cfg *Config) error { cfg.Token = "synthetic-session"; return nil }); err != nil {
					t.Fatal(err)
				}
				var op *ipc.Operation
				var err error
				if forget {
					peer.Administrator = true
					op, err = m.forgetEnrollmentAs(peer, &ipc.ForgetLocalEnrollmentRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile, Confirmed: true})
				} else {
					op, err = m.logoutAs(peer, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile})
				}
				if err != nil {
					t.Fatal(err)
				}
				driver := ClientRPCProfileDriver{Lock: new(sync.Mutex), Stop: func(context.Context) (ipc.ConnectionContinuity, error) { return continuity, nil }}
				if forget {
					err = m.ReconcileDisconnect(t.Context(), driver)
				} else {
					err = m.ReconcileLogout(t.Context(), driver, func(_ context.Context, _ Config, _ ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
						return "", checkpoint(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true})
					})
				}
				if err != nil {
					t.Fatal(err)
				}
				m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
				if err != nil {
					t.Fatal(err)
				}
				result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
				if err != nil {
					t.Fatal(err)
				}
				confirmed := name == "unknown" || name == "absent" || name == "interrupted"
				if confirmed {
					if result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || !result.GetCleanup().GetLocalRegistrationRemoved() || m.store.Read().Token != "" {
						t.Fatal("confirmed stop did not clean up")
					}
				} else if result.State != ipc.OperationState_OPERATION_STATE_FAILED || m.store.Read().Token != "synthetic-session" {
					t.Fatal("unconfirmed stop removed credentials")
				}
			})
		}
	}
}

func TestRPCLogoutRejectsNetworkChangeBeforeStop(t *testing.T) {
	m, peer, enroll := enrollmentAdmissionTest(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NetworkID, cfg.NodeID, cfg.Token, cfg.NodeCredential = "original-network", "node", "synthetic-session", "synthetic-credential"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.logoutAs(peer, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile}); err != nil {
		t.Fatal(err)
	}
	stops := 0
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}}
	err := m.ReconcileLogout(t.Context(), driver, func(_ context.Context, _ Config, _ ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
		if err := checkpoint(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true}); err != nil {
			return "", err
		}
		return "", m.store.Update(func(cfg *Config) error { cfg.NetworkID = "new-network"; return nil })
	})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	cfg := reopenRPCStoreFromDisk(t, m.store).Read()
	if stops != 0 || cfg.NetworkID != "new-network" || cfg.NodeID != "node" || cfg.Token != "synthetic-session" || cfg.NodeCredential != "synthetic-credential" {
		t.Fatal("late network change invoked Stop or erased registration")
	}
}

func TestRPCCleanupRejectsContextChangeDuringStop(t *testing.T) {
	for _, forget := range []bool{false, true} {
		for _, field := range []string{"network", "profile", "owner", "authority"} {
			t.Run(fmt.Sprintf("forget_%t/%s", forget, field), func(t *testing.T) {
				m, peer, enroll := enrollmentAdmissionTest(t)
				if err := m.store.Update(func(cfg *Config) error {
					cfg.NetworkID, cfg.Token = "original-network", "synthetic-session"
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				var err error
				if forget {
					peer.Administrator = true
					_, err = m.forgetEnrollmentAs(peer, &ipc.ForgetLocalEnrollmentRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile, Confirmed: true})
				} else {
					_, err = m.logoutAs(peer, &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile})
				}
				if err != nil {
					t.Fatal(err)
				}
				var afterChange Config
				driver := ClientRPCProfileDriver{Lock: new(sync.Mutex), Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
					if err := m.store.Update(func(cfg *Config) error {
						switch field {
						case "network":
							cfg.NetworkID = "other-network"
						case "profile":
							cfg.RPCState.ActiveProfileID = "other-profile"
						case "owner":
							cfg.LocalOwnerID = "other-owner"
						case "authority":
							cfg.Token = "replacement"
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					afterChange = m.store.Read()
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
				}}
				if forget {
					err = m.ReconcileDisconnect(t.Context(), driver)
				} else {
					err = m.ReconcileLogout(t.Context(), driver, func(_ context.Context, _ Config, _ ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
						return "", checkpoint(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true})
					})
				}
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
				if !reflect.DeepEqual(afterChange, m.store.Read()) {
					t.Fatal("late Stop removed or changed foreign context")
				}
				cfg := reopenRPCStoreFromDisk(t, m.store).Read()
				if cfg.Token == "" {
					t.Fatal("restart lost retained credentials")
				}
			})
		}
	}
}
