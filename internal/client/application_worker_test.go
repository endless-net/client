package client

import (
	"context"
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

type applicationWorkerStage struct {
	dns                         bool
	started, cancelled, release chan struct{}
	requests                    atomic.Int32
	closed                      atomic.Bool
}

func (s *applicationWorkerStage) wait(ctx context.Context) {
	close(s.started)
	<-ctx.Done()
	close(s.cancelled)
	<-s.release
}

func (s *applicationWorkerStage) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	if s.dns {
		s.wait(ctx)
		return nil, ctx.Err()
	}
	return []netip.Addr{netip.MustParseAddr("10.1.2.3")}, nil
}

func (s *applicationWorkerStage) RoundTrip(r *http.Request) (*http.Response, error) {
	s.requests.Add(1)
	defer func() {
		if r.Body != nil {
			_ = r.Body.Close()
		}
	}()
	s.wait(r.Context())
	return nil, r.Context().Err()
}

func (s *applicationWorkerStage) CloseIdleConnections() { s.closed.Store(true) }

func TestApplicationContextChangeJoinsDiscoveryAndReport(t *testing.T) {
	for _, dns := range []bool{true, false} {
		name := "HTTP"
		if dns {
			name = "DNS"
		}
		t.Run(name, func(t *testing.T) {
			cfg, network, _ := signedApplicationFixture(t, true)
			cfg.ControlPlaneURLs = []string{"https://control.example"}
			stage := &applicationWorkerStage{dns: dns, started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(stage.release) }) }
			engine := &WireGuardEngine{}
			engine.startApplicationReportsLocked(cfg, network, stage, &http.Client{Transport: stage})
			done := engine.applicationDone
			defer func() { release(); engine.stopApplicationReportsLocked() }()
			select {
			case <-stage.started:
			case <-time.After(5 * time.Second):
				t.Fatal("worker did not reach active stage")
			}
			changed := make(chan struct{})
			go func() { engine.configureApplicationsLocked(Config{}, clientapi.RegisterNodeResponse{}); close(changed) }()
			select {
			case <-stage.cancelled:
			case <-time.After(5 * time.Second):
				t.Fatal("context change did not cancel active stage")
			}
			select {
			case <-changed:
				t.Fatal("context changed before old worker completed")
			default:
			}
			release()
			select {
			case <-changed:
			case <-time.After(5 * time.Second):
				t.Fatal("context change did not join worker")
			}
			select {
			case <-done:
			default:
				t.Fatal("worker completion was not recorded")
			}
			if !stage.closed.Load() || engine.applicationCancel != nil || engine.applicationDone != nil {
				t.Fatal("old worker transport or ownership retained")
			}
			if dns && stage.requests.Load() != 0 {
				t.Fatal("cancelled DNS produced a report")
			}
		})
	}
}
