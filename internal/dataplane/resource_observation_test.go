package dataplane

import (
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestResourceConfirmationAgeSurvivesStatusPolling(t *testing.T) {
	at := connectivity.ClockSample{Wall: time.Unix(1800000000, 0), Epoch: "boot", Uptime: time.Minute, UpperUptime: time.Minute}
	peer := coordinator.Peer{ID: "router", PublicKey: "key", Endpoint: "192.0.2.1:1234", EndpointGeneration: 2}
	m := coordinator.NetworkMapSnapshot{Peers: []coordinator.Peer{peer}}
	host := coordinator.ServiceHost{NodeID: peer.ID, PublicKey: peer.PublicKey}
	status := &clientv0.PeerTransportStatus{PeerId: peer.ID, SelectedPath: clientv0.PathKind_PATH_KIND_RELAY, CheckedAt: timestamppb.New(at.Wall.Add(-29 * time.Second))}
	statuses := []*clientv0.PeerTransportStatus{status}
	seen := resourceObservations(m, statuses, at, nil)
	next := at
	next.Uptime += time.Second - time.Nanosecond
	next.UpperUptime = next.Uptime
	if !seen[host].current(next) {
		t.Fatal("fresh confirmation lost")
	}
	next.UpperUptime += time.Nanosecond
	seen = resourceObservations(m, statuses, next, seen)
	if seen[host].current(next) {
		t.Fatal("poll renewed an old confirmation")
	}
	next.Wall = next.Wall.Add(time.Second)
	status.CheckedAt = timestamppb.New(next.Wall)
	seen = resourceObservations(m, statuses, next, seen)
	if !seen[host].current(next) {
		t.Fatal("new confirmation ignored")
	}
	next.Epoch = "another-boot"
	if seen[host].current(next) {
		t.Fatal("confirmation survived a clock epoch change")
	}
}
