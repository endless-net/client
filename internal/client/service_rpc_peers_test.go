package client

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCPeersRejectsMalformedQueryBeforeProvider(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	s := NewClientRPCService(m, nil)
	s.PeersProvider = func(context.Context) (ClientRPCPeerObservation, error) {
		t.Fatal("invalid request reached provider")
		return ClientRPCPeerObservation{}, nil
	}
	for _, request := range []*ipc.ListPeersRequest{
		{Profile: profile, Search: string([]byte{0xff})},
		{Profile: profile, Search: strings.Repeat(" ", 257)},
		{Profile: profile, Page: &ipc.PageRequest{PageSize: 501}},
		{Profile: profile, Page: &ipc.PageRequest{PageToken: strings.Repeat("x", 2049)}},
	} {
		_, err := s.peersAs(t.Context(), peer, request)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
}

func TestRPCPeersSearchPaginationAndFreshness(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error { cfg.CachedMap.Network.Revision = 7; return nil }); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	items := []*ipc.Peer{{Id: "b", Hostname: "Beta"}, {Id: "a", Hostname: "Alpha", OverlayAddresses: []string{"100.64.0.2"}}}
	calls := 0
	s.PeersProvider = func(context.Context) (ClientRPCPeerObservation, error) {
		calls++
		return ClientRPCPeerObservation{ProfileID: profile.ProfileId, MapRevision: 7, Peers: items}, nil
	}
	req := &ipc.ListPeersRequest{Profile: profile, Page: &ipc.PageRequest{PageSize: 1}}
	_, err := s.peersAs(t.Context(), local.Peer{Identity: "observer"}, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	if calls != 0 {
		t.Fatal("observer collected private peers")
	}
	first, err := s.peersAs(t.Context(), peer, req)
	if err != nil || len(first.Peers) != 1 || first.Peers[0].Id != "a" || first.Page.NextPageToken == "" || first.MapRevision != 7 || first.TargetMapRevision != 7 || first.SnapshotState != ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT {
		t.Fatal("wrong current peer page", err)
	}
	req.Page.PageToken = first.Page.NextPageToken
	last, err := s.peersAs(t.Context(), peer, req)
	if err != nil || last.Peers[0].Id != "b" || last.Page.NextPageToken != "" {
		t.Fatal("wrong last peer page", err)
	}
	req.Search = "alpha"
	_, err = s.peersAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	req.Page.PageToken = ""
	for _, query := range []string{" ALPHA ", "100.64.0.2"} {
		req.Search = query
		found, err := s.peersAs(t.Context(), peer, req)
		if err != nil || len(found.Peers) != 1 || found.Peers[0].Id != "a" {
			t.Fatal("peer search failed", err)
		}
	}
	req.Search = ""
	req.Page.PageToken = first.Page.NextPageToken
	items[1].Hostname = "changed"
	_, err = s.peersAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if first.Peers[0].Hostname != "Alpha" {
		t.Fatal("peer response aliases provider")
	}
}

func TestRPCPeersProviderContextAndFailure(t *testing.T) {
	for _, mode := range []string{"profile", "map-revision", "duplicate", "revision", "owner", "failure", "cancel", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.CachedMap.Network.Revision = 7
				if mode == "inactive" {
					cfg.RPCState.ActiveProfileID = ""
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s := NewClientRPCService(m, nil)
			s.PeersProvider = func(context.Context) (ClientRPCPeerObservation, error) {
				out := ClientRPCPeerObservation{ProfileID: profile.ProfileId, MapRevision: 7, Peers: []*ipc.Peer{{Id: "a"}}}
				switch mode {
				case "inactive":
					t.Fatal("inactive profile inspected engine")
				case "profile":
					out.ProfileID = "other"
				case "map-revision":
					out.MapRevision++
				case "duplicate":
					out.Peers = append(out.Peers, out.Peers[0])
				case "revision", "owner":
					if err := m.store.Update(func(cfg *Config) error {
						if mode == "owner" {
							cfg.LocalOwnerID = "replacement"
						} else {
							cfg.RPCState.Revision++
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				case "failure":
					return out, errors.New("private source detail")
				case "cancel":
					cancel()
				}
				return out, nil
			}
			result, err := s.peersAs(ctx, peer, &ipc.ListPeersRequest{Profile: profile})
			if result != nil {
				t.Fatal("invalid provider result exposed")
			}
			code := ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
			switch mode {
			case "profile", "map-revision", "revision":
				code = ipc.ErrorCode_ERROR_CODE_STALE_STATE
			case "owner":
				code = ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED
			case "inactive":
				code = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
			case "cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			assertRPCFailure(t, err, code)
		})
	}
}
