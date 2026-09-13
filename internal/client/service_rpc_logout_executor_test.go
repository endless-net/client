package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCLogoutExecutorPreservesStateAndRetriesConfirmedSteps(t *testing.T) {
	for _, failRemote := range []bool{false, true} {
		t.Run(map[bool]string{false: "Down failure", true: "remote failure"}[failRemote], func(t *testing.T) {
			m, peer, enroll := enrollmentAdmissionTest(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NodeCredential, cfg.Token = "node", "synthetic-credential", "synthetic-session"
				cfg.CachedMap = &clientapi.RegisterNodeResponse{Node: clientapi.Node{ID: "node"}}
				profile := cfg.RPCState.Profiles[enroll.Profile.ProfileId]
				profile.Configuration = Config{NodeID: cfg.NodeID, Token: cfg.Token, NodeCredential: cfg.NodeCredential}
				cfg.RPCState.Profiles[profile.ID] = profile
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			request := &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile}
			op, err := m.logoutAs(peer, request)
			if err != nil {
				t.Fatal(err)
			}
			stops := 0
			driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stops++
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("synthetic Down failure")
			}}
			err = m.ReconcileLogout(t.Context(), driver, func(_ context.Context, _ Config, _ ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
				if err := checkpoint(ClientRPCLogoutProgress{NodeRevoked: true}); err != nil {
					return "", err
				}
				if failRemote {
					return "remote-correlation", errors.New("synthetic private remote failure")
				}
				return "", checkpoint(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true})
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil || result.State != ipc.OperationState_OPERATION_STATE_FAILED || m.store.Read().Token == "" || m.store.Read().NodeCredential == "" {
				t.Fatal("failed logout deleted registration", err)
			}
			if failRemote && (stops != 0 || result.GetFailure().ControlRequestId != "remote-correlation") {
				t.Fatal("remote failure lost correlation or invoked Down")
			}
			_, err = m.connectAs(peer, &ipc.ConnectRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile})
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
			request.Mutation = rpcCreateRequest(t, m).Mutation
			if _, err := m.logoutAs(peer, request); err != nil {
				t.Fatal(err)
			}
			driver.Stop = func(context.Context) (ipc.ConnectionContinuity, error) {
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
			}
			err = m.ReconcileLogout(t.Context(), driver, func(_ context.Context, _ Config, progress ClientRPCLogoutProgress, checkpoint func(ClientRPCLogoutProgress) error) (string, error) {
				if !progress.NodeRevoked || progress.SessionRevoked == failRemote {
					t.Fatal("retry lost confirmed progress")
				}
				return "", checkpoint(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true})
			})
			if err != nil {
				t.Fatal(err)
			}
			cfg := m.store.Read()
			if retained := cfg.RPCState.Profiles[enroll.Profile.ProfileId].Configuration; retained.NodeID != "" || retained.Token != "" || retained.NodeCredential != "" {
				t.Fatal("profile retained a stale credential copy")
			}
			if cfg.NodeID != "" || cfg.Token != "" || cfg.RPCState.Logout != nil || cfg.RPCState.Profiles[enroll.Profile.ProfileId].LogoutConfirmation != nil {
				t.Fatal("confirmed logout failed to clean up")
			}
		})
	}
}
