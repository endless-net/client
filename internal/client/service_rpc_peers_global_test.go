package client

import (
	"context"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCPeersGlobalRevisionRejectsStaleObservationAndPage(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	setGlobal := func(global uint64) {
		t.Helper()
		if err := m.store.Update(func(cfg *Config) error {
			cfg.CachedMap.Network.Revision = 7
			cfg.CachedMap.Revision.Global = global
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	setGlobal(10)
	global := uint64(9)
	s := NewClientRPCService(m, nil)
	s.PeersProvider = func(context.Context) (ClientRPCPeerObservation, error) {
		return ClientRPCPeerObservation{ProfileID: profile.ProfileId, MapRevision: 7, MapGlobalRevision: global, Peers: []*ipc.Peer{{Id: "a"}, {Id: "b"}}}, nil
	}
	req := &ipc.ListPeersRequest{Profile: profile, Page: &ipc.PageRequest{PageSize: 1}}
	result, err := s.peersAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if result != nil {
		t.Fatal("old global policy disclosed peers")
	}
	global = 10
	first, err := s.peersAs(t.Context(), peer, req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Page.NextPageToken == "" {
		t.Fatal("fixture must span pages")
	}
	// Same visible peers and network revision, but a new global authorization
	// snapshot. A continuation must not splice two different snapshots together.
	setGlobal(11)
	global = 11
	req.Page.PageToken = first.Page.NextPageToken
	result, err = s.peersAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if result != nil {
		t.Fatal("page crossed global policy revisions")
	}
	req.Page.PageToken = ""
	if _, err := s.peersAs(t.Context(), peer, req); err != nil {
		t.Fatal(err)
	}
}
