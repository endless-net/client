package client

import (
	"context"
	"reflect"
	"testing"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestNetworkSelectionTargetReportsPrecisePreflightFailures(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		mutate     func(*Config)
		wantReason string
	}{
		{name: "profile", mutate: func(cfg *Config) { cfg.RPCState = nil }, wantReason: "network_selection_target_profile_stale"},
		{name: "already_selected", mutate: func(cfg *Config) { cfg.NetworkID = "target" }, wantReason: "network_selection_target_already_selected"},
		{name: "origin", mutate: func(cfg *Config) { cfg.ControlPlaneURLs = []string{"https://other.test"} }, wantReason: "network_selection_target_origin_stale"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			m, _, _ := rpcPreferenceFixture(t)
			source := m.store.Read()
			source.ControlPlaneURLs = []string{source.RPCState.Profiles[source.RPCState.ActiveProfileID].ControlOrigin}
			source.ActiveAccountID, source.Token = "account", "synthetic-session"
			scenario.mutate(&source)
			calls := 0
			_, err := PrepareNetworkSelectionTarget(t.Context(), source, "target", func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
				calls++
				return nil, nil
			})
			failure := rpc.FailureFromError(err)
			if failure.GetCode() != ipc.ErrorCode_ERROR_CODE_STALE_STATE || failure.GetReasonKey() != scenario.wantReason || calls != 0 {
				t.Fatalf("preflight result: failure=%v provider_calls=%d", failure, calls)
			}
		})
	}
}

func TestNetworkSelectionTargetSeparatesOldNetworkState(t *testing.T) {
	m, _, _ := rpcPreferenceFixture(t)
	source := clonePersistentConfig(m.store.Read())
	// An administrator caller may operate an unclaimed local installation; the
	// target preserves that empty owner binding rather than inventing one.
	source.LocalOwnerID = ""
	source.ControlPlaneURLs = []string{source.RPCState.Profiles[source.RPCState.ActiveProfileID].ControlOrigin}
	source.ActiveAccountID, source.Token = "account", "synthetic-session"
	source.IdentityPrivateKey, source.PrivateKey = "synthetic-identity", "synthetic-wireguard"
	source.NodeCredential = "synthetic-old-node-credential"
	source.ResourcePreferences = map[string]bool{"old-resource": true}
	source.NetworkPreferences = &ClientNetworkPreferences{}
	source.ExitSelection = &ClientExitSelection{}
	source.EnrollmentRecovery = &EnrollmentRecovery{}
	source.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
	before := clonePersistentConfig(source)
	calls := 0
	target, err := PrepareNetworkSelectionTarget(t.Context(), source, "target", func(_ context.Context, input ClientRPCNetworksInput) ([]*ipc.Network, error) {
		calls++
		if input.AccountID != source.ActiveAccountID || input.SessionToken != source.Token || input.ControlOrigin != source.RPCState.Profiles[source.RPCState.ActiveProfileID].ControlOrigin {
			t.Fatal("catalog authorization crossed account or origin")
		}
		return []*ipc.Network{{Id: "target", AccountId: "account"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !reflect.DeepEqual(source, before) || target.NetworkID != "target" || target.ActiveAccountID != source.ActiveAccountID || target.PrivateKey != source.PrivateKey || target.IdentityPrivateKey != source.IdentityPrivateKey {
		t.Fatal("target preparation changed source or installation/account identity")
	}
	if target.NodeID != "" || target.NodeCredential != "" || target.CachedMap != nil || target.MapRevision != 0 || target.MapGlobalRevision != 0 || target.MapHash != "" || target.CachedMapSavedAt != nil || target.RPCState != nil || target.ResourcePreferences != nil || target.NetworkPreferences != nil || target.ExitSelection != nil || target.EnrollmentRecovery != nil || target.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
		t.Fatal("target retained old network state")
	}
	target.ControlPlaneURLs[0] = "https://changed.test"
	if target.MapSigningTrust != nil {
		target.MapSigningTrust.ActiveKeyID = "changed"
	}
	if !reflect.DeepEqual(source, before) {
		t.Fatal("target aliases source authority")
	}
}

func TestNetworkSelectionTargetRejectsUnauthorizedOrInvalidCatalog(t *testing.T) {
	for _, scenario := range []string{"absent", "foreign_account", "duplicate", "nil_entry", "overflow", "denied", "cancelled", "foreign_origin"} {
		t.Run(scenario, func(t *testing.T) {
			m, _, _ := rpcPreferenceFixture(t)
			source := m.store.Read()
			source.ControlPlaneURLs = []string{source.RPCState.Profiles[source.RPCState.ActiveProfileID].ControlOrigin}
			source.ActiveAccountID, source.Token = "account", "synthetic-session"
			if scenario == "foreign_origin" {
				source.ControlPlaneURLs = []string{"https://other.test"}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			target, err := PrepareNetworkSelectionTarget(ctx, source, "target", func(context.Context, ClientRPCNetworksInput) ([]*ipc.Network, error) {
				calls++
				entry := &ipc.Network{Id: "target", AccountId: "account"}
				switch scenario {
				case "absent":
					return nil, nil
				case "foreign_account":
					entry.AccountId = "other"
				case "duplicate":
					return []*ipc.Network{entry, entry}, nil
				case "nil_entry":
					return []*ipc.Network{nil}, nil
				case "overflow":
					return make([]*ipc.Network, 1001), nil
				case "denied":
					return nil, connect.NewError(connect.CodePermissionDenied, nil)
				case "cancelled":
					cancel()
				}
				return []*ipc.Network{entry}, nil
			})
			if err == nil || !reflect.DeepEqual(target, Config{}) {
				t.Fatal("invalid catalog produced registration authority")
			}
			if scenario == "foreign_origin" && calls != 0 {
				t.Fatal("session sent to foreign origin")
			}
			if scenario != "foreign_origin" && calls != 1 {
				t.Fatal("catalog failure case did not reach provider")
			}
		})
	}
}
