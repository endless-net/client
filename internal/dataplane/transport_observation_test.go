package dataplane

import (
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTransportObservationFencesAndDetachedPublicFields(t *testing.T) {
	f := newCandidateFixture(t)
	sample, _ := f.clock.Sample()
	g := &transportSupervisor{lease: f.lease, statusCursor: f.verified, status: []*clientv0.PeerTransportStatus{{PeerId: "peer", SelectedPath: clientv0.PathKind_PATH_KIND_DIRECT, Engine: "amneziawg", ProtocolVersion: "3.1", TransportProfileId: "fixture-profile", TransportProfileRevision: 1, CheckedAt: timestamppb.New(sample.Wall), SearchState: clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_IDLE}}}
	value := g.transportStatuses(f.verified)
	value[0].CheckedAt.Seconds++
	if g.transportStatuses(f.verified)[0].CheckedAt.Seconds == value[0].CheckedAt.Seconds {
		t.Fatal("reader changed published timestamp")
	}
	if len(g.transportStatuses(connectivity.Map{})) != 0 {
		t.Fatal("foreign map read selected path")
	}
	f.lease.PausePackets(true)
	value = g.transportStatuses(f.verified)
	if len(value) != 1 || value[0].SelectedPath != clientv0.PathKind_PATH_KIND_UNSPECIFIED || value[0].Engine != "" || value[0].TransportProfileId != "" || value[0].TransportProfileRevision != 0 || value[0].CheckedAt != nil || value[0].ReasonKey != "client.transport.paused" {
		t.Fatal("paused path still claimed selection", value)
	}
	f.lease.PausePackets(false)
	f.lease.Revoke()
	if g.transportStatuses(f.verified)[0].SelectedPath != clientv0.PathKind_PATH_KIND_UNSPECIFIED {
		t.Fatal("revoked path stayed selected")
	}
}

func TestTransportObservationDistinguishesSignedIncompatibilityAndMissingProfiles(t *testing.T) {
	for _, mode := range []string{"empty", "disjoint", "common_wg_but_catalog_unsupported"} {
		name := "empty_catalog_with_common_capability"
		switch mode {
		case "disjoint":
			name = "no_common_advertised_capability"
		case "common_wg_but_catalog_unsupported":
			name = mode
		}
		t.Run(name, func(t *testing.T) {
			n, f := negotiationFixture(t, false)
			route, err := newPeerRoute(n.relay, f.clock, f.profile.ID, 1)
			if err != nil {
				t.Fatal(err)
			}
			if err := route.ConfirmRelay(routeRelayProof(t, n, f)); err != nil {
				t.Fatal(err)
			}
			old := f.verified
			cursor := old.Cursor()
			f.clock.advance(time.Millisecond, time.Millisecond)
			f.snapshot.Revision.Network++
			f.snapshot.Network.Revision++
			// JSON [] is explicit; nil would be a malformed producer payload.
			f.snapshot.Peers[0].Transport.Profiles = []coordinator.TransportProfileOffer{}
			f.snapshot.Peers[0].Transport.Availability = coordinator.TransportUnconfigured
			f.snapshot.Node.Transport.Endpoints = []coordinator.TransportEndpoint{}
			f.snapshot.Peers[0].Transport.PeerAdvertisement.Endpoints = []coordinator.TransportEndpoint{}
			if mode != "empty" {
				f.snapshot.Peers[0].Transport.Availability = coordinator.TransportUnsupported
			}
			switch mode {
			case "disjoint":
				f.snapshot.Node.Transport.Capabilities.Engines = []transport.Capability{{Engine: transport.WireGuard, ProtocolVersion: "1", ProfileVersion: 1}}
			case "common_wg_but_catalog_unsupported":
				f.snapshot.Node.Transport.Capabilities.Engines = []transport.Capability{{Engine: transport.WireGuard, ProtocolVersion: "1", ProfileVersion: 1}}
				f.snapshot.Peers[0].Transport.PeerAdvertisement.Capabilities.Engines = []transport.Capability{{Engine: transport.WireGuard, ProtocolVersion: "1", ProfileVersion: 1}, {Engine: transport.AmneziaWG, ProtocolVersion: "3.1", ProfileVersion: 1}}
			}
			f.refresh(t, &cursor) // Real producer canonical signature and Client verifier.
			if err := n.relay.bind.mux.Update(f.verified); err != nil {
				t.Fatal(err)
			}
			packets, _ := switchFixture(t, f)
			if err := packets.SetRoute("peer", route); err != nil {
				t.Fatal(err)
			}
			sample, _ := f.clock.Sample()
			cooldown, err := connectivity.NewRetryDelay(f.clock, f.profile.ID, 1, sample, 15*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := f.verified.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			g := &transportSupervisor{lease: f.lease, current: f.verified, snapshot: snapshot, primary: &Engine{direct: newDirectTracker()}, packets: packets,
				peers: map[string]*supervisedRelay{"peer": {channel: n.relay, route: route, checkedRelay: sample, cooldown: cooldown}}}
			g.publishTransportStatus(sample)
			status := g.transportStatuses(f.verified)
			want, reason := clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_EXHAUSTED, "client.transport.no_profiles"
			if mode != "empty" {
				want, reason = clientv0.TransportSearchState_TRANSPORT_SEARCH_STATE_UNSUPPORTED, "client.transport.capabilities_incompatible"
			}
			if len(status) != 1 || status[0].SearchState != want || status[0].ReasonKey != reason || status[0].SelectedPath != clientv0.PathKind_PATH_KIND_RELAY || status[0].NextSearchAt != nil || status[0].CheckedAt == nil {
				t.Fatal("compatibility diagnosis lost signed scope or existing Relay", status)
			}
			if route.Current() != peerRouteRelay || len(g.transportStatuses(old)) != 0 {
				t.Fatal("diagnosis changed the working route or accepted a stale map reader")
			}
			f.lease.Revoke()
			if g.transportStatuses(f.verified)[0].SelectedPath != clientv0.PathKind_PATH_KIND_UNSPECIFIED {
				t.Fatal("unsupported status retained revoked Relay selection")
			}
		})
	}
}
