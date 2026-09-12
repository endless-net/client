package tests

import (
	"context"
	"net/netip"
	"testing"
	"time"

	rpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	bindings "github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"google.golang.org/protobuf/proto"
)

func TestControlPlaneNativeFlowConsent(t *testing.T) {
	for _, family := range []string{"ipv4", "ipv6"} {
		for _, protocol := range []string{"tcp", "udp"} {
			t.Run(family+"/"+protocol, func(t *testing.T) {
				exerciseNativeTrafficScenario(t, family == "ipv6", protocol, true, "", false)
			})
		}
	}
}

func checkNativeFlowConsent(t *testing.T, s *testcontrol.Server, id, protocol string, clientIP, peerIP netip.Addr, fresh func(string) bool) {
	t.Helper()
	awaitPolicy := func(kind string, after int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		defer cancel()
		if err := testclient.Await(ctx, func() bool {
			for _, event := range s.Events()[after:] {
				if event.Kind == kind && event.NodeID == id {
					return true
				}
			}
			return false
		}); err != nil {
			t.Fatal("client did not request the expected HTTPS flow policy")
		}
	}
	reportAttempts := func() int {
		count := 0
		for _, event := range s.Events() {
			if event.Kind == "request" && event.Path == "POST "+bindings.FlowLogServiceReportFlowLogProcedure {
				count++
			}
		}
		return count
	}
	// Observe real traffic for longer than the documented ten-second window.
	// Absence is checked on raw authenticated RPC bodies, including rejections.
	assertQuiet := func() {
		t.Helper()
		before := len(s.FlowReports())
		attempts := reportAttempts()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			if !fresh("24001") {
				t.Fatal("flow consent change disrupted application traffic")
			}
			if len(s.FlowReports()) != before || reportAttempts() != attempts {
				t.Fatal("client sent flow metadata while collection was disabled")
			}
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-timer.C:
			case <-t.Context().Done():
				timer.Stop()
				t.Fatal("flow observation interrupted")
			}
		}
	}
	awaitPolicy("flow-policy-disabled", 0)
	if reportAttempts() != 0 {
		t.Fatal("client attempted flow reporting before consent")
	}
	assertQuiet()
	retriedLostWindow := func(after int) bool {
		t.Helper()
		lostID := ""
		for _, event := range s.Events()[after:] {
			if event.Kind == "flow-ack-lost" && event.NodeID == id {
				lostID = event.Path
			}
		}
		if lostID == "" {
			return false
		}
		var original *rpc.ReportFlowLogRequest
		count := 0
		for _, report := range s.FlowReports() {
			if report.NodeId != id || report.Window == nil || report.Window.WindowId != lostID {
				continue
			}
			if original == nil {
				original = report
			} else if !proto.Equal(original, report) {
				t.Fatal("client changed the flow window after losing its acknowledgement")
			}
			count++
		}
		return count >= 2
	}
	grantAndObserve := func(loseAck bool) time.Time {
		t.Helper()
		beforeGrant := len(s.Events())
		beforeReports := len(s.FlowReports())
		if loseAck {
			s.LoseNextFlowAcknowledgement()
		}
		// Leave room for the policy refresh and report retry within a short grant.
		from, until := time.Now().Add(-time.Second), time.Now().Add(50*time.Second)
		if err := s.SetFlowConsent(id, from, until); err != nil {
			t.Fatal(err)
		}
		awaitPolicy("flow-policy-granted", beforeGrant)
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		defer cancel()
		if err := testclient.Await(ctx, func() bool {
			if !fresh("24001") {
				t.Fatal("consented traffic could not reach the reference peer")
			}
			for _, report := range s.FlowReports()[beforeReports:] {
				w := report.Window
				if report.NodeId == id && report.ConsentVersion == 1 && w != nil && w.Source == clientIP.String() && w.Destination == peerIP.String() && w.DestinationPort == 24001 && w.Protocol == protocol && w.Decision == "observed" && w.Packets > 0 && w.Bytes >= 32 {
					if w.WindowStart == nil || w.WindowEnd == nil || w.WindowStart.AsTime().Before(from) || w.WindowEnd.AsTime().After(until) {
						t.Fatal("client reported traffic outside its consent interval")
					}
					for _, event := range s.Events()[beforeGrant:] {
						if event.Kind == "flow-accepted" && event.NodeID == id && event.Path == w.WindowId {
							return !loseAck || retriedLostWindow(beforeGrant)
						}
					}
				}
			}
			return false
		}); err != nil {
			t.Fatalf("client did not report real %s flow metadata under consent", protocol)
		}
		return until
	}
	grantAndObserve(true)
	beforeRevocation := len(s.Events())
	if err := s.RevokeFlowConsent(id); err != nil {
		t.Fatal(err)
	}
	awaitPolicy("flow-policy-disabled", beforeRevocation)
	assertQuiet()
	expires := grantAndObserve(false)
	// Keep returning the same policy, including its original expiry, rather
	// than replacing it with a revocation. Traffic must survive the expiry.
	ctx, cancel := context.WithDeadline(t.Context(), expires.Add(10*time.Second))
	defer cancel()
	beforeExpiry, afterExpiry := 0, 0
	if err := testclient.Await(ctx, func() bool {
		started := time.Now()
		if !fresh("24001") {
			t.Fatalf("traffic failed during consent-expiry observation: probe_started_relative_to_expiry=%s probe_finished_relative_to_expiry=%s successful_before=%d successful_after=%d", started.Sub(expires).Round(time.Millisecond), time.Since(expires).Round(time.Millisecond), beforeExpiry, afterExpiry)
		}
		if started.Before(expires) {
			beforeExpiry++
		} else {
			afterExpiry++
		}
		// Drain the documented five-second RPC deadline before measuring
		// silence; window timestamps still enforce the exact expiry below.
		return afterExpiry > 0 && time.Now().After(expires.Add(5*time.Second))
	}); err != nil {
		t.Fatal("flow consent expiry observation interrupted")
	}
	assertQuiet()
	for _, report := range s.FlowReports() {
		if report.NodeId == id && report.Window != nil && report.Window.WindowEnd != nil && report.Window.WindowEnd.AsTime().After(expires) {
			t.Fatal("client collected flow metadata beyond consent expiry")
		}
	}
	grantAndObserve(false)
}
