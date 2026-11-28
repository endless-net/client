package publicationproxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/connectivity"
	api "github.com/endless-net/management/api/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type testRoutes struct {
	e        *api.PublicationExecutor
	installs int
}

func (r *testRoutes) Executor() *api.PublicationExecutor { return r.e }
func (r *testRoutes) Install(s *api.GetPublicationRoutesResponse) error {
	r.installs++
	if !proto.Equal(r.e, s.Executor) {
		return connectivity.ErrPublication
	}
	return nil
}
func (r *testRoutes) AllowsCertificate(host string) bool { return host != "missing.example" }

type publicationControl struct {
	s       *api.GetPublicationRoutesResponse
	reports []*api.ReportPublicationRoutesRequest
	badAck  bool
	lost    bool
}

func (f *publicationControl) GetPublicationRoutes(ctx context.Context, req *connect.Request[api.GetPublicationRoutesRequest]) (*connect.Response[api.GetPublicationRoutesResponse], error) {
	if _, ok := ctx.Deadline(); !ok || !proto.Equal(req.Msg.Executor, f.s.Executor) {
		return nil, connectivity.ErrConfig
	}
	return connect.NewResponse(proto.CloneOf(f.s)), nil
}
func (f *publicationControl) ReportPublicationRoutes(_ context.Context, req *connect.Request[api.ReportPublicationRoutesRequest]) (*connect.Response[api.ReportPublicationRoutesResponse], error) {
	f.reports = append(f.reports, req.Msg)
	if f.lost {
		f.lost = false
		return nil, errors.New("response lost")
	}
	generation := req.Msg.Generation
	if f.badAck {
		generation++
	}
	return connect.NewResponse(&api.ReportPublicationRoutesResponse{AcknowledgedGeneration: generation, LeaseId: req.Msg.LeaseId}), nil
}
func TestExecutorReportsFullSnapshotAndRetriesExactLostResponse(t *testing.T) {
	now := time.Now().UTC()
	routes := &testRoutes{e: &api.PublicationExecutor{AccountId: "a", NetworkId: "n", ProxyId: "p", NodeId: "node"}}
	service := &publicationControl{s: &api.GetPublicationRoutesResponse{Executor: routes.e, Generation: 1, LeaseId: "first", ValidUntil: timestamppb.New(now.Add(time.Minute))}, lost: true}
	for _, host := range []string{"docs.example", "new.example", "missing.example", "offline.example"} {
		service.s.Routes = append(service.s.Routes, &api.PublicationRoute{ServiceId: host, Hostname: host, Version: 2})
	}
	control := controlLoop{routes: routes, management: service, gate: &routeGate{next: http.NotFoundHandler()}, now: func() time.Time { return now }, probe: func(_ context.Context, host string) bool { return host == "docs.example" }, certificate: func(h *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if h.ServerName != "docs.example" && h.ServerName != "offline.example" {
			return nil, connectivity.ErrPublication
		}
		return &tls.Certificate{Leaf: &x509.Certificate{DNSNames: []string{h.ServerName}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}}, nil
	}}
	paused, stop := context.WithCancel(t.Context())
	stop()
	if partial := control.observe(paused, service.s); partial != nil {
		t.Fatal("unprobed target reported as unavailable", partial)
	}
	if control.observations[2] == nil || control.observations[0] != nil {
		t.Fatal("incomplete probe lost its position")
	}
	if full := control.observe(t.Context(), service.s); len(full) != 4 || control.observations != nil {
		t.Fatal("incomplete observation did not resume")
	}
	if err := control.update(t.Context()); err == nil {
		t.Fatal("ack loss hidden")
	}
	now = now.Add(time.Second)
	service.badAck = true
	if err := control.update(t.Context()); err == nil {
		t.Fatal("wrong generation accepted")
	}
	service.badAck = false
	if err := control.update(t.Context()); err != nil {
		t.Fatal(err)
	}
	a := service.reports[0]
	if routes.installs != 1 || len(a.Observations) != 4 || a.Observations[0].State != api.PublicationRouteState_PUBLICATION_ROUTE_STATE_SERVING || a.Observations[1].State != api.PublicationRouteState_PUBLICATION_ROUTE_STATE_WAITING_TLS || a.Observations[2].State != api.PublicationRouteState_PUBLICATION_ROUTE_STATE_TARGET_UNAVAILABLE || a.Observations[3].State != api.PublicationRouteState_PUBLICATION_ROUTE_STATE_TARGET_UNAVAILABLE {
		t.Fatal(a, routes.installs)
	}
	if !proto.Equal(a, service.reports[1]) || !proto.Equal(a, service.reports[2]) {
		t.Fatal("receipt retry changed")
	}
	service.s.Generation++
	service.s.LeaseId = "second"
	service.s.Routes = nil
	if err := control.update(t.Context()); err != nil {
		t.Fatal(err)
	}
	if routes.installs != 2 || len(service.reports[3].Observations) != 0 || service.reports[3].LeaseId != "second" {
		t.Fatal("withdrawal report", service.reports[3])
	}
}
