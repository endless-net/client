package connectivity

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/testwire"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/public/v1"
	"github.com/endless-net/coordinator/api/public/v1/publicv1connect"
	"google.golang.org/protobuf/proto"
)

func TestDomainDiscoveryReportsFreshAnswersAndWithdrawals(t *testing.T) {
	f := newMapFixture(t)
	clock := &leaseClock{sample: ClockSample{Wall: f.now, Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "boot"}}
	f.config.Clock = clock
	f.config.UnavailableResourceGuards = true
	app := coordinator.Application{ID: "printer", TargetType: "domain", Target: "printer.example", PolicyHash: strings.Repeat("ab", 32), Connectors: []coordinator.ServiceHost{{NodeID: f.value.Node.ID, PublicKey: f.value.Node.PublicKey}}}
	f.value.Network.Applications = []coordinator.Application{app}
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	m, err := v.Verify(t.Context(), f.sign(t, nil, f.now, f.now.Add(20*time.Second)), nil)
	if err != nil {
		t.Fatal(err)
	}
	var reports []*wire.ReportApplicationDiscoveryRequest
	transport := transportFixture(t, func(r *http.Request) (*http.Response, error) {
		assertNodeRequest(t, r, f)
		data, err := io.ReadAll(r.Body)
		req := new(wire.ReportApplicationDiscoveryRequest)
		if err != nil || proto.Unmarshal(data, req) != nil || r.URL.Path != publicv1connect.ConnectorServiceReportApplicationDiscoveryProcedure || req.NodeId != v.node || req.ApplicationId != app.ID || req.PolicyHash != app.PolicyHash || req.TtlSeconds != 30 {
			t.Fatal("incorrect report", req, err)
		}
		reports = append(reports, req)
		return testwire.UnaryResponse(r, &wire.ReportApplicationDiscoveryResponse{})
	})
	for _, outcome := range []string{"resolved", "timeout", "not found", "map expired during lookup"} {
		count := len(reports)
		err := transport.discoverDomain(t.Context(), v, m, app, 15*time.Second, func(_ context.Context, network, name string) ([]netip.Addr, error) {
			if network != "ip4" || name != "printer.example." {
				t.Fatal(network, name)
			}
			switch outcome {
			case "timeout":
				return nil, &net.DNSError{IsTimeout: true}
			case "not found":
				return nil, &net.DNSError{IsNotFound: true}
			case "map expired during lookup":
				clock.advance(0, 20*time.Second)
			}
			return []netip.Addr{netip.MustParseAddr("10.2.0.2"), netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("10.2.0.2"), netip.MustParseAddr("10.2.0.1")}, nil
		})
		switch outcome {
		case "resolved":
			if err != nil || len(reports) != count+1 || !slices.Equal(reports[count].Addresses, []string{"10.2.0.1", "10.2.0.2"}) {
				t.Fatal(outcome, reports, err)
			}
		case "not found":
			if err != nil || len(reports) != count+1 || len(reports[count].Addresses) != 0 {
				t.Fatal(outcome, reports, err)
			}
		case "timeout":
			if err == nil || len(reports) != count {
				t.Fatal(outcome, reports, err)
			}
		default:
			if !errors.Is(err, ErrMap) || len(reports) != count {
				t.Fatal(outcome, reports, err)
			}
		}
	}
	t.Run("map renewals do not trigger a reporting loop", func(t *testing.T) {
		// Use a new verified assignment after the previous lease expired.
		f.config.Clock = &leaseClock{sample: ClockSample{Wall: f.now, Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "boot"}}
		v, err := NewVerifier(f.profile, f.config)
		if err != nil {
			t.Fatal(err)
		}
		m, err := v.Verify(t.Context(), f.sign(t, nil, f.now, f.now.Add(time.Minute)), nil)
		if err != nil {
			t.Fatal(err)
		}
		lookupCount := 0
		maps := make(chan Map)
		ticks := make(chan time.Time)
		done := make(chan struct{})
		secondLookup := make(chan struct{})
		go func() {
			defer close(done)
			transport.runDomainDiscovery(t.Context(), v, maps, 15*time.Second, ticks, func(context.Context, string, string) ([]netip.Addr, error) {
				lookupCount++
				if lookupCount == 2 {
					close(secondLookup)
				}
				return nil, &net.DNSError{IsTimeout: true}
			})
		}()
		maps <- m
		maps <- m
		ticks <- f.now
		select {
		case <-secondLookup:
		case <-time.After(time.Second):
			t.Fatal("periodic resolution missing")
		}
		close(maps)
		<-done
		if lookupCount != 2 {
			t.Fatal("initial and periodic lookups", lookupCount)
		}
	})
}
