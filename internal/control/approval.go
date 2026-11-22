package control

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	approvalmodel "github.com/endless-net/client/internal/approval"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
)

var ErrApprovalNotDue = errors.New("approval poll not due")

type ApprovalConfig struct {
	RegistrationConfig
	// ApprovalBase must match the explicitly configured Coordinator browser base.
	// No URL received from IPC, an error body or a redirect establishes this trust.
	ApprovalBase string
}

type Approval struct {
	registration *Registration
	base         string
}

func NewApproval(client *http.Client, announcements Announcements, config ApprovalConfig) (*Approval, error) {
	if !approvalmodel.ValidBase(config.ApprovalBase) {
		return nil, approvalmodel.ErrProtocol
	}
	r, err := NewRegistration(client, announcements, config.RegistrationConfig)
	if err != nil {
		return nil, err
	}
	return &Approval{registration: r, base: config.ApprovalBase}, nil
}

// Create sends once from the exact private replay input. The caller must save a
// validated result atomically before polling; this adapter never changes state.
func (a *Approval) Create(ctx context.Context, p state.Profile, original []byte, previous *approvalmodel.Snapshot) (approvalmodel.Snapshot, error) {
	q, err := a.input(p, original)
	if err != nil {
		return approvalmodel.Snapshot{}, err
	}
	if previous != nil && a.validatePrevious(q, p, *previous) != nil {
		return approvalmodel.Snapshot{}, approvalmodel.ErrProtocol
	}
	var message *wire.CreateNodeEnrollmentRequestResponse
	err = a.call(ctx, &p, nil, previous, func(ctx context.Context, client publicqueryv1connect.EnrollmentServiceClient) error {
		response, err := client.CreateNodeEnrollmentRequest(ctx, connect.NewRequest(&wire.CreateNodeEnrollmentRequestRequest{Proof: registrationProof(q)}))
		if err == nil {
			message = response.Msg
		}
		return err
	})
	if err != nil {
		return approvalmodel.Snapshot{}, err
	}
	if !knownRPC(message.ProtoReflect()) {
		return approvalmodel.Snapshot{}, approvalmodel.ErrProtocol
	}
	request, err := enrollmentView(message.Request)
	v := coordinator.CreateNodeEnrollmentRequestResponse{Request: request, PollToken: message.PollToken, PollAfterSeconds: int(message.PollAfterSeconds)}
	now := a.registration.config.Now()
	if err != nil || !approvalToken(v.PollToken) || a.validate(q, p, v.Request, v.PollAfterSeconds, now, true) != nil {
		return approvalmodel.Snapshot{}, approvalmodel.ErrProtocol
	}
	if previous != nil && (now.Before(previous.ObservedAt) || previous.PollToken != v.PollToken || !approvalSuccessor(previous.Request, v.Request)) {
		return approvalmodel.Snapshot{}, approvalmodel.ErrProtocol
	}
	return approvalmodel.Snapshot{Request: v.Request, PollToken: v.PollToken, PollAfterSeconds: v.PollAfterSeconds, ObservedAt: now}, nil
}

// Poll uses only the saved poll bearer and ID. It never retries, creates a new
// request, follows an approval URL or treats approved as registered.
func (a *Approval) Poll(ctx context.Context, p state.Profile, original []byte, previous approvalmodel.Snapshot) (approvalmodel.Snapshot, error) {
	q, err := a.input(p, original)
	if err != nil || a.validatePrevious(q, p, previous) != nil {
		return approvalmodel.Snapshot{}, approvalmodel.ErrProtocol
	}
	if previous.Request.Status == "pending" && a.registration.config.Now().Before(previous.ObservedAt.Add(time.Duration(previous.PollAfterSeconds)*time.Second)) {
		return approvalmodel.Snapshot{}, ErrApprovalNotDue
	}
	var message *wire.GetNodeEnrollmentRequestResponse
	err = a.call(ctx, &p, nil, &previous, func(ctx context.Context, client publicqueryv1connect.EnrollmentServiceClient) error {
		response, err := client.GetNodeEnrollmentRequest(ctx, bearer(&wire.GetNodeEnrollmentRequestRequest{RequestId: previous.Request.ID}, previous.PollToken))
		if err == nil {
			message = response.Msg
		}
		return err
	})
	if err != nil {
		return approvalmodel.Snapshot{}, err
	}
	if !knownRPC(message.ProtoReflect()) {
		return approvalmodel.Snapshot{}, approvalmodel.ErrProtocol
	}
	request, err := enrollmentView(message.Request)
	v := coordinator.NodeEnrollmentRequestStatusResponse{Request: request, PollAfterSeconds: int(message.PollAfterSeconds)}
	now := a.registration.config.Now()
	if err != nil || now.Before(previous.ObservedAt) || a.validate(q, p, v.Request, v.PollAfterSeconds, now, false) != nil || !approvalSuccessor(previous.Request, v.Request) {
		return approvalmodel.Snapshot{}, approvalmodel.ErrProtocol
	}
	return approvalmodel.Snapshot{Request: v.Request, PollToken: previous.PollToken, PollAfterSeconds: v.PollAfterSeconds, ObservedAt: now}, nil
}

func (a *Approval) validatePrevious(q coordinator.CreateNodeEnrollmentRequest, p state.Profile, previous approvalmodel.Snapshot) error {
	if !approvalToken(previous.PollToken) || previous.ObservedAt.IsZero() || previous.ObservedAt.After(a.registration.config.Now()) || previous.ObservedAt.Before(previous.Request.CreatedAt) {
		return approvalmodel.ErrProtocol
	}
	return a.validate(q, p, previous.Request, previous.PollAfterSeconds, previous.ObservedAt, previous.PollAfterSeconds > 0)
}

func (a *Approval) input(p state.Profile, original []byte) (coordinator.CreateNodeEnrollmentRequest, error) {
	r := a.registration
	q, err := coordinator.DecodeCreateNodeEnrollmentRequest(original, r.config.MaxRequestBytes)
	if err != nil || !p.RemoteInFlight || p.InFlightAt.IsZero() || !bytes.Equal(original, []byte(p.Secrets.RegistrationRequest)) {
		return coordinator.CreateNodeEnrollmentRequest{}, approvalmodel.ErrProtocol
	}
	expected, err := PrepareApproval(p, ApprovalTarget{AccountID: q.AccountID, NetworkID: q.NetworkID, NetworkName: q.NetworkName})
	if err != nil || !bytes.Equal(original, expected) || p.InFlightAt.Before(p.Attempt.CreatedAt) || p.InFlightAt.After(r.config.Now()) || r.config.Now().Before(p.Attempt.ObservedAt) {
		return coordinator.CreateNodeEnrollmentRequest{}, approvalmodel.ErrProtocol
	}
	return q, nil
}

func (a *Approval) budget(p state.Profile, previous *approvalmodel.Snapshot) time.Duration {
	now := a.registration.config.Now()
	deadline := p.Attempt.Deadline
	if p.Attempt.Uncertain {
		if p.Attempt.RecoveryUntil.Before(deadline) {
			deadline = p.Attempt.RecoveryUntil
		}
	} else {
		for _, end := range []time.Time{p.Attempt.ApprovalUntil, approvalExpiry(previous)} {
			if !end.IsZero() && end.Before(deadline) {
				deadline = end
			}
		}
	}
	return min(30*time.Second, deadline.Sub(now))
}

func approvalExpiry(s *approvalmodel.Snapshot) time.Time {
	if s == nil {
		return time.Time{}
	}
	return s.Request.ExpiresAt
}

func (a *Approval) call(ctx context.Context, p *state.Profile, proof *Announcement, previous *approvalmodel.Snapshot, send func(context.Context, publicqueryv1connect.EnrollmentServiceClient) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remaining := a.budget(*p, previous)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	r := a.registration
	current, err := r.announcements.Fetch(ctx, p.ControlOrigin)
	if err != nil || !current.MatchesProfile(*p, r.config.Now()) {
		return ErrBootstrap
	}
	p.Trust = state.ServerTrust{KeyID: current.KeyID, AnnouncementID: current.ID, Bundle: current.Bundle}
	if a.budget(*p, previous) <= 0 {
		return context.DeadlineExceeded
	}
	if err := send(ctx, r.rpc(p.ControlOrigin)); err != nil {
		return registrationCallError(ctx)
	}
	if ctx.Err() != nil {
		return registrationCallError(ctx)
	}
	if a.budget(*p, previous) <= 0 {
		return context.DeadlineExceeded
	}
	if !current.MatchesProfile(*p, r.config.Now()) {
		return ErrBootstrap
	}
	if proof != nil {
		*proof = current
	}
	return nil
}

func approvalToken(token string) bool { return approvalmodel.ValidToken(token) }
func (a *Approval) validate(q coordinator.CreateNodeEnrollmentRequest, p state.Profile, v coordinator.NodeEnrollmentRequest, hint int, now time.Time, create bool) error {
	return approvalmodel.ValidateView(q, v, a.base, p.Attempt.CreatedAt, hint, now, create)
}
func approvalSuccessor(old, next coordinator.NodeEnrollmentRequest) bool {
	return approvalmodel.Successor(old, next)
}

func enrollmentView(m *wire.NodeEnrollmentRequest) (coordinator.NodeEnrollmentRequest, error) {
	if m == nil {
		return coordinator.NodeEnrollmentRequest{}, approvalmodel.ErrProtocol
	}
	d := mapDecoder{}
	value := coordinator.NodeEnrollmentRequest{
		ID: m.Id, Status: m.Status, AccountID: m.AccountId, NetworkID: m.NetworkId,
		NetworkName: m.NetworkName, Hostname: m.Hostname, PublicKey: m.PublicKey,
		IdentityPublicKey: m.IdentityPublicKey, DeviceFingerprint: m.DeviceFingerprint, Tags: m.Tags,
		ApprovalURL: m.ApprovalUrl, ApprovedByUserID: m.ApprovedByUserId, RejectedByUserID: m.RejectedByUserId,
		RejectionReason: m.RejectionReason, NodeID: m.NodeId, CreatedAt: d.timestamp(m.CreatedAt),
		ExpiresAt: d.timestamp(m.ExpiresAt), ApprovedAt: d.optionalTime(m.ApprovedAt),
		RejectedAt: d.optionalTime(m.RejectedAt), CompletedAt: d.optionalTime(m.CompletedAt),
	}
	if d.err != nil {
		return coordinator.NodeEnrollmentRequest{}, approvalmodel.ErrProtocol
	}
	return value, nil
}
