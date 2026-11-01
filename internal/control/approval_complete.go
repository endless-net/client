package control

import (
	"context"
	"net/http"

	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// ApprovalCompletion is private daemon output. Registered is present only after
// the original device proof, approval receipt and NODE_CREDENTIAL all agree.
// The caller still needs an atomic Store commit before projecting registered.
type ApprovalCompletion struct {
	Snapshot   ApprovalSnapshot
	Registered *RegisteredResult
}

func (ApprovalCompletion) String() string   { return "[private approval completion]" }
func (ApprovalCompletion) GoString() string { return "[private approval completion]" }

// Complete sends once with the original poll bearer and no body. An approved
// response without a credential remains an observation, never a success grant.
func (a *Approval) Complete(ctx context.Context, p state.Profile, original []byte, previous ApprovalSnapshot) (ApprovalCompletion, error) {
	q, err := a.input(p, original)
	if err != nil || a.validatePrevious(q, p, previous) != nil || previous.Request.Status != "approved" && previous.Request.Status != "enrolled" {
		return ApprovalCompletion{}, ErrApprovalProtocol
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
	raw, err := a.call(ctx, p, &previous, http.MethodPost, "/nodes/enrollment-requests/"+previous.Request.ID+"/complete", previous.PollToken, nil)
	if err != nil {
		return ApprovalCompletion{}, err
	}
	v, err := coordinator.DecodeCompleteNodeEnrollmentRequestResponse(raw, a.registration.config.MaxResponseBytes)
	now := a.registration.config.Now()
	if err != nil || now.Before(previous.ObservedAt) || a.validate(q, p, v.Request, 0, now, false) != nil || !approvalSuccessor(previous.Request, v.Request) {
		return ApprovalCompletion{}, ErrApprovalProtocol
	}
	completion := ApprovalCompletion{Snapshot: ApprovalSnapshot{Request: v.Request, PollToken: previous.PollToken, ObservedAt: now}}
	if v.Request.Status == "enrolled" {
		result := v.Registration
		if result == nil || result.Network.AccountID != v.Request.AccountID || result.Network.ID != v.Request.NetworkID || result.Node.ID != v.Request.NodeID || result.Node.Hostname != q.Hostname {
			return ApprovalCompletion{}, ErrApprovalProtocol
		}
		registered, err := a.registration.verifyResult(p, q, *result, now)
		if err != nil {
			return ApprovalCompletion{}, err
		}
		completion.Registered = &registered
	} else if v.Registration != nil {
		return ApprovalCompletion{}, ErrApprovalProtocol
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
