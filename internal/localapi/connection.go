package localapi

import (
	"context"
	"errors"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ConnectionRunner owns daemon-lifetime reconciliation. Retryable is a bounded,
// nonblocking read of an exact generation's terminal failure; it must never call
// Store or perform I/O because the controller calls it under the state lock.
// Missing/restarted/pending/applied generations are not retryable. Reconcile
// reloads and fences current intent before effects; duplicate wakes join the
// same operation. wait=false acknowledges scheduling only; wait=true succeeds
// only after the requested local state is confirmed. Cancelling that wait does
// not cancel or undo accepted intent. Operation budgets belong to the original
// durable attempt (or a separately bounded startup restoration).
type ConnectionRunner interface {
	Retryable(profile string, generation uint64) bool
	Reconcile(ctx context.Context, profile string, generation uint64, wait bool) error
}

var errConnectionInactive = errors.New("connection profile inactive")
var errConnectionUnsupported = errors.New("connection backend unavailable")
var errConnectionRegistration = errors.New("connection registration required")

func connectionFailure(code codes.Code, typed clientv0.ErrorCode, reason string) error {
	s, err := status.New(code, reason).WithDetails(&clientv0.Failure{Code: typed, ReasonKey: reason, ActionOwner: clientv0.ActionOwner_ACTION_OWNER_USER})
	if err != nil {
		return status.Error(code, reason)
	}
	return s.Err()
}

func (c *Controller) Connect(ctx context.Context, req *clientv0.ConnectRequest) (*clientv0.ConnectResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.connect(ctx, caller, req)
}

func (c *Controller) connect(ctx context.Context, caller localipc.Principal, req *clientv0.ConnectRequest) (*clientv0.ConnectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "connection required")
	}
	if _, err := c.connectionIntent(ctx, caller, req.Mutation, req.Profile, state.IntentConnected, !req.Async); err != nil {
		return nil, err
	}
	return &clientv0.ConnectResponse{}, nil
}

func (c *Controller) Disconnect(ctx context.Context, req *clientv0.DisconnectRequest) (*clientv0.DisconnectResponse, error) {
	caller, err := localipc.Caller(ctx)
	if err != nil {
		return nil, err
	}
	return c.disconnect(ctx, caller, req)
}

func (c *Controller) disconnect(ctx context.Context, caller localipc.Principal, req *clientv0.DisconnectRequest) (*clientv0.DisconnectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "disconnection required")
	}
	changed, err := c.connectionIntent(ctx, caller, req.Mutation, req.Profile, state.IntentDisconnected, true)
	if err != nil {
		return nil, err
	}
	return &clientv0.DisconnectResponse{Change: &clientv0.ChangeResult{Changed: changed}}, nil
}

func (c *Controller) connectionIntent(ctx context.Context, caller localipc.Principal, mutation *clientv0.MutationContext, ref *clientv0.ProfileRef, intent state.DesiredIntent, wait bool) (bool, error) {
	if err := c.mutation(mutation); err != nil {
		return false, err
	}
	if ref != nil && ref.ProfileId != "" && !validID(ref.ProfileId) {
		return false, status.Error(codes.InvalidArgument, "invalid profile")
	}
	budget := 30 * time.Second
	if intent == state.IntentDisconnected {
		budget = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var id string
	var generation uint64
	var changed bool
	err := c.store.Update(ctx, mutation.ExpectedRevision, func(s *state.State) error {
		if err := owner(s, caller, false); err != nil {
			return err
		}
		id = profileID(ref, s.ActiveProfileID)
		p, exists := s.Profiles[id]
		if !exists {
			return status.Error(codes.NotFound, "profile not found")
		}
		if id != s.ActiveProfileID {
			return errConnectionInactive
		}
		if c.config.Connections == nil || c.config.ConnectionClock == nil {
			return errConnectionUnsupported
		}
		if p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || p.Secrets.NodeCredential == "" {
			return errConnectionRegistration
		}
		if p.Connection == nil {
			p.Connection = &state.ConnectionState{}
		}
		if p.Connection.Intent == intent && !c.config.Connections.Retryable(id, p.Connection.Generation) {
			generation = p.Connection.Generation
			return errReplay
		}
		var err error
		sample, err := c.config.ConnectionClock.Sample()
		if err != nil || sample.Epoch != c.config.RuntimeID {
			return connectivity.ErrClock
		}
		// Validate the native receipt before committing timing evidence. This
		// samples only the counter; no Store recursion or network I/O is allowed.
		if _, err := connectivity.NewOperationBudget(c.config.ConnectionClock, id, p.Connection.Generation+1, sample, budget); err != nil {
			return err
		}
		changed, err = p.Connection.BeginRequestedAttempt(intent, sample.Wall, state.ConnectionRequest{InstanceID: c.config.RuntimeID, Revision: mutation.ExpectedRevision, ClockEpoch: sample.Epoch, Uptime: sample.Uptime, UpperUptime: sample.UpperUptime})
		if err != nil {
			return err
		}
		generation = p.Connection.Generation
		s.Profiles[id] = p
		return nil
	})
	if err != nil && !errors.Is(err, errReplay) {
		switch {
		case errors.Is(err, errConnectionInactive):
			return false, connectionFailure(codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_RESOURCE_CONFLICT, "client.connection.profile_inactive")
		case errors.Is(err, errConnectionUnsupported):
			return false, connectionFailure(codes.Unimplemented, clientv0.ErrorCode_ERROR_CODE_UNSUPPORTED, "client.connection.unsupported")
		case errors.Is(err, errConnectionRegistration):
			return false, connectionFailure(codes.FailedPrecondition, clientv0.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT, "client.connection.registration_required")
		default:
			return false, rpcError(err)
		}
	}
	// No dispatch after stale CAS, failed or uncertain commit. In particular an
	// ACK loss is not a reason to allocate another generation or renew a budget.
	if err := c.config.Connections.Reconcile(ctx, id, generation, wait); err != nil {
		return false, rpcError(err)
	}
	return changed, nil
}
