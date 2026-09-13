package tests

import (
	"fmt"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNativePeerTunnelSummaryWithholdsUntrustedDetails(t *testing.T) {
	baseline := &ipc.Status{NodeId: "private-node", ActiveProfileId: "private-profile", MapRevision: 7}
	d := &ipc.Diagnostics{
		Status:   baseline,
		Failures: []*ipc.Failure{{ReasonKey: "private-failure"}},
		Tunnel: &ipc.TunnelInspection{Ok: true, Peers: []*ipc.TunnelPeer{
			{PeerId: "private-peer", PublicKey: "private-key", Endpoint: "private-endpoint", LatestHandshake: &timestamppb.Timestamp{Seconds: 100}, ReceivedBytes: 10, TransmittedBytes: 20},
			{LatestHandshake: &timestamppb.Timestamp{Seconds: 100, Nanos: 1000000000}},
			nil,
		}},
	}
	want := "response=true node_bound=true profile_bound=true map_current=true tunnel_ok=true tunnel_failure=0 diagnostic_failures=1 peers=3 handshakes=1 receiving=1 transmitting=1"
	if got := nativePeerTunnelSummary(d, baseline); got != want {
		t.Fatalf("unexpected bounded summary: %s", got)
	}
	d.Status = &ipc.Status{NodeId: "foreign-node", ActiveProfileId: "foreign-profile", MapRevision: 6}
	d.Tunnel = &ipc.TunnelInspection{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_UNAVAILABLE, ReasonKey: "private-reason"}}
	want = fmt.Sprintf("response=true node_bound=false profile_bound=false map_current=false tunnel_ok=false tunnel_failure=%d diagnostic_failures=1 peers=0 handshakes=0 receiving=0 transmitting=0", ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	if got := nativePeerTunnelSummary(d, baseline); got != want {
		t.Fatalf("unexpected failed summary: %s", got)
	}
}

func TestNativePeerTunnelSummaryMissingResponse(t *testing.T) {
	want := "response=false node_bound=false profile_bound=false map_current=false tunnel_ok=false tunnel_failure=0 diagnostic_failures=0 peers=0 handshakes=0 receiving=0 transmitting=0"
	if got := nativePeerTunnelSummary(nil, nil); got != want {
		t.Fatalf("missing response reported as evidence: %s", got)
	}
}
