package client

import (
	"context"
	"errors"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCNetworksProfileCatalogAndStalePages(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ActiveAccountID = "account"
		cfg.Token = "synthetic-session"
		cfg.NetworkID = "b"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	items := []*ipc.Network{{Id: "b", Name: "second", AccountId: "account"}, {Id: "a", Name: "first", AccountId: "account"}}
	calls := 0
	s.NetworksProvider = func(_ context.Context, input ClientRPCNetworksInput) ([]*ipc.Network, error) {
		calls++
		if input.AccountID != "account" || input.SessionToken != "synthetic-session" || input.ControlOrigin != "https://control.test" {
			t.Fatal("wrong profile authorization")
		}
		return items, nil
	}
	req := &ipc.ListNetworksRequest{Profile: profile, Page: &ipc.PageRequest{PageSize: 1}}
	_, err := s.networksAs(t.Context(), local.Peer{Identity: "observer"}, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	if calls != 0 {
		t.Fatal("observer fetched account catalog")
	}
	first, err := s.networksAs(t.Context(), peer, req)
	if err != nil || len(first.Networks) != 1 || first.Networks[0].Id != "a" || first.SelectedNetworkId != "b" || first.Page.NextPageToken == "" {
		t.Fatal("wrong first catalog page", err)
	}
	if first.Networks[0].Selection.Availability != ipc.Availability_AVAILABILITY_UNSUPPORTED {
		t.Fatal("unimplemented switching advertised")
	}
	req.Page.PageToken = first.Page.NextPageToken
	last, err := s.networksAs(t.Context(), peer, req)
	if err != nil || last.Networks[0].Id != "b" || last.Page.NextPageToken != "" {
		t.Fatal("wrong last page", err)
	}
	items[0].Name = "changed"
	_, err = s.networksAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if last.Networks[0].Name != "second" {
		t.Fatal("provider result aliased")
	}
	items = items[1:]
	req.Page = nil
	_, err = s.networksAs(t.Context(), peer, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
}

func TestRPCNetworksRevalidatesProviderBoundary(t *testing.T) {
	for _, mode := range []string{"missing-auth", "nil-provider", "failure", "account", "duplicate", "nil-entry", "revision", "owner", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.ActiveAccountID = "account"
				if mode != "missing-auth" {
					cfg.Token = "synthetic-session"
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.NetworksProvider = func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
				items := []*ipc.Network{{Id: "a", AccountId: "account"}}
				switch mode {
				case "missing-auth":
					t.Fatal("provider called without account authorization")
				case "failure":
					return nil, errors.New("private provider detail")
				case "account":
					items[0].AccountId = "other"
				case "duplicate":
					items = append(items, items[0])
				case "nil-entry":
					items[0] = nil
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
				case "cancel":
					cancel()
				}
				return items, nil
			}
			if mode == "nil-provider" {
				s.NetworksProvider = nil
			}
			_, err := s.networksAs(ctx, peer, &ipc.ListNetworksRequest{Profile: profile})
			code := ipc.ErrorCode_ERROR_CODE_UNAVAILABLE
			switch mode {
			case "missing-auth":
				code = ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT
			case "nil-provider":
				code = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
			case "revision":
				code = ipc.ErrorCode_ERROR_CODE_STALE_STATE
			case "owner":
				code = ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED
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
