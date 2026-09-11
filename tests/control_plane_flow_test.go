package tests

import (
	"context"
	"net/netip"
	"testing"
	"time"

	bindings "github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
)

func TestControlPlaneNativeFlowConsent(t *testing.T) {
	exerciseNativeTrafficScenario(t, false, "udp", true)
}

func checkNativeFlowConsent(t *testing.T, s *testcontrol.Server, id string, clientIP, peerIP netip.Addr, fresh func(string) bool) {
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
	grantAndObserve := func() {
		t.Helper()
		beforeGrant := len(s.Events())
		beforeReports := len(s.FlowReports())
		from, until := time.Now().Add(-time.Second), time.Now().Add(2*time.Minute)
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
				if report.NodeId == id && report.ConsentVersion == 1 && w != nil && w.Source == clientIP.String() && w.Destination == peerIP.String() && w.DestinationPort == 24001 && w.Protocol == "udp" && w.Decision == "observed" && w.Packets > 0 && w.Bytes >= 32 {
					if w.WindowStart == nil || w.WindowEnd == nil || w.WindowStart.AsTime().Before(from) || w.WindowEnd.AsTime().After(until) {
						t.Fatal("client reported traffic outside its consent interval")
					}
					for _, event := range s.Events()[beforeGrant:] {
						if event.Kind == "flow-accepted" && event.NodeID == id && event.Path == w.WindowId {
							return true
						}
					}
				}
			}
			return false
		}); err != nil {
			t.Fatal("client did not report real UDP flow metadata under consent")
		}
	}
	grantAndObserve()
	beforeRevocation := len(s.Events())
	if err := s.RevokeFlowConsent(id); err != nil {
		t.Fatal(err)
	}
	awaitPolicy("flow-policy-disabled", beforeRevocation)
	assertQuiet()
	grantAndObserve()
}
