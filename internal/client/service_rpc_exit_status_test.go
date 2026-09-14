package client

import (
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitReadSeparatesDurableRequestAndUnknownEnforcement(t *testing.T) {
	for _, mode := range []string{"none", "saved", "pending_select", "pending_clear"} {
		t.Run(mode, func(t *testing.T) {
			m, owner, profile := rpcConnectFixture(t)
			if err := m.store.Update(func(cfg *Config) error {
				if mode != "none" {
					cfg.ExitSelection = &ClientExitSelection{ID: "saved-exit", Family: api.ExitFamilyIPv4Only, LAN: api.ExitLANBlock}
				}
				if mode == "pending_clear" || mode == "pending_select" {
					cfg.RPCState.ExitChange = &clientRPCExitChange{ProfileID: profile.ProfileId}
					if mode == "pending_select" {
						cfg.RPCState.ExitChange.Requested = &ClientExitSelection{ID: "next-exit", Family: api.ExitFamilyIPv6Only, LAN: api.ExitLANAllow}
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s := NewClientRPCService(m, nil)
			status, err := s.exitNodeAs(t.Context(), owner, &ipc.GetExitNodeRequest{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			if status.EffectiveExitNodeId != nil || status.FailClosed || status.EffectiveLanAccess != 0 || status.ApplyState == ipc.ApplyState_APPLY_STATE_APPLIED || status.Ipv4.EffectiveExitNodeId != nil || status.Ipv6.EffectiveExitNodeId != nil || status.Ipv4.FailClosed || status.Ipv6.FailClosed {
				t.Fatal("durable request fabricated OS enforcement", status)
			}
			want := ""
			if mode == "saved" {
				want = "saved-exit"
				if status.Ipv4.GetRequestedExitNodeId() != want || status.Ipv6.RequestedExitNodeId != nil {
					t.Fatal("wrong requested family")
				}
			}
			if mode == "pending_select" {
				want = "next-exit"
				if status.Ipv6.GetRequestedExitNodeId() != want || status.Ipv4.RequestedExitNodeId != nil {
					t.Fatal("pending family not projected")
				}
			}
			if status.GetRequestedExitNodeId() != want || status.Failure.GetCode() != ipc.ErrorCode_ERROR_CODE_UNAVAILABLE {
				t.Fatal("lost durable request or unknown state", status)
			}
			if (mode == "pending_select" || mode == "pending_clear") != (status.ApplyState == ipc.ApplyState_APPLY_STATE_PENDING) {
				t.Fatal("pending state lost")
			}
			_, err = s.exitNodeAs(t.Context(), local.Peer{Identity: "other"}, &ipc.GetExitNodeRequest{Profile: profile})
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
		})
	}
}
