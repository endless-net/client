package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestNetworkPreferencePublicAdmissionWakesWorkerAndSeparatesPending(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	request := &ipc.SetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Patch: &ipc.PreferencesPatch{AcceptDns: proto.Bool(false), AcceptRoutes: proto.Bool(false)}}
	_, err := s.acceptNetworkPreferenceOperation(func() (*ipc.Operation, error) { t.Fatal("admitted without worker"); return nil, nil })
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(ctx context.Context, cfg Config) error {
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
	}}
	done, err := s.StartProfileWorker(ctx, driver)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); <-done }()
	// The public route must reject an unauthenticated network patch as such,
	// rather than silently taking the UI-quit-only path.
	_, err = s.SetPreferences(context.Background(), connect.NewRequest(request))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	request.Mutation = rpcCreateRequest(t, m).Mutation
	op, err := s.acceptNetworkPreferenceOperation(func() (*ipc.Operation, error) { return m.setNetworkPreferencesAs(owner, request) })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("accepted patch did not wake worker")
	}
	pending, err := s.preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []*ipc.BooleanSetting{pending.Msg.Preferences.AcceptDns, pending.Msg.Preferences.AcceptRoutes} {
		if value == nil || !value.Effective || value.Requested == nil || *value.Requested || value.Control.Mutation.Availability != ipc.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE {
			t.Fatal("pending request replaced committed policy resolution", value)
		}
	}
	close(release)
	deadline := time.After(5 * time.Second)
	for {
		result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
		if err != nil {
			t.Fatal(err)
		}
		if rpcOperationTerminal(result.State) {
			if result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
				t.Fatal(result)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("worker did not commit")
		case <-time.After(time.Millisecond):
		}
	}
	applied, err := s.preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
	if err != nil || applied.Msg.Preferences.AcceptDns.Effective || applied.Msg.Preferences.AcceptRoutes.Effective || applied.Msg.Preferences.AcceptDns.Control.Source != ipc.SettingSource_SETTING_SOURCE_USER {
		t.Fatal("committed preferences not projected", err)
	}
}

func TestNetworkPreferenceProjectionManagedLockAndReset(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	opts, key := signedServiceDNSFixture(t)
	opts.NetworkMap.Network.ClientPolicy = &api.ClientPolicy{Settings: []api.ManagedClientSetting{{Key: api.ClientSettingAcceptDNS, BooleanValue: proto.Bool(false), Locked: true, Source: api.ClientPolicyDevice, PolicyID: "dns-device"}}}
	resignApplicationMap(t, &opts.NetworkMap, key)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.CachedMap, cfg.MapSigningTrust = &opts.NetworkMap, opts.SigningTrust
		cfg.NetworkPreferences = &ClientNetworkPreferences{AcceptDNS: proto.Bool(true), AcceptRoutes: proto.Bool(false)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	read := func() *ipc.Preferences {
		t.Helper()
		response, err := s.preferencesAs(owner, &ipc.GetPreferencesRequest{Profile: profile})
		if err != nil {
			t.Fatal(err)
		}
		return response.Msg.Preferences
	}
	value := read().AcceptDns
	if value.Effective || !value.GetRequested() || !value.Control.Locked || value.Control.Source != ipc.SettingSource_SETTING_SOURCE_DEVICE_POLICY || value.Control.PolicyId != "dns-device" {
		t.Fatal("managed lock/provenance lost", value)
	}
	_, err := m.resetNetworkPreferencesAs(owner, &ipc.ResetPreferencesRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, Keys: []ipc.PreferenceKey{ipc.PreferenceKey_PREFERENCE_KEY_ACCEPT_ROUTES}})
	if err != nil {
		t.Fatal(err)
	}
	value = read().AcceptRoutes
	if value.Effective || value.Requested != nil {
		t.Fatal("pending reset replaced committed value")
	}
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { t.Fatal("offline reset started tunnel"); return nil }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
	}}
	if err := m.ReconcileNetworkPreferences(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	value = read().AcceptRoutes
	if !value.Effective || value.Requested != nil || value.Control.Source != ipc.SettingSource_SETTING_SOURCE_DEFAULT {
		t.Fatal("reset did not restore baseline", value)
	}
}
