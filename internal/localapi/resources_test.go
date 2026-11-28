package localapi

import (
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestListResourcesBindsCurrentOwnerNetworkAndPathState(t *testing.T) {
	c, _, _, _, caller := connectionFixture(t)
	ctx := t.Context()
	if _, err := c.connect(ctx, caller, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	c.config.LogPageKey = [32]byte{1}
	c.config.LogResponseBytes = 65536
	o := &observedConnection{value: connectivity.Observation{Profile: defaultID, Generation: 1, Intent: state.IntentConnected, Phase: connectivity.PhaseConnected, Local: connectivity.LocalObservation{Known: true, Applied: true, Permission: connectivity.Permission{NewFlows: true}, MapRevision: 3}}}
	for _, id := range []string{"one", "two"} {
		o.value.Local.Resources = append(o.value.Local.Resources, &clientv0.Resource{Id: id, NetworkId: "network", DisplayName: "Service " + id, Kind: clientv0.ResourceKind_RESOURCE_KIND_HOST, Availability: &clientv0.Restriction{Availability: clientv0.Availability_AVAILABILITY_AVAILABLE}})
	}
	c.config.ConnectionObservations = o
	req := &clientv0.ListResourcesRequest{Page: &clientv0.PageRequest{PageSize: 1}, Search: "SERVICE", Kinds: []clientv0.ResourceKind{clientv0.ResourceKind_RESOURCE_KIND_HOST}}
	if _, err := c.listResources(ctx, localipc.Principal{SID: "other"}, req); status.Code(err) != codes.PermissionDenied {
		t.Fatal("foreign caller", err)
	}
	first, err := c.listResources(ctx, caller, req)
	if err != nil || len(first.Resources) != 1 || first.Resources[0].Id != "one" || first.Page.NextPageToken == "" {
		t.Fatal(first, err)
	}
	req.Page.PageToken = first.Page.NextPageToken
	next, err := c.listResources(ctx, caller, req)
	if err != nil || len(next.Resources) != 1 || next.Resources[0].Id != "two" || next.Page.NextPageToken != "" {
		t.Fatal(next, err)
	}
	o.value.Local.Resources[1].Availability.Availability = clientv0.Availability_AVAILABILITY_TEMPORARILY_UNAVAILABLE
	if _, err := c.listResources(ctx, caller, req); status.Code(err) != codes.Aborted {
		t.Fatal("stale continuation", err)
	}
	req.Page.PageToken = ""
	o.value.Local.Resources[0].NetworkId = "other"
	if _, err := c.listResources(ctx, caller, req); status.Code(err) != codes.Unavailable {
		t.Fatal("foreign assignment", err)
	}
	o.value.Local.Permission.NewFlows = false
	if _, err := c.listResources(ctx, caller, req); status.Code(err) != codes.Unavailable {
		t.Fatal("offline disclosure", err)
	}
}
