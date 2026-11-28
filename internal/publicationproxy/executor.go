package publicationproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/connectivity"
	api "github.com/endless-net/management/api/v1"
	rpc "github.com/endless-net/management/api/v1/managementapiconnect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type executorRoutes interface {
	Executor() *api.PublicationExecutor
	Install(*api.GetPublicationRoutesResponse) error
	AllowsCertificate(string) bool
}

type controlLoop struct {
	routes       executorRoutes
	management   rpc.PublicationExecutorServiceClient
	gate         *routeGate
	certificate  func(*tls.ClientHelloInfo) (*tls.Certificate, error)
	probe        func(context.Context, string) bool
	observations []*api.PublicationRouteObservation
	now          func() time.Time
	snapshot     *api.GetPublicationRoutesResponse
	pending      *api.ReportPublicationRoutesRequest
}

func (c *controlLoop) update(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	fetched, err := c.management.GetPublicationRoutes(ctx, connect.NewRequest(&api.GetPublicationRoutesRequest{Executor: c.routes.Executor()}))
	cancel()
	if err != nil {
		return err
	}
	if fetched == nil || fetched.Msg == nil {
		return connectivity.ErrPublication
	}
	s := fetched.Msg
	if !proto.Equal(s, c.snapshot) {
		c.observations = nil
		if err = c.gate.replace(parent, func() error { return c.routes.Install(s) }); err != nil {
			// The gate is fenced even when install rejected a stale or malformed set.
			c.snapshot = nil
			c.pending = nil
			return err
		}
		c.snapshot = proto.CloneOf(s)
		c.pending = nil
	}
	if !c.now().Before(s.GetValidUntil().AsTime()) {
		return connectivity.ErrPublication
	}
	if c.pending == nil {
		r := &api.ReportPublicationRoutesRequest{Executor: c.routes.Executor(), Generation: s.Generation, LeaseId: s.LeaseId}
		r.Observations = c.observe(parent, s)
		if len(r.Observations) != len(s.Routes) {
			return nil
		}
		c.pending = r
	}
	ctx, cancel = context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	ack, err := c.management.ReportPublicationRoutes(ctx, connect.NewRequest(proto.CloneOf(c.pending)))
	if err != nil {
		return err
	}
	if ack == nil || ack.Msg.GetAcknowledgedGeneration() != s.Generation || ack.Msg.GetLeaseId() != s.LeaseId {
		return connectivity.ErrPublication
	}
	c.pending = nil
	return nil
}

func (c *controlLoop) observe(parent context.Context, s *api.GetPublicationRoutesResponse) []*api.PublicationRouteObservation {
	if c.observations == nil {
		c.observations = make([]*api.PublicationRouteObservation, len(s.Routes))
	}
	result := c.observations
	var ready []int
	for i, route := range s.Routes {
		if result[i] != nil {
			continue
		}
		at := c.now()
		o := &api.PublicationRouteObservation{ServiceId: route.ServiceId, Version: route.Version, ObservedAt: timestamppb.New(at), State: api.PublicationRouteState_PUBLICATION_ROUTE_STATE_TARGET_UNAVAILABLE}
		if !c.routes.AllowsCertificate(route.Hostname) {
			result[i] = o
			continue
		}
		if c.certificateUntil(route.Hostname, at).IsZero() {
			o.State = api.PublicationRouteState_PUBLICATION_ROUTE_STATE_WAITING_TLS
			result[i] = o
			continue
		}
		ready = append(ready, i)
	}
	if len(ready) == 0 {
		c.observations = nil
		return result
	}
	// Keep polling responsive on a network with many unreachable targets.
	// Resume unfinished observations only while the same lease is current.
	ctx, cancel := context.WithTimeout(parent, min(time.Second, s.ValidUntil.AsTime().Sub(c.now())))
	defer cancel()
	var started atomic.Int64
	var wg sync.WaitGroup
	for range min(8, len(ready)) {
		wg.Go(func() {
			for ctx.Err() == nil {
				n := int(started.Add(1)) - 1
				if n >= len(ready) {
					return
				}
				i := ready[n]
				route := s.Routes[i]
				host := route.Hostname
				result[i] = &api.PublicationRouteObservation{ServiceId: route.ServiceId, Version: route.Version, ObservedAt: timestamppb.New(c.now()), State: api.PublicationRouteState_PUBLICATION_ROUTE_STATE_TARGET_UNAVAILABLE}
				if !c.probe(ctx, host) {
					continue
				}
				at := c.now()
				until := c.certificateUntil(host, at)
				if !until.IsZero() && c.routes.AllowsCertificate(host) {
					result[i].ObservedAt = timestamppb.New(at)
					result[i].State = api.PublicationRouteState_PUBLICATION_ROUTE_STATE_SERVING
					result[i].CertificateValidUntil = timestamppb.New(until)
				}
			}
		})
	}
	wg.Wait()
	if int(started.Load()) < len(ready) {
		return nil
	}
	c.observations = nil
	return result
}

func (c *controlLoop) certificateUntil(host string, at time.Time) time.Time {
	cert, err := c.certificate(&tls.ClientHelloInfo{ServerName: host})
	if err != nil || cert == nil || cert.Leaf == nil || at.Before(cert.Leaf.NotBefore) || !at.Before(cert.Leaf.NotAfter) || cert.Leaf.VerifyHostname(host) != nil {
		return time.Time{}
	}
	return cert.Leaf.NotAfter
}

// Serve runs one proxy instance on an already-bound listener. The private
// Management client carries the registered workload's transport credentials.
func Serve(ctx context.Context, listener net.Listener, server *http.Server, certs *Certificates, routes *connectivity.PublicationRoutes, management rpc.PublicationExecutorServiceClient, dial func(context.Context, netip.AddrPort) (net.Conn, error)) error {
	if listener == nil || server == nil || server.Handler == nil || certs == nil || routes == nil || management == nil || dial == nil {
		return connectivity.ErrConfig
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	gate := &routeGate{next: server.Handler}
	server.Handler = gate
	control := &controlLoop{routes: routes, management: management, gate: gate, certificate: certs.GetCertificate, now: certs.now, probe: func(ctx context.Context, host string) bool { return probeAvailable(ctx, host, routes, dial) }}
	tlsDone := make(chan error, 1)
	go func() { tlsDone <- server.ServeTLS(listener, "", ""); cancel() }()
	certDone := make(chan struct{})
	go func() { defer close(certDone); certs.Run(run) }()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for run.Err() == nil {
		// A failed fetch/report retries; each admission independently checks expiry.
		_ = control.update(run)
		select {
		case <-tick.C:
		case <-run.Done():
		}
	}
	_ = server.Close()
	// Wait for handlers to exit before their socket identity table can be
	// replaced by a session that assigns the same addresses to other peers.
	drain, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	drainErr := gate.replace(drain, nil)
	finish()
	err := <-tlsDone
	<-certDone
	if drainErr != nil {
		return drainErr
	}
	if ctx.Err() != nil && errors.Is(err, http.ErrServerClosed) {
		return ctx.Err()
	}
	return err
}
