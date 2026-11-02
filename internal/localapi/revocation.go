package localapi

import (
	"context"
	"errors"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	contract "github.com/endless-net/coordinator/api/enrollment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type RevocationSource interface {
	List(context.Context, state.Profile) ([]*clientv0.NodeRevocationTarget, error)
	Revoke(context.Context, state.Profile, state.RemoteRevocation) (contract.NodeRevocationReceipt, error)
	Get(context.Context, state.Profile, state.RemoteRevocation) (contract.NodeRevocationReceipt, error)
}

func validRevocationProfile(ref *clientv0.ProfileRef) bool {
	return ref == nil || len(ref.ProtoReflect().GetUnknown()) == 0 && (ref.ProfileId == "" || validID(ref.ProfileId))
}

func (c *Controller) revocationContext(ctx context.Context, caller localipc.Principal, ref *clientv0.ProfileRef) (state.State, state.Profile, error) {
	s, err := c.store.Load(ctx)
	if err != nil {
		return s, state.Profile{}, rpcError(err)
	}
	if err := owner(&s, caller, false); err != nil {
		return s, state.Profile{}, err
	}
	id := profileID(ref, s.ActiveProfileID)
	p, ok := s.Profiles[id]
	if !ok || id != s.ActiveProfileID || p.Attempt == nil || p.Attempt.Phase != enrollment.Registered {
		return s, p, status.Error(codes.FailedPrecondition, "registered active profile required")
	}
	if p.Secrets.Access == "" || p.Secrets.UserID == "" || !c.config.Now().Before(p.Secrets.SessionExpiresAt) {
		return s, p, targetError(control.ErrSessionDenied)
	}
	if c.config.Revocations == nil {
		return s, p, status.Error(codes.Unimplemented, "remote revocation unavailable")
	}
	return s, p, nil
}

func (c *Controller) ListNodeRevocationTargets(ctx context.Context, req *clientv0.ListNodeRevocationTargetsRequest) (*clientv0.ListNodeRevocationTargetsResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.listRevocations(ctx, caller, req)
}

func (c *Controller) listRevocations(ctx context.Context, caller localipc.Principal, req *clientv0.ListNodeRevocationTargetsRequest) (*clientv0.ListNodeRevocationTargetsResponse, error) {
	if req == nil || len(req.ProtoReflect().GetUnknown()) != 0 || !validRevocationProfile(req.Profile) {
		return nil, status.Error(codes.InvalidArgument, "revocation profile required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, p, err := c.revocationContext(ctx, caller, req.Profile)
	if err != nil {
		return nil, err
	}
	targets, err := c.config.Revocations.List(ctx, p)
	if err != nil {
		return nil, targetError(err)
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if target == nil || len(target.ProtoReflect().GetUnknown()) != 0 {
			return nil, targetError(control.ErrTargetProtocol)
		}
		r := contract.RevokeNodeRequest{AccountID: target.AccountId, NetworkID: target.NetworkId, NodeID: target.NodeId, CommandID: "selection", ExpectedRevision: target.NetworkRevision}
		if r.Validate() != nil || r.AccountID != p.Attempt.AccountID || r.NetworkID != p.Attempt.NetworkID || seen[r.NodeID] {
			return nil, targetError(control.ErrTargetProtocol)
		}
		seen[r.NodeID] = true
	}
	latest, _, err := c.revocationContext(ctx, caller, req.Profile)
	if err != nil {
		return nil, err
	}
	if latest.Revision != s.Revision {
		return nil, rpcError(state.ErrStale)
	}
	return &clientv0.ListNodeRevocationTargetsResponse{Targets: targets, Metadata: &clientv0.SnapshotMetadata{InstanceId: c.config.RuntimeID, Revision: s.Revision, GeneratedAt: timestamppb.New(c.config.Now())}}, nil
}

func revocationProjection(profile string, command state.RemoteRevocation, receipt *contract.NodeRevocationReceipt) *clientv0.NodeRevocationResult {
	r := &clientv0.NodeRevocationResult{ProfileId: profile, CommandId: command.Request.CommandID, Phase: clientv0.NodeRevocationPhase_NODE_REVOCATION_PHASE_UNKNOWN, Target: &clientv0.NodeRevocationTarget{AccountId: command.Request.AccountID, NetworkId: command.Request.NetworkID, NodeId: command.Request.NodeID, NetworkRevision: command.Request.ExpectedRevision, Hostname: command.Hostname, AssignedIp: command.AssignedIP, Status: command.TargetStatus}}
	if receipt != nil {
		r.Phase = clientv0.NodeRevocationPhase_NODE_REVOCATION_PHASE_ACCEPTED
		r.AcceptedAt = timestamppb.New(receipt.AcceptedAt)
		r.EnforceBy = timestamppb.New(receipt.EnforceBy)
		r.CommittedNetworkRevision = receipt.Revision
	}
	return r
}

func (c *Controller) RevokeNodeAccess(ctx context.Context, req *clientv0.RevokeNodeAccessRequest) (*clientv0.RevokeNodeAccessResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.revokeNode(ctx, caller, req)
}

func (c *Controller) revokeNode(ctx context.Context, caller localipc.Principal, input *clientv0.RevokeNodeAccessRequest) (*clientv0.RevokeNodeAccessResponse, error) {
	if input == nil || len(input.ProtoReflect().GetUnknown()) != 0 || !validRevocationProfile(input.Profile) || input.Mutation != nil && len(input.Mutation.ProtoReflect().GetUnknown()) != 0 || !validID(input.CommandId) || input.Target == nil || len(input.Target.ProtoReflect().GetUnknown()) != 0 {
		return nil, status.Error(codes.InvalidArgument, "exact revocation command required")
	}
	req := proto.Clone(input).(*clientv0.RevokeNodeAccessRequest)
	if err := c.mutation(req.Mutation); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s, p, err := c.revocationContext(ctx, caller, req.Profile)
	if err != nil {
		return nil, err
	}
	request := contract.RevokeNodeRequest{AccountID: req.Target.AccountId, NetworkID: req.Target.NetworkId, NodeID: req.Target.NodeId, CommandID: req.CommandId, ExpectedRevision: req.Target.NetworkRevision}
	if request.Validate() != nil || request.AccountID != p.Attempt.AccountID || request.NetworkID != p.Attempt.NetworkID {
		return nil, status.Error(codes.InvalidArgument, "revocation target differs from profile")
	}
	command, exists := p.RemoteRevocations[req.CommandId]
	if exists {
		if command.Request != request || command.CallerSID != caller.SID || command.UserID != p.Secrets.UserID || command.Hostname != req.Target.Hostname || command.AssignedIP != req.Target.AssignedIp || command.TargetStatus != req.Target.Status {
			return nil, connectionFailure(codes.Aborted, clientv0.ErrorCode_ERROR_CODE_STALE_STATE, "client.revocation.command_conflict")
		}
	} else {
		if s.Revision != req.Mutation.ExpectedRevision {
			return nil, rpcError(state.ErrStale)
		}
		command = state.RemoteRevocation{CallerSID: caller.SID, UserID: p.Secrets.UserID, ControlOrigin: p.ControlOrigin, CreatedAt: c.config.Now(), Request: request, Hostname: req.Target.Hostname, AssignedIP: req.Target.AssignedIp, TargetStatus: req.Target.Status}
		if p.RemoteRevocations == nil {
			p.RemoteRevocations = map[string]state.RemoteRevocation{}
		}
		p.RemoteRevocations[req.CommandId] = command
		if err := c.store.Update(ctx, s.Revision, func(current *state.State) error {
			if err := owner(current, caller, false); err != nil {
				return err
			}
			current.Profiles[p.ID] = p
			return nil
		}); err != nil {
			return nil, rpcError(err)
		}
	}
	result, err := c.executeRevocation(ctx, caller, p.ID, command, false)
	if err != nil {
		return nil, err
	}
	return &clientv0.RevokeNodeAccessResponse{Result: result}, nil
}

func (c *Controller) GetNodeRevocation(ctx context.Context, req *clientv0.GetNodeRevocationRequest) (*clientv0.GetNodeRevocationResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.getRevocation(ctx, caller, req)
}

func (c *Controller) getRevocation(ctx context.Context, caller localipc.Principal, req *clientv0.GetNodeRevocationRequest) (*clientv0.GetNodeRevocationResponse, error) {
	if req == nil || len(req.ProtoReflect().GetUnknown()) != 0 || !validRevocationProfile(req.Profile) || !validID(req.CommandId) {
		return nil, status.Error(codes.InvalidArgument, "original command required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, p, err := c.revocationContext(ctx, caller, req.Profile)
	if err != nil {
		return nil, err
	}
	command, ok := p.RemoteRevocations[req.CommandId]
	if !ok || command.CallerSID != caller.SID || command.UserID != p.Secrets.UserID {
		return nil, status.Error(codes.NotFound, "revocation command unavailable")
	}
	result, err := c.executeRevocation(ctx, caller, p.ID, command, true)
	if err != nil {
		return nil, err
	}
	return &clientv0.GetNodeRevocationResponse{Result: result}, nil
}

func (c *Controller) executeRevocation(ctx context.Context, caller localipc.Principal, profile string, command state.RemoteRevocation, query bool) (*clientv0.NodeRevocationResult, error) {
	_, p, err := c.revocationContext(ctx, caller, &clientv0.ProfileRef{ProfileId: profile})
	if err != nil {
		return nil, err
	}
	if command.UserID != p.Secrets.UserID {
		return nil, targetError(control.ErrSessionDenied)
	}
	var receipt contract.NodeRevocationReceipt
	if query {
		receipt, err = c.config.Revocations.Get(ctx, p, command)
	} else {
		receipt, err = c.config.Revocations.Revoke(ctx, p, command)
	}
	if err != nil {
		if errors.Is(err, control.ErrRevocationUnknown) || errors.Is(err, control.ErrTargetProtocol) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return revocationProjection(profile, command, nil), nil
		}
		if errors.Is(err, control.ErrRevocationConflict) {
			return nil, connectionFailure(codes.Aborted, clientv0.ErrorCode_ERROR_CODE_STALE_STATE, "client.revocation.command_conflict")
		}
		return nil, targetError(err)
	}
	if receipt.Match(command.Request) != nil {
		return revocationProjection(profile, command, nil), nil
	}
	s, p, err := c.revocationContext(ctx, caller, &clientv0.ProfileRef{ProfileId: profile})
	if err != nil {
		return nil, err
	}
	latest, ok := p.RemoteRevocations[command.Request.CommandID]
	if !ok || latest.Request != command.Request || latest.UserID != command.UserID {
		return nil, rpcError(state.ErrStale)
	}
	latest.Receipt = &receipt
	p.RemoteRevocations[command.Request.CommandID] = latest
	if err := c.store.Update(ctx, s.Revision, func(current *state.State) error {
		if err := owner(current, caller, false); err != nil {
			return err
		}
		current.Profiles[p.ID] = p
		return nil
	}); err != nil {
		return revocationProjection(profile, command, nil), nil
	}
	return revocationProjection(profile, command, &receipt), nil
}
