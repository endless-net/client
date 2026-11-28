package localapi

import (
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestDNSDiagnosticsBoundToOwnerProfileAndNode(t *testing.T) {
	c, _, _, _, owner := connectionFixture(t)
	ctx := t.Context()
	if _, err := c.connect(ctx, owner, &clientv0.ConnectRequest{Mutation: connectionMutation(3), Async: true}); err != nil {
		t.Fatal(err)
	}
	source := &observedConnection{value: connectivity.Observation{Profile: defaultID, Generation: 1, Intent: state.IntentConnected}}
	c.config.ConnectionObservations = source
	if _, err := c.diagnostics(ctx, localipc.Principal{SID: "other", Administrator: true}, nil); status.Code(err) != codes.PermissionDenied || source.calls != 0 {
		t.Fatal("foreign owner read diagnostics", err)
	}
	if _, err := c.diagnostics(ctx, owner, &clientv0.GetDiagnosticsRequest{Profile: &clientv0.ProfileRef{ProfileId: "other"}}); status.Code(err) != codes.NotFound || source.calls != 0 {
		t.Fatal("foreign profile read diagnostics", err)
	}
	result := func() *clientv0.DnsApplication {
		t.Helper()
		out, err := c.diagnostics(ctx, owner, nil)
		if err != nil || out.GetDiagnostics().GetStatus().GetActiveProfileId() != defaultID {
			t.Fatal("status and DNS snapshot", err)
		}
		return out.Diagnostics.GetDns().GetApplication()
	}
	if result() != nil {
		t.Fatal("fabricated initial DNS state")
	}
	prior := &clientv0.DnsApplication{AccountId: "account", NetworkId: "network", NodeId: "node", TargetMapRevision: 9, LastAppliedMapRevision: proto.Uint64(8), State: clientv0.DnsApplyState_DNS_APPLY_STATE_FAILED, Failure: clientv0.DnsApplyFailure_DNS_APPLY_FAILURE_OS_APPLY}
	source.value.Local.DNS = prior
	got := result()
	if !proto.Equal(got, prior) {
		t.Fatal("failed update lost prior revision", got)
	}
	got.LastAppliedMapRevision = nil
	if prior.LastAppliedMapRevision == nil {
		t.Fatal("RPC modified producer's receipt")
	}
	for _, field := range []*string{&prior.AccountId, &prior.NetworkId, &prior.NodeId, &source.value.Profile} {
		saved := *field
		*field = "foreign"
		if result() != nil {
			t.Fatal("returned DNS for a different scope")
		}
		*field = saved
	}
	source.value.Generation++
	if result() != nil {
		t.Fatal("returned DNS from stale generation")
	}
}
