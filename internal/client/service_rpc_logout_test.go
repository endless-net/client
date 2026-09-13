package client

import (
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCLogoutAdmissionAndMonotonicRemoteProgress(t *testing.T) {
	m, peer, enroll := enrollmentAdmissionTest(t)
	if err := m.store.Update(func(cfg *Config) error { cfg.NodeID = "node"; cfg.Token = "synthetic-session"; return nil }); err != nil {
		t.Fatal(err)
	}
	req := &ipc.LogoutRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile}
	op, err := m.logoutAs(peer, req)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := m.logoutAs(peer, req)
	if err != nil || !proto.Equal(op, retry) {
		t.Fatal("logout acceptance not idempotent")
	}
	checkpoint := m.LogoutProgressCallback(op.Id, m.store.Read())
	assertRPCFailure(t, checkpoint(ClientRPCLogoutProgress{NodeRevoked: true}), ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	_, err = m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkpoint(ClientRPCLogoutProgress{NodeRevoked: true}); err != nil {
		t.Fatal(err)
	}
	assertRPCFailure(t, checkpoint(ClientRPCLogoutProgress{}), ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	disk, err := loadConfigFile(m.store.path)
	if err != nil || disk.RPCState.Logout == nil || !disk.RPCState.Logout.Progress.NodeRevoked || disk.Token != "synthetic-session" || disk.NodeID != "node" {
		t.Fatal("remote checkpoint lost confirmation or deleted local registration")
	}
	restarted, err := NewClientRPCMutations(m.store)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint = restarted.LogoutProgressCallback(op.Id, restarted.store.Read())
	if err := checkpoint(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.Token = "synthetic-replacement-session"; return nil }); err != nil {
		t.Fatal(err)
	}
	assertRPCFailure(t, checkpoint(ClientRPCLogoutProgress{NodeRevoked: true, SessionRevoked: true}), ipc.ErrorCode_ERROR_CODE_STALE_STATE)
}
