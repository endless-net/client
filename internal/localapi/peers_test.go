package localapi

import (
	"context"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListPeersRequiresOwnerAndCurrentMap(t *testing.T) {
	c, _, _, _, caller := connectionFixture(t)
	ctx := context.Background()
	if _, err := c.connect(ctx, caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	c.config.LogPageKey = [32]byte{1}
	c.config.LogResponseBytes = 65536
	observation := &observedConnection{value: connectivity.Observation{
		Profile: defaultID, Generation: 1, Intent: state.IntentConnected, Phase: connectivity.PhaseConnected,
		Local: connectivity.LocalObservation{
			Known: true, Applied: true, MapRevision: 8, PeerCount: 2,
			Permission: connectivity.Permission{NewFlows: true},
			Peers: []*clientv0.Peer{
				{Id: "first", Hostname: "Laptop", OverlayAddresses: []string{"100.64.0.8"}},
				{Id: "second", Hostname: "Desktop", OverlayAddresses: []string{"100.64.0.9"}},
			},
		},
	}}
	c.config.ConnectionObservations = observation
	req := &clientv0.ListPeersRequest{Page: &clientv0.PageRequest{PageSize: 1}}
	if _, err := c.listPeers(ctx, localipc.Principal{SID: "other", Administrator: true}, req); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign administrator read peers: %v", err)
	}
	first, err := c.listPeers(ctx, caller, req)
	if err != nil || len(first.GetPeers()) != 1 || first.Peers[0].GetHostname() != "Laptop" ||
		first.GetSnapshotState() != clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_CURRENT ||
		first.GetPage().GetNextPageToken() == "" {
		t.Fatalf("first page: %v, %v", first, err)
	}
	req.Page.PageToken = first.Page.NextPageToken
	second, err := c.listPeers(ctx, caller, req)
	if err != nil || len(second.GetPeers()) != 1 || second.Peers[0].GetId() != "second" || second.GetPage().GetNextPageToken() != "" {
		t.Fatalf("second page: %v, %v", second, err)
	}
	observation.value.Local.MapRevision++
	if _, err := c.listPeers(ctx, caller, req); status.Code(err) != codes.Aborted {
		t.Fatalf("cursor survived a map change: %v", err)
	}
	observation.value.Local.Permission.NewFlows = false
	stale, err := c.listPeers(ctx, caller, &clientv0.ListPeersRequest{})
	if err != nil || len(stale.GetPeers()) != 0 || stale.GetSnapshotState() != clientv0.AgentSnapshotState_AGENT_SNAPSHOT_STATE_PREVIOUS {
		t.Fatalf("stale map exposed peer data: %v, %v", stale, err)
	}
}
