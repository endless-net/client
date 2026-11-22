package control

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"connectrpc.com/connect"
	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	contract "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
)

var ErrRevocationUnknown = errors.New("remote revocation outcome unknown")
var ErrRevocationConflict = errors.New("remote revocation command conflicts")

type RevocationConfig struct {
	MaxBytes, MaxTargets int
	Now                  func() time.Time
}
type Revocations struct {
	http          *http.Client
	announcements Announcements
	config        RevocationConfig
}

func NewRevocations(client *http.Client, announcements Announcements, c RevocationConfig) (*Revocations, error) {
	if announcements == nil || c.Now == nil || c.MaxBytes < 1 || c.MaxTargets < 1 {
		return nil, ErrTargetProtocol
	}
	c.MaxBytes = min(c.MaxBytes, 65536)
	b, err := NewBootstrap(client, int64(c.MaxBytes), c.Now)
	if err != nil {
		return nil, err
	}
	return &Revocations{http: b.client, announcements: announcements, config: c}, nil
}

func (r *Revocations) call(ctx context.Context, p state.Profile, send func(context.Context, publicqueryv1connect.NodeManagementServiceClient) error) error {
	if p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || p.Secrets.Access == "" || p.Secrets.UserID == "" || !r.config.Now().Before(p.Secrets.SessionExpiresAt) {
		return ErrSessionDenied
	}
	ctx, cancel := context.WithTimeout(ctx, min(10*time.Second, p.Secrets.SessionExpiresAt.Sub(r.config.Now())))
	defer cancel()
	a, err := r.announcements.Fetch(ctx, p.ControlOrigin)
	if err != nil || !a.MatchesProfile(p, r.config.Now()) {
		return ErrBootstrap
	}
	client := publicqueryv1connect.NewNodeManagementServiceClient(r.http, p.ControlOrigin,
		connect.WithSendMaxBytes(r.config.MaxBytes), connect.WithReadMaxBytes(r.config.MaxBytes))
	if err := send(ctx, client); err != nil {
		switch connect.CodeOf(err) {
		case connect.CodeUnauthenticated:
			return ErrSessionDenied
		case connect.CodePermissionDenied:
			return ErrTargetDenied
		case connect.CodeAborted:
			return ErrRevocationConflict
		}
		return ErrRevocationUnknown
	}
	if ctx.Err() != nil || !r.config.Now().Before(p.Secrets.SessionExpiresAt) {
		return ErrRevocationUnknown
	}
	return nil
}
func (r *Revocations) List(ctx context.Context, p state.Profile) ([]*clientv0.NodeRevocationTarget, error) {
	if p.Attempt == nil {
		return nil, ErrSessionDenied
	}
	shape := contract.RevokeNodeRequest{AccountID: p.Attempt.AccountID, NetworkID: p.Attempt.NetworkID, NodeID: "selection", CommandID: "selection", ExpectedRevision: 1}
	if shape.Validate() != nil {
		return nil, ErrTargetProtocol
	}
	var result *wire.ListNodesResponse
	err := r.call(ctx, p, func(ctx context.Context, client publicqueryv1connect.NodeManagementServiceClient) error {
		response, err := client.ListNodes(ctx, bearer(&wire.ListNodesRequest{NetworkId: shape.NetworkID}, p.Secrets.Access))
		if err == nil {
			result = response.Msg
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if !knownRPC(result.ProtoReflect()) || result.NetworkRevision == 0 || len(result.Nodes) > r.config.MaxTargets {
		return nil, ErrTargetProtocol
	}
	nodes, revision := result.Nodes, result.NetworkRevision
	seen := make(map[string]bool, len(nodes))
	targets := make([]*clientv0.NodeRevocationTarget, 0, len(nodes))
	for _, n := range nodes {
		shape.NodeID = n.Id
		if shape.Validate() != nil || n.NetworkId != shape.NetworkID || seen[n.Id] {
			return nil, ErrTargetProtocol
		}
		seen[n.Id] = true
		targets = append(targets, &clientv0.NodeRevocationTarget{AccountId: shape.AccountID, NetworkId: shape.NetworkID, NodeId: n.Id, Hostname: n.Hostname, AssignedIp: n.AssignedIp, Status: n.Status, NetworkRevision: revision})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].NodeId < targets[j].NodeId })
	return targets, nil
}

func (r *Revocations) execute(ctx context.Context, p state.Profile, command state.RemoteRevocation, query bool) (contract.NodeRevocationReceipt, error) {
	stored, ok := p.RemoteRevocations[command.Request.CommandID]
	if !ok || stored.Request != command.Request || command.Request.Validate() != nil || p.Attempt == nil || command.UserID != p.Secrets.UserID || command.ControlOrigin != p.ControlOrigin || command.Request.AccountID != p.Attempt.AccountID || command.Request.NetworkID != p.Attempt.NetworkID {
		return contract.NodeRevocationReceipt{}, ErrTargetProtocol
	}
	var receipt *wire.NodeRevocationReceipt
	valid := false
	request := command.Request
	err := r.call(ctx, p, func(ctx context.Context, client publicqueryv1connect.NodeManagementServiceClient) error {
		if query {
			response, err := client.GetNodeRevocation(ctx, bearer(&wire.GetNodeRevocationRequest{
				NetworkId: request.NetworkID, NodeId: request.NodeID, CommandId: request.CommandID,
			}, p.Secrets.Access))
			if err == nil {
				valid, receipt = knownRPC(response.Msg.ProtoReflect()), response.Msg.GetReceipt()
			}
			return err
		}
		response, err := client.RevokeNode(ctx, bearer(&wire.RevokeNodeRequest{
			AccountId: request.AccountID, NetworkId: request.NetworkID, NodeId: request.NodeID,
			CommandId: request.CommandID, ExpectedRevision: request.ExpectedRevision,
		}, p.Secrets.Access))
		if err == nil {
			valid, receipt = knownRPC(response.Msg.ProtoReflect()), response.Msg.GetReceipt()
		}
		return err
	})
	if err != nil {
		return contract.NodeRevocationReceipt{}, err
	}
	if !valid || receipt == nil || receipt.AcceptedAt.CheckValid() != nil || receipt.EnforceBy.CheckValid() != nil {
		return contract.NodeRevocationReceipt{}, ErrTargetProtocol
	}
	result := contract.NodeRevocationReceipt{
		AccountID: receipt.AccountId, NetworkID: receipt.NetworkId, NodeID: receipt.NodeId, CommandID: receipt.CommandId,
		Status: receipt.Status, Revision: receipt.Revision, AcceptedAt: receipt.AcceptedAt.AsTime(), EnforceBy: receipt.EnforceBy.AsTime(),
	}
	if result.Match(request) != nil || r.config.Now().Before(result.AcceptedAt) {
		return contract.NodeRevocationReceipt{}, ErrTargetProtocol
	}
	return result, nil
}

func (r *Revocations) Revoke(ctx context.Context, p state.Profile, c state.RemoteRevocation) (contract.NodeRevocationReceipt, error) {
	return r.execute(ctx, p, c, false)
}
func (r *Revocations) Get(ctx context.Context, p state.Profile, c state.RemoteRevocation) (contract.NodeRevocationReceipt, error) {
	return r.execute(ctx, p, c, true)
}
