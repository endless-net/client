package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/internal/client"
)

func TestNativeConnectPreservesEnrolledHostnameWithoutReregistering(t *testing.T) {
	var remoteCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		remoteCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	fixture := newRecoveryTestFixture(t, server.URL)
	store, err := client.OpenConfigStore(fixture.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(cfg *client.Config) error {
		cfg.EnrollmentRecovery = nil
		cfg.MapSigningTrust = testSigningTrustBundle(t, testMapSigningPublicKey(t, cfg.CachedMap.MapSignature))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := store.Read()
	wake := make(chan struct{}, 1)
	wg := &testAgentWireGuard{configure: func(cfg client.Config, networkMap api.RegisterNodeResponse) (client.WireGuardApplyResult, error) {
		if networkMap.Node.Hostname != "recovery-node" || networkMap.Node.ID != before.NodeID ||
			cfg.NodeCredential != before.NodeCredential || networkMap.Network.ID != before.NetworkID {
			t.Error("native reconnect changed enrolled hostname, identity or credential")
		}
		return client.WireGuardApplyResult{OK: true}, nil
	}}
	driver := agentRPCProfileDriver(agentIPCOptions{ConfigStore: store, WireGuard: wg, SyncWake: wake})
	for range 2 {
		if err := driver.Start(t.Context(), store.Read()); err != nil {
			t.Fatal(err)
		}
		if len(wake) != 1 {
			t.Fatal("native reconnect did not schedule separate agent synchronization")
		}
		<-wake
	}
	if wg.configureCalls != 2 || remoteCalls.Load() != 0 || !reflect.DeepEqual(before, store.Read()) {
		t.Fatal("native reconnect registered remotely or changed persisted enrollment")
	}
}
