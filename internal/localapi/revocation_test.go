package localapi

import (
	"context"
	"testing"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/localipc"
	"github.com/endless-net/client/internal/state"
	contract "github.com/endless-net/coordinator/api/enrollment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type revokeSource struct {
	t        *testing.T
	store    *state.Store
	requests []contract.RevokeNodeRequest
	receipt  contract.NodeRevocationReceipt
	lose     bool
	queries  int
	now      *time.Time
}

func (*revokeSource) List(context.Context, state.Profile) ([]*clientv0.NodeRevocationTarget, error) {
	return []*clientv0.NodeRevocationTarget{{AccountId: "account", NetworkId: "network", NodeId: "lost", Hostname: "same-name", NetworkRevision: 7}}, nil
}
func (r *revokeSource) Revoke(ctx context.Context, p state.Profile, c state.RemoteRevocation) (contract.NodeRevocationReceipt, error) {
	s, err := r.store.Load(ctx)
	if err != nil || s.Profiles[p.ID].RemoteRevocations[c.Request.CommandID].Request != c.Request {
		r.t.Fatal("dispatch before durable original command", err)
	}
	r.requests = append(r.requests, c.Request)
	if r.receipt.CommandID == "" {
		r.receipt = contract.NodeRevocationReceipt{AccountID: c.Request.AccountID, NetworkID: c.Request.NetworkID, NodeID: c.Request.NodeID, CommandID: c.Request.CommandID, Status: "accepted", Revision: 8, AcceptedAt: *r.now, EnforceBy: r.now.Add(contract.RevocationLimit)}
	}
	if r.lose {
		r.lose = false
		return contract.NodeRevocationReceipt{}, control.ErrRevocationUnknown
	}
	return r.receipt, nil
}
func (r *revokeSource) Get(_ context.Context, _ state.Profile, c state.RemoteRevocation) (contract.NodeRevocationReceipt, error) {
	r.queries++
	if r.receipt.CommandID == "" {
		return contract.NodeRevocationReceipt{}, control.ErrRevocationUnknown
	}
	if r.receipt.Match(c.Request) != nil {
		return contract.NodeRevocationReceipt{}, control.ErrTargetProtocol
	}
	return r.receipt, nil
}

func revocationFixture(t *testing.T) (*Controller, *memory, *revokeSource, localipc.Principal) {
	t.Helper()
	c, b, _, now := fixture(t)
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	if _, err := c.begin(t.Context(), caller, request(1)); err != nil {
		t.Fatal(err)
	}
	if err := c.store.Update(t.Context(), 2, func(s *state.State) error {
		p := s.Profiles[defaultID]
		p.Attempt.Phase = enrollment.Registered
		p.Attempt.AccountID, p.Attempt.NetworkID, p.Attempt.NodeID = "account", "network", "available"
		p.Attempt.CredentialEnds = now.Add(time.Hour)
		p.Secrets.RegistrationID = requestID
		p.Secrets.RegistrationRequest = "original"
		p.Secrets.NodeCredential = "enc_fixture"
		p.Secrets.Access, p.Secrets.UserID, p.Secrets.SessionID = "native-secret", "user", "session"
		p.Secrets.SessionExpiresAt = now.Add(time.Hour)
		s.Profiles[p.ID] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r := &revokeSource{t: t, store: c.store, now: now}
	c.config.Revocations = r
	return c, b, r, caller
}
func revokeRequest() *clientv0.RevokeNodeAccessRequest {
	return &clientv0.RevokeNodeAccessRequest{Mutation: connectionMutation(3), CommandId: "c086b75d-a2a6-4d4e-9bb8-6ce3bc0b674e", Target: &clientv0.NodeRevocationTarget{AccountId: "account", NetworkId: "network", NodeId: "lost", Hostname: "same-name", NetworkRevision: 7}}
}

func TestRemoteRevocationLostAnswerReopenAndExactRetry(t *testing.T) {
	c, _, r, caller := revocationFixture(t)
	r.lose = true
	req := revokeRequest()
	first, err := c.revokeNode(t.Context(), caller, req)
	if err != nil || first.Result.Phase != clientv0.NodeRevocationPhase_NODE_REVOCATION_PHASE_UNKNOWN || first.Result.AcceptedAt != nil || len(r.requests) != 1 {
		t.Fatal("lost answer invented success", err)
	}
	c.config.RuntimeID = "37d937c6-30aa-49cc-a621-347a37ba8c5b"
	*r.now = r.now.Add(10 * time.Minute)
	get, err := c.getRevocation(t.Context(), caller, &clientv0.GetNodeRevocationRequest{CommandId: req.CommandId})
	if err != nil || get.Result.Phase != clientv0.NodeRevocationPhase_NODE_REVOCATION_PHASE_ACCEPTED || !get.Result.AcceptedAt.AsTime().Equal(r.receipt.AcceptedAt) || r.queries != 1 || len(r.requests) != 1 {
		t.Fatal("reopen minted a new mutation/time", err)
	}
	req.Mutation.ExpectedInstanceId = c.config.RuntimeID
	if again, err := c.revokeNode(t.Context(), caller, req); err != nil || again.Result.CommandId != req.CommandId || len(r.requests) != 2 || r.requests[0] != r.requests[1] {
		t.Fatal("explicit retry changed original binding", err)
	}
	req.Target.NodeId = "other"
	if _, err := c.revokeNode(t.Context(), caller, req); status.Code(err) != codes.Aborted || len(r.requests) != 2 {
		t.Fatal("same UUID retargeted another Node", err)
	}
	if _, err := c.getRevocation(t.Context(), localipc.Principal{SID: "S-1-5-21-99"}, &clientv0.GetNodeRevocationRequest{CommandId: req.CommandId}); status.Code(err) != codes.PermissionDenied {
		t.Fatal("foreign local caller read command", err)
	}
}

func TestRemoteRevocationUncertainLocalSaveNeverDispatches(t *testing.T) {
	c, b, r, caller := revocationFixture(t)
	b.lostACK = true
	req := revokeRequest()
	if _, err := c.revokeNode(t.Context(), caller, req); err == nil || len(r.requests) != 0 {
		t.Fatal("uncertain durable save dispatched remote mutation", err)
	}
	b.lostACK = false
	if got, err := c.getRevocation(t.Context(), caller, &clientv0.GetNodeRevocationRequest{CommandId: req.CommandId}); err != nil || got.Result.Phase != clientv0.NodeRevocationPhase_NODE_REVOCATION_PHASE_UNKNOWN || len(r.requests) != 0 {
		t.Fatal("query after save loss caused mutation", err)
	}
	if _, err := c.revokeNode(t.Context(), caller, req); err != nil || len(r.requests) != 1 {
		t.Fatal("original explicit retry could not recover durable command", err)
	}
}
