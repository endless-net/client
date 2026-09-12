package client

import (
	"errors"
	"strings"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCRuntimeDoesNotExposeDiagnosticErrors(t *testing.T) {
	err := runtimeRPCFailure(errors.New("private-path-and-provider-diagnostic"))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	if strings.Contains(err.Error(), "private-path") {
		t.Fatal("runtime leaked diagnostic text")
	}
}

func TestRPCProfilePendingOperationBlocksRemoval(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	request := rpcCreateRequest(t, m)
	request.ControlOrigin = "https://control.example.test"
	created, err := m.createProfileAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = m.acceptAs(peer, "/client.v0.ClientService/RenewSession", &ipc.RenewSessionRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}}, func(_ *Config, op *ipc.Operation) error { op.ProfileId = created.ProfileId; return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.removeProfileAs(peer, &ipc.RemoveProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_BUSY)
}

func TestRPCProfileLocalLifecycle(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	request := rpcCreateRequest(t, m)
	request.ControlOrigin = "https://CONTROL.example.test:443/"
	created, err := m.createProfileAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	if created.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || created.Kind != ipc.OperationKind_OPERATION_KIND_CREATE_PROFILE || created.GetSelection().GetSelectedId() != created.ProfileId {
		t.Fatal("creation did not return typed completed operation")
	}
	cfg := m.store.Read()
	profile := cfg.RPCState.Profiles[created.ProfileId]
	if cfg.RPCState.ActiveProfileID != "" || profile.ControlOrigin != "https://control.example.test" || rpcConfigHasEnrollment(profile.Configuration) {
		t.Fatal("create changed active context, origin or registration")
	}
	record := cfg.RPCState.Operations[created.RequestId]
	if record.CompletedAt == nil {
		t.Fatal("synchronous completion is not durable")
	}
	replayed, err := m.createProfileAs(peer, request)
	if err != nil || !proto.Equal(created, replayed) || len(m.store.Read().RPCState.Profiles) != 1 {
		t.Fatalf("duplicate profile creation: %v", err)
	}
	ref := &ipc.ProfileRef{ProfileId: created.ProfileId}
	for _, name := range []string{"new name", "new name"} {
		before := m.store.Read().RPCState.Profiles[created.ProfileId].DisplayName
		op, err := m.renameProfileAs(peer, &ipc.RenameProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: ref, DisplayName: name})
		if err != nil {
			t.Fatal(err)
		}
		if op.GetChange().GetChanged() != (name != before) {
			t.Fatal("rename no-op outcome is wrong")
		}
		if m.store.Read().RPCState.Profiles[created.ProfileId].ControlOrigin != profile.ControlOrigin {
			t.Fatal("rename changed origin")
		}
	}
	remove := &ipc.RemoveProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: ref}
	removed, err := m.removeProfileAs(peer, remove)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.GetChange().GetChanged() || len(m.store.Read().RPCState.Profiles) != 0 || m.store.Read().LocalOwnerID != peer.Identity {
		t.Fatal("remove did not preserve installation ownership")
	}
	replayed, err = m.removeProfileAs(peer, remove)
	if err != nil || !proto.Equal(removed, replayed) {
		t.Fatalf("remove replay failed after profile deletion: %v", err)
	}
}

func TestRPCProfileRemovalGuards(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	request := rpcCreateRequest(t, m)
	request.ControlOrigin = "https://control.example.test"
	created, err := m.createProfileAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, active := range []bool{true, false} {
		if err := m.store.Update(func(cfg *Config) error {
			if active {
				cfg.RPCState.ActiveProfileID = created.ProfileId
			} else {
				cfg.RPCState.ActiveProfileID = ""
			}
			profile := cfg.RPCState.Profiles[created.ProfileId]
			profile.Configuration.NodeCredential = "test-only-retained-credential"
			cfg.RPCState.Profiles[created.ProfileId] = profile
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		_, err := m.removeProfileAs(peer, &ipc.RemoveProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}})
		code := ipc.ErrorCode_ERROR_CODE_REMOTE_CLEANUP_REQUIRED
		if active {
			code = ipc.ErrorCode_ERROR_CODE_PROFILE_ACTIVE
		}
		assertRPCFailure(t, err, code)
		if len(m.store.Read().RPCState.Profiles) != 1 {
			t.Fatal("rejected remove deleted profile")
		}
	}
}

func TestRPCProfileInputRejectionDoesNotClaimOwnership(t *testing.T) {
	for _, origin := range []string{"", "http://control.test", "https://user:secret@control.test", "https://control.test/path", "https://control.test?token=x", "https://control.test/#fragment", "https://control.test:0", "https://control.test:65536", "https://control.test:"} {
		m := newRPCStoreTest(t)
		request := rpcCreateRequest(t, m)
		request.ControlOrigin = origin
		_, err := m.createProfileAs(local.Peer{Identity: "uid:1000"}, request)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		if m.store.Read().LocalOwnerID != "" {
			t.Fatal("invalid profile claimed installation")
		}
	}
	for _, name := range []string{"", " \t ", "name\nline", strings.Repeat("x", 129), strings.Repeat("я", 65), string([]byte{255})} {
		if _, err := rpcProfileDisplayName(name); err == nil {
			t.Errorf("accepted invalid display name")
		}
	}
}
