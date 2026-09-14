package client

import (
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestPreferenceWorkerAbsencePreservesContextRestriction(t *testing.T) {
	for _, mode := range []string{"idle", "pending", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			m, owner, ref := rpcPreferenceFixture(t)
			want := "preference_worker_unavailable"
			if mode == "pending" {
				_, err := m.setNetworkPreferencesAs(owner, &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: ref, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false)}})
				if err != nil {
					t.Fatal(err)
				}
				want = "preference_requires_idle_active_profile"
			}
			if mode == "inactive" {
				if err := m.store.Update(func(cfg *Config) error {
					profile := cfg.RPCState.Profiles[ref.ProfileId]
					profile.Configuration = profileConfiguration(*cfg)
					cfg.RPCState.Profiles[ref.ProfileId] = profile
					cfg.RPCState.ActiveProfileID = ""
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				want = "preference_requires_idle_active_profile"
			}
			response, err := NewClientRPCService(m, nil).preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: ref})
			if err != nil {
				t.Fatal(err)
			}
			for _, setting := range []*ipc.BooleanSetting{response.Msg.Preferences.AcceptDns, response.Msg.Preferences.AcceptRoutes, response.Msg.Preferences.AllowInbound} {
				if setting == nil || setting.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE || setting.Control.Mutation.ReasonKey != want {
					t.Fatal("worker absence hid profile or pending-patch restriction")
				}
			}
		})
	}
}
