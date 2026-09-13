package client

import (
	"encoding/json"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCUpdateInfoDoesNotInferReleaseOrPairing(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "owner"}
	if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = peer.Identity; return nil }); err != nil {
		t.Fatal(err)
	}
	build := &ipc.BuildIdentity{Version: "synthetic-version", Commit: "synthetic-commit", Platform: ipc.Platform_PLATFORM_WINDOWS, Architecture: "amd64"}
	s := NewClientRPCService(m, build)
	before, err := json.Marshal(m.store.Read())
	if err != nil {
		t.Fatal(err)
	}
	for _, reported := range []*ipc.BuildIdentity{nil, {}, build, {Version: "different", Platform: ipc.Platform_PLATFORM_ANDROID, Architecture: "arm64"}} {
		request := &ipc.GetUpdateInfoRequest{ReportedUi: reported}
		response, err := s.updateInfoAs(peer, request)
		if err != nil {
			t.Fatal(err)
		}
		info := response.Info
		if !proto.Equal(info.InstalledRuntime, build) || !proto.Equal(info.ReportedUi, reported) || info.Metadata.InstanceId != m.instanceID || info.Metadata.Revision != m.Metadata().Revision {
			t.Fatal("update projection lost its current build, caller claim or metadata")
		}
		if info.State != ipc.UpdateState_UPDATE_STATE_SOURCE_UNAVAILABLE || info.Available != nil || info.InstalledPair.State != ipc.CompatibilityState_COMPATIBILITY_STATE_UNKNOWN || len(info.InstalledPair.AcceptedContractSha256) != 0 || info.Discovery.Availability != ipc.Availability_AVAILABILITY_UNSUPPORTED {
			t.Fatal("unconfigured source inferred verified distribution or pair compatibility")
		}
		if info.Metadata.GeneratedAt.CheckValid() != nil {
			t.Fatal("invalid observation timestamp")
		}
		info.InstalledRuntime.Version = "changed response"
		if info.ReportedUi != nil {
			info.ReportedUi.Version = "changed claim copy"
			if reported.Version == "changed claim copy" {
				t.Fatal("response aliased caller-owned build identity")
			}
		}
	}
	after, err := json.Marshal(m.store.Read())
	if err != nil || string(before) != string(after) {
		t.Fatal("update discovery changed configuration or operation journal")
	}
}

func TestRPCUpdateInfoAuthorizationPrecedesValidation(t *testing.T) {
	m := newRPCStoreTest(t)
	s := NewClientRPCService(m, nil)
	if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = "owner"; return nil }); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		peer local.Peer
		want ipc.ErrorCode
	}{
		{local.Peer{}, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED},
		{local.Peer{Identity: "observer"}, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED},
		{local.Peer{Identity: "owner"}, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT},
		{local.Peer{Identity: "admin", Administrator: true}, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT},
	} {
		_, err := s.updateInfoAs(test.peer, nil)
		assertRPCFailure(t, err, test.want)
	}
	for _, request := range []*ipc.GetUpdateInfoRequest{
		{ReportedUi: &ipc.BuildIdentity{Platform: ipc.Platform(999)}},
	} {
		_, err := s.updateInfoAs(local.Peer{Identity: "owner"}, request)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if _, err := s.updateInfoAs(local.Peer{Identity: "admin", Administrator: true}, &ipc.GetUpdateInfoRequest{}); err != nil {
		t.Fatal("administrator could not read installation-level update state", err)
	}
}
