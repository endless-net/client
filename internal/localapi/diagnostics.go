package localapi

import (
	"context"
	"runtime"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func (c *Controller) GetDiagnostics(ctx context.Context, req *clientv0.GetDiagnosticsRequest) (*clientv0.GetDiagnosticsResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.diagnostics(ctx, caller, req)
}

func (c *Controller) diagnostics(ctx context.Context, caller localipc.Principal, req *clientv0.GetDiagnosticsRequest) (*clientv0.GetDiagnosticsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := c.store.Load(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	defer clearProfileKeys(s)
	if err := owner(&s, caller, false); err != nil {
		return nil, err
	}
	p := s.Profiles[s.ActiveProfileID]
	if p.ID == "" || profileID(req.GetProfile(), s.ActiveProfileID) != p.ID {
		return nil, status.Error(codes.NotFound, "active diagnostics profile required")
	}
	current, observation := c.statusAndObservation(s, caller)
	result := &clientv0.Diagnostics{Metadata: current.Metadata, Status: current, Client: proto.CloneOf(c.config.Build), OsName: runtime.GOOS, GoVersion: runtime.Version(), Failures: current.Failures}
	dns := observation.Local.DNS
	if a := p.Attempt; a != nil && dns != nil && dns.AccountId != "" && dns.NetworkId != "" && dns.NodeId != "" && dns.AccountId == a.AccountID && dns.NetworkId == a.NetworkID && dns.NodeId == a.NodeID {
		result.Dns = &clientv0.DnsDiagnostics{Application: proto.CloneOf(dns)}
	}
	return &clientv0.GetDiagnosticsResponse{Diagnostics: result}, nil
}
