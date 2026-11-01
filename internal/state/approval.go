package state

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"time"

	approvalmodel "github.com/endless-net/client/internal/approval"
	"github.com/endless-net/client/internal/enrollment"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// ApprovalProgress is protected daemon state, never an IPC/browser projection.
// A snapshot is a validated observation, not a Node credential or current grant.
type ApprovalProgress struct {
	Base          string
	Snapshot      *approvalmodel.Snapshot
	NextCallAt    time.Time
	BlockedReason string
}

func (ApprovalProgress) String() string   { return "[private approval progress]" }
func (ApprovalProgress) GoString() string { return "[private approval progress]" }

func validateApproval(p Profile) error {
	if (p.Approval != nil) != p.Attempt.BrowserApproval {
		return ErrCorrupt
	}
	if p.Approval == nil {
		return nil
	}
	v := p.Approval
	if !v.NextCallAt.IsZero() {
		end := p.Attempt.Deadline
		if p.Attempt.Uncertain && p.Attempt.RecoveryUntil.Before(end) {
			end = p.Attempt.RecoveryUntil
		}
		if !p.Attempt.Uncertain && p.Attempt.ApprovalUntil.Before(end) {
			end = p.Attempt.ApprovalUntil
		}
		if v.NextCallAt.Before(p.Attempt.CreatedAt) || v.NextCallAt.After(end) {
			return ErrCorrupt
		}
	}
	if !approvalmodel.ValidBase(v.Base) {
		return ErrCorrupt
	}
	q, err := coordinator.DecodeCreateNodeEnrollmentRequest([]byte(p.Secrets.RegistrationRequest), len(p.Secrets.RegistrationRequest))
	if err != nil || !validID(p.Secrets.RegistrationID) || q.IdempotencyID != p.Secrets.RegistrationID || q.Hostname != p.Hostname || q.JoinToken != "" || q.NodeCredential != "" || q.RegistrationBinding != "" || q.SessionTokenBinding != "" || q.NetworkID == "" && q.NetworkName == "" || coordinator.VerifyRegisterNodeIdentityProof(q) != nil {
		return ErrCorrupt
	}
	fingerprint, err := p.Keys.Fingerprint()
	if err != nil || q.DeviceFingerprint != hex.EncodeToString(fingerprint[:]) || q.IdentityPublicKey != "enp_"+base64.RawURLEncoding.EncodeToString(p.Keys.IdentityPrivate[ed25519.SeedSize:]) {
		return ErrCorrupt
	}
	wg, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
	if err != nil || q.PublicKey != base64.StdEncoding.EncodeToString(wg.PublicKey().Bytes()) {
		return ErrCorrupt
	}
	if q.AccountID != "" && p.Attempt.AccountID != "" && q.AccountID != p.Attempt.AccountID || q.NetworkID != "" && p.Attempt.NetworkID != "" && q.NetworkID != p.Attempt.NetworkID {
		return ErrCorrupt
	}
	if v.Snapshot == nil {
		if p.Attempt.Phase == enrollment.Completing || p.Attempt.Phase == enrollment.Registered {
			return ErrCorrupt
		}
		return nil
	}
	s := v.Snapshot
	if s.Validate(q, v.Base, p.Attempt.CreatedAt) != nil || s.ObservedAt.After(p.Attempt.ObservedAt) {
		return ErrCorrupt
	}
	approved := s.Request.ApprovedAt != nil
	if approved && p.Attempt.AccountID != "" && (s.Request.AccountID != p.Attempt.AccountID || s.Request.NetworkID != p.Attempt.NetworkID) {
		return ErrCorrupt
	}
	if !approved && q.NetworkID == "" && p.Attempt.AccountID != "" {
		return ErrCorrupt
	}
	if p.Attempt.Phase == enrollment.Completing && !approved {
		return ErrCorrupt
	}
	if p.Attempt.Phase == enrollment.Registered && (s.Request.Status != "enrolled" || s.Request.NodeID != p.Attempt.NodeID) {
		return ErrCorrupt
	}
	return nil
}

func approvalUpdate(old, next Profile) error {
	if old.Attempt.BrowserApproval {
		if !next.Attempt.BrowserApproval || old.Approval == nil || next.Approval == nil || old.Approval.Base != next.Approval.Base {
			return ErrCorrupt
		}
		if old.Approval.Snapshot != nil {
			a, b := old.Approval.Snapshot, next.Approval.Snapshot
			if b == nil || a.PollToken != b.PollToken || b.ObservedAt.Before(a.ObservedAt) || !approvalmodel.Successor(a.Request, b.Request) {
				return ErrCorrupt
			}
		}
	} else if next.Attempt.BrowserApproval {
		if old.RemoteInFlight || old.Secrets.RegistrationRequest != "" || old.Attempt.Uncertain || old.Attempt.Phase != enrollment.ChoosingContext && old.Attempt.Phase != enrollment.Registering {
			return ErrCorrupt
		}
	}
	return nil
}
