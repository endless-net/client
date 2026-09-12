package client

import (
	"bytes"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCProfilePaginationBindingsAndPrivacy(t *testing.T) {
	m := newRPCStoreTest(t)
	now := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	peer := local.Peer{Identity: "uid:1000"}
	for i := 0; i < 3; i++ {
		req := rpcCreateRequest(t, m)
		req.ControlOrigin = "https://control.example.test"
		if _, err := m.createProfileAs(peer, req); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.store.Update(func(cfg *Config) error {
		for id, profile := range cfg.RPCState.Profiles {
			profile.Configuration.Token = "test-only-private-enrollment-token"
			cfg.RPCState.Profiles[id] = profile
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	page, err := m.listProfilesAs(peer, &ipc.ListProfilesRequest{Page: &ipc.PageRequest{PageSize: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Profiles) != 1 || page.Page.NextPageToken == "" || page.Page.Metadata.Revision != m.Metadata().Revision {
		t.Fatal("invalid first page")
	}
	wire, err := proto.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wire, []byte("test-only-private-enrollment-token")) {
		t.Fatal("profile projection disclosed secret state")
	}
	token := page.Page.NextPageToken
	seen := map[string]bool{page.Profiles[0].Id: true}
	for page.Page.NextPageToken != "" {
		page, err = m.listProfilesAs(peer, &ipc.ListProfilesRequest{Page: &ipc.PageRequest{PageSize: 1, PageToken: page.Page.NextPageToken}})
		if err != nil {
			t.Fatal(err)
		}
		for _, profile := range page.Profiles {
			if seen[profile.Id] {
				t.Fatal("duplicate profile across pages")
			}
			seen[profile.Id] = true
		}
	}
	if len(seen) != 3 {
		t.Fatal("pagination omitted profiles")
	}
	for _, request := range []*ipc.PageRequest{{PageSize: 2, PageToken: token}, {PageSize: 1, PageToken: token + "tampered"}} {
		_, err = m.listProfilesAs(peer, &ipc.ListProfilesRequest{Page: request})
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	_, err = m.listProfilesAs(local.Peer{Identity: "administrator", Administrator: true}, &ipc.ListProfilesRequest{Page: &ipc.PageRequest{PageSize: 1, PageToken: token}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	_, _, _, err = m.pageRange(peer, "different-method", &ipc.PageRequest{PageSize: 1, PageToken: token}, m.store.Read(), 3)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	restarted, err := NewClientRPCMutations(m.store)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = m.now
	_, err = restarted.listProfilesAs(peer, &ipc.ListProfilesRequest{Page: &ipc.PageRequest{PageSize: 1, PageToken: token}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	now = now.Add(5 * time.Minute)
	_, err = m.listProfilesAs(peer, &ipc.ListProfilesRequest{Page: &ipc.PageRequest{PageSize: 1, PageToken: token}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	_, err = m.listProfilesAs(local.Peer{Identity: "other-user"}, &ipc.ListProfilesRequest{})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
}

func TestRPCProfilePaginationRejectsChangedSnapshot(t *testing.T) {
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	var profileID string
	for i := 0; i < 2; i++ {
		req := rpcCreateRequest(t, m)
		req.ControlOrigin = "https://control.example.test"
		op, err := m.createProfileAs(peer, req)
		if err != nil {
			t.Fatal(err)
		}
		profileID = op.ProfileId
	}
	page, err := m.listProfilesAs(peer, &ipc.ListProfilesRequest{Page: &ipc.PageRequest{PageSize: 1}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.renameProfileAs(peer, &ipc.RenameProfileRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: profileID}, DisplayName: "changed"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.listProfilesAs(peer, &ipc.ListProfilesRequest{Page: &ipc.PageRequest{PageSize: 1, PageToken: page.Page.NextPageToken}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	_, err = m.listProfilesAs(peer, &ipc.ListProfilesRequest{Page: &ipc.PageRequest{PageSize: 501}})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
}
