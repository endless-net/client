//go:build windows || linux || darwin

package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCPeersLocalTransportOwnershipAndPagination(t *testing.T) {
	m := newRPCStoreTest(t)
	service := NewClientRPCService(m, nil)
	var calls atomic.Int32
	var changed atomic.Bool
	var networkCalls atomic.Int32
	var diagnosticCalls atomic.Int32
	service.DiagnosticsProvider = func(context.Context) (ClientRPCDiagnosticsObservation, error) {
		diagnosticCalls.Add(1)
		return ClientRPCDiagnosticsObservation{OSVersion: "test-os", Tunnel: WireGuardInspection{Error: "synthetic-private-driver-error"}}, nil
	}
	service.NetworksProvider = func(_ context.Context, input ClientRPCNetworksInput) ([]*ipc.Network, error) {
		networkCalls.Add(1)
		if input.AccountID != "account" || input.SessionToken != "synthetic-session" || input.ControlOrigin != "https://control.example.test" {
			return nil, fmt.Errorf("unexpected profile context")
		}
		return []*ipc.Network{{Id: "net", Name: "Network", AccountId: "account"}}, nil
	}
	service.PeersProvider = func(context.Context) (ClientRPCPeerObservation, error) {
		calls.Add(1)
		name := "Alpha"
		if changed.Load() {
			name = "Updated"
		}
		return ClientRPCPeerObservation{ProfileID: m.store.Read().RPCState.ActiveProfileID, MapRevision: 7, Peers: []*ipc.Peer{{Id: "b", Hostname: "Beta"}, {Id: "a", Hostname: name}}}, nil
	}
	endpoint := fmt.Sprintf(`\\.\pipe\endlessnet-peer-test-%d`, time.Now().UnixNano())
	if runtime.GOOS != "windows" {
		dir, err := os.MkdirTemp("/tmp", "en-peer-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Remove(dir); err != nil {
				t.Error(err)
			}
		})
		endpoint = filepath.Join(dir, "rpc.sock")
	}
	listener, err := local.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	server := local.NewServer(service.Handler())
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() {
		_ = server.Close()
		if err := <-done; err != http.ErrServerClosed {
			t.Error(err)
		}
	}()
	consumer, err := local.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	info, err := consumer.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantMissingProfile := rpcUnownedMissingProfileFailure(t, info)
	_, err = consumer.ListPeers(ctx, connect.NewRequest(&ipc.ListPeersRequest{Profile: &ipc.ProfileRef{ProfileId: "private"}}))
	assertRPCFailure(t, err, wantMissingProfile)
	if calls.Load() != 0 {
		t.Fatal("rejected missing-profile request invoked peer source")
	}
	_, err = consumer.ListNetworks(ctx, connect.NewRequest(&ipc.ListNetworksRequest{Profile: &ipc.ProfileRef{ProfileId: "private"}}))
	assertRPCFailure(t, err, wantMissingProfile)
	if networkCalls.Load() != 0 {
		t.Fatal("rejected missing-profile request invoked account network source")
	}
	_, err = consumer.GetDiagnostics(ctx, connect.NewRequest(&ipc.GetDiagnosticsRequest{Profile: &ipc.ProfileRef{ProfileId: "private"}}))
	assertRPCFailure(t, err, wantMissingProfile)
	if diagnosticCalls.Load() != 0 {
		t.Fatal("rejected missing-profile request invoked diagnostics source")
	}
	create := rpcCreateRequest(t, m)
	create.ControlOrigin = "https://control.example.test"
	created, err := consumer.CreateProfile(ctx, connect.NewRequest(create))
	if err != nil {
		t.Fatal(err)
	}
	profile := &ipc.ProfileRef{ProfileId: created.Msg.Operation.ProfileId}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.ActiveProfileID = profile.ProfileId
		cfg.ActiveAccountID = "account"
		cfg.Token = "synthetic-session"
		cfg.NetworkID = "net"
		cfg.CachedMap = &clientapi.RegisterNodeResponse{Network: clientapi.Network{Revision: 7}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	networks, err := consumer.ListNetworks(ctx, connect.NewRequest(&ipc.ListNetworksRequest{Profile: profile}))
	if err != nil {
		t.Fatal(err)
	}
	if len(networks.Msg.Networks) != 1 || networks.Msg.Networks[0].Id != "net" || networks.Msg.Networks[0].AccountId != "account" || networks.Msg.SelectedNetworkId != "net" || networks.Msg.Page.Metadata.InstanceId != m.instanceID || networks.Msg.Networks[0].Selection.Availability != ipc.Availability_AVAILABILITY_UNSUPPORTED {
		t.Fatal("native account catalog lost profile context or advertised switching")
	}
	request := &ipc.ListPeersRequest{Profile: profile, Page: &ipc.PageRequest{PageSize: 1}}
	diagnostics, err := consumer.GetDiagnostics(ctx, connect.NewRequest(&ipc.GetDiagnosticsRequest{Profile: profile}))
	if err != nil {
		t.Fatal(err)
	}
	if !diagnostics.Msg.Diagnostics.Truncated || len(diagnostics.Msg.Diagnostics.Failures) == 0 || diagnostics.Msg.Diagnostics.Metadata.InstanceId != m.instanceID || strings.Contains(diagnostics.Msg.String(), "synthetic-private-driver-error") {
		t.Fatal("native diagnostics hid partial state or leaked raw driver error")
	}
	first, err := consumer.ListPeers(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Msg.Peers) != 1 || first.Msg.Peers[0].Id != "a" || first.Msg.MapRevision != 7 || first.Msg.TargetMapRevision != 7 || first.Msg.SnapshotState != ipc.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT || first.Msg.Page.Metadata.InstanceId != m.instanceID || first.Msg.Page.NextPageToken == "" {
		t.Fatal("native peer projection lost transport metadata")
	}
	request.Page.PageToken = first.Msg.Page.NextPageToken
	last, err := consumer.ListPeers(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Msg.Peers) != 1 || last.Msg.Peers[0].Id != "b" || last.Msg.Page.NextPageToken != "" {
		t.Fatal("native continuation failed")
	}
	changed.Store(true)
	_, err = consumer.ListPeers(ctx, connect.NewRequest(request))
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	request.Page.PageToken = ""
	request.Search = " UPDATED "
	found, err := consumer.ListPeers(ctx, connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Msg.Peers) != 1 || found.Msg.Peers[0].Hostname != "Updated" {
		t.Fatal("explicit fresh search failed")
	}
	before := calls.Load()
	beforeNetworks, beforeDiagnostics := networkCalls.Load(), diagnosticCalls.Load()
	var wantProviderCalls int32
	if info.CallerAccess == ipc.Access_ACCESS_ADMINISTRATOR {
		wantProviderCalls = 1
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = "replacement-owner"; return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = consumer.ListPeers(ctx, connect.NewRequest(request))
	assertRPCAccessAfterOwnerReplacement(t, info, err)
	if calls.Load() != before+wantProviderCalls {
		t.Fatal("owner replacement produced incorrect peer provider access")
	}
	_, err = consumer.ListNetworks(ctx, connect.NewRequest(&ipc.ListNetworksRequest{Profile: profile}))
	assertRPCAccessAfterOwnerReplacement(t, info, err)
	if networkCalls.Load() != beforeNetworks+wantProviderCalls {
		t.Fatal("owner replacement produced incorrect account catalog access")
	}
	_, err = consumer.GetDiagnostics(ctx, connect.NewRequest(&ipc.GetDiagnosticsRequest{Profile: profile}))
	assertRPCAccessAfterOwnerReplacement(t, info, err)
	if diagnosticCalls.Load() != beforeDiagnostics+wantProviderCalls {
		t.Fatal("owner replacement produced incorrect diagnostics access")
	}
}
