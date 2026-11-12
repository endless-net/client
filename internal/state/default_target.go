package state

import (
	"crypto/ed25519"
	"encoding/base64"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// DefaultTargetProgress retains original private resolver bytes, not a grant.
type DefaultTargetProgress struct {
	Request, Receipt string
	NextCallAt       time.Time
	BlockedReason    string
}

func validateDefaultTarget(p Profile) error {
	d := p.DefaultTarget
	if d == nil {
		return nil
	}
	if p.Attempt == nil || p.Attempt.BrowserApproval || len(p.Keys.IdentityPrivate) != ed25519.PrivateKeySize {
		return ErrCorrupt
	}
	r, err := coordinator.DecodeEnsureDefaultEnrollmentTargetRequest([]byte(d.Request), coordinator.DefaultTargetMaxBytes)
	if err != nil || coordinator.VerifyDefaultTargetIdentityProof(r) != nil || r.RequestID != p.Attempt.RequestID || !r.CreatedAt.Equal(p.Attempt.CreatedAt) || r.ExpiresAt.After(p.Attempt.Deadline) || r.ExpiresAt.After(p.Secrets.SessionExpiresAt) || r.SessionTokenBinding != coordinator.RegistrationSessionTokenBinding(p.Secrets.Access) || r.IdentityPublicKey != coordinator.IdentityPublicKeyPrefix+base64.RawURLEncoding.EncodeToString(p.Keys.IdentityPrivate[ed25519.SeedSize:]) {
		return ErrCorrupt
	}
	if d.Receipt != "" {
		receipt, err := coordinator.DecodeDefaultEnrollmentTarget([]byte(d.Receipt), coordinator.DefaultTargetMaxBytes)
		if err != nil || receipt.Match(r, receipt.ResolvedAt) != nil || receipt.AccountID != p.Attempt.AccountID || receipt.NetworkID != p.Attempt.NetworkID || p.Secrets.RegistrationID != r.RequestID {
			return ErrCorrupt
		}
	}
	return nil
}

func defaultTargetUpdate(before, after Profile) error {
	if before.DefaultTarget == nil {
		return nil
	}
	if after.DefaultTarget == nil || before.DefaultTarget.Request != after.DefaultTarget.Request || before.DefaultTarget.Receipt != "" && before.DefaultTarget.Receipt != after.DefaultTarget.Receipt || before.Secrets.Access != after.Secrets.Access || before.Secrets.SessionID != after.Secrets.SessionID || before.Secrets.UserID != after.Secrets.UserID || !before.Secrets.SessionExpiresAt.Equal(after.Secrets.SessionExpiresAt) {
		return ErrCorrupt
	}
	return nil
}
