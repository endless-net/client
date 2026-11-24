package control

import (
	"context"

	approvalmodel "github.com/endless-net/client/internal/approval"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
)

// ApprovalCompletion is private daemon output. Registered is present only after
// the original device proof, approval receipt and NODE_CREDENTIAL all agree.
// The caller still needs an atomic Store commit before projecting registered.
type ApprovalCompletion struct {
	Snapshot   approvalmodel.Snapshot
	Registered *RegisteredResult
	trust      Announcement
}

func (ApprovalCompletion) String() string   { return "[private approval completion]" }
func (ApprovalCompletion) GoString() string { return "[private approval completion]" }

// Complete sends once with the original poll bearer and request ID. An approved
// response without a credential remains an observation, never a success grant.
func (a *Approval) Complete(ctx context.Context, p state.Profile, original []byte, previous approvalmodel.Snapshot) (ApprovalCompletion, error) {
	q, err := a.input(p, original)
	if err != nil || a.validatePrevious(q, p, previous) != nil || previous.Request.Status != "approved" && previous.Request.Status != "enrolled" {
		return ApprovalCompletion{}, approvalmodel.ErrProtocol
	}
	if err := ctx.Err(); err != nil {
		return ApprovalCompletion{}, err
	}
	remaining := a.budget(p, &previous)
	if remaining <= 0 {
		return ApprovalCompletion{}, context.DeadlineExceeded
	}
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	var proof Announcement
	var message *wire.CompleteNodeEnrollmentRequestResponse
	err = a.call(ctx, &p, &proof, &previous, func(ctx context.Context, client publicqueryv1connect.EnrollmentServiceClient) error {
		response, err := client.CompleteNodeEnrollmentRequest(ctx, bearer(&wire.CompleteNodeEnrollmentRequestRequest{RequestId: previous.Request.ID}, previous.PollToken))
		if err == nil {
			message = response.Msg
		}
		return err
	})
	if err != nil {
		return ApprovalCompletion{}, err
	}
	if !knownRPC(message.ProtoReflect()) {
		return ApprovalCompletion{}, approvalmodel.ErrProtocol
	}
	request, err := enrollmentView(message.Request)
	v := coordinator.CompleteNodeEnrollmentRequestResponse{Request: request}
	if message.Registration != nil {
		result, decodeErr := registrationResult(message.Registration)
		if decodeErr != nil {
			return ApprovalCompletion{}, decodeErr
		}
		v.Registration = &result
	}
	now := a.registration.config.Now()
	if err != nil || now.Before(previous.ObservedAt) || a.validate(q, p, v.Request, 0, now, false) != nil || !approvalSuccessor(previous.Request, v.Request) {
		return ApprovalCompletion{}, approvalmodel.ErrProtocol
	}
	completion := ApprovalCompletion{trust: proof, Snapshot: approvalmodel.Snapshot{Request: v.Request, PollToken: previous.PollToken, ObservedAt: now}}
	if v.Request.Status == "enrolled" {
		result := v.Registration
		if result == nil || result.Network.AccountID != v.Request.AccountID || result.Network.ID != v.Request.NetworkID || result.Node.ID != v.Request.NodeID || result.Node.Hostname != q.Hostname {
			return ApprovalCompletion{}, approvalmodel.ErrProtocol
		}
		registered, err := a.registration.verifyResult(p, q, *result, now)
		if err != nil {
			return ApprovalCompletion{}, err
		}
		completion.Registered = &registered
	} else if v.Registration != nil {
		return ApprovalCompletion{}, approvalmodel.ErrProtocol
	}
	// Include decoding/signature work in this attempt's bounded context, then
	// check the retained purpose bundle again at the time of acceptance.
	if err := ctx.Err(); err != nil {
		return ApprovalCompletion{}, err
	}
	if a.budget(p, &previous) <= 0 {
		return ApprovalCompletion{}, context.DeadlineExceeded
	}
	trust := Announcement{Origin: p.ControlOrigin, KeyID: p.Trust.KeyID, ID: p.Trust.AnnouncementID, Bundle: p.Trust.Bundle}
	if trust.Validate(a.registration.config.Now()) != nil {
		return ApprovalCompletion{}, ErrBootstrap
	}
	if completion.Registered != nil && !a.registration.config.Now().Before(completion.Registered.CredentialEnds) {
		return ApprovalCompletion{}, ErrRegistrationProtocol
	}
	return completion, nil
}
