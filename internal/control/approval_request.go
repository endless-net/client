package control

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"

	approvalmodel "github.com/endless-net/client/internal/approval"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// ApprovalTarget is local intent, not an authorized Account/Network selection.
// A name-only request can be resolved by the approving owner on Coordinator.
type ApprovalTarget struct {
	AccountID, NetworkID, NetworkName string
}

// PrepareApproval constructs the device-only published Create request. It does
// not select an admission policy, send a request, persist state or start a worker.
// The caller must durably commit the exact bytes before the first network send.
// A previously frozen direct request cannot be converted into an approval one.
func PrepareApproval(p state.Profile, target ApprovalTarget) ([]byte, error) {
	if p.Attempt == nil || !identifier(p.Secrets.RegistrationID) || !approvalText(p.Hostname, false, false) {
		return nil, ErrRegistrationProtocol
	}
	switch p.Attempt.Phase {
	case enrollment.ChoosingContext, enrollment.Registering, enrollment.PendingApproval, enrollment.Completing:
	default:
		return nil, ErrRegistrationProtocol
	}
	if _, _, err := NativeBinding(p); err != nil {
		return nil, ErrRegistrationProtocol
	}
	if !approvalText(target.AccountID, true, false) || !approvalText(target.NetworkID, true, false) || !approvalText(target.NetworkName, true, true) || target.NetworkID == "" && target.NetworkName == "" {
		return nil, ErrRegistrationProtocol
	}
	if !p.Attempt.BrowserApproval && p.Attempt.AccountID != "" && (target.AccountID != p.Attempt.AccountID || target.NetworkID != p.Attempt.NetworkID) {
		return nil, ErrRegistrationProtocol
	}
	fingerprint, err := p.Keys.Fingerprint()
	if err != nil {
		return nil, ErrRegistrationProtocol
	}
	wg, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
	if err != nil {
		return nil, ErrRegistrationProtocol
	}
	request := coordinator.CreateNodeEnrollmentRequest{
		SchemaVersion: coordinator.SchemaVersion, IdempotencyID: p.Secrets.RegistrationID,
		AccountID: target.AccountID, NetworkID: target.NetworkID, NetworkName: target.NetworkName,
		Hostname: p.Hostname, PublicKey: base64.StdEncoding.EncodeToString(wg.PublicKey().Bytes()),
		DeviceFingerprint: hex.EncodeToString(fingerprint[:]),
	}
	if coordinator.SetRegisterNodeIdentityProof(&request, ed25519.PrivateKey(p.Keys.IdentityPrivate)) != nil {
		return nil, ErrRegistrationProtocol
	}
	raw, err := json.Marshal(request)
	if err != nil || p.Secrets.RegistrationRequest != "" && !bytes.Equal(raw, []byte(p.Secrets.RegistrationRequest)) || p.RemoteInFlight && p.Secrets.RegistrationRequest == "" {
		return nil, ErrRegistrationProtocol
	}
	return raw, nil
}

func approvalText(value string, empty, spaces bool) bool {
	return approvalmodel.ValidText(value, empty, spaces)
}

// PrepareApprovalState is a pure persistence candidate for an explicit browser
// choice. The caller must commit it atomically before setting a send marker.
// It does not select a product entrypoint, change a native request, or dispatch.
func PrepareApprovalState(p state.Profile, target ApprovalTarget, base string, now time.Time) (state.Profile, error) {
	if p.Attempt == nil || !approvalmodel.ValidBase(base) {
		return state.Profile{}, ErrApprovalProtocol
	}
	if p.Approval != nil {
		if !p.Attempt.BrowserApproval || p.Approval.Base != base {
			return state.Profile{}, ErrApprovalProtocol
		}
		if _, err := PrepareApproval(p, target); err != nil {
			return state.Profile{}, err
		}
		return p, nil
	}
	if p.Attempt.BrowserApproval || p.RemoteInFlight || p.Attempt.Uncertain || p.Secrets.RegistrationRequest != "" || p.Secrets.NodeCredential != "" {
		return state.Profile{}, ErrApprovalProtocol
	}
	raw, err := PrepareApproval(p, target)
	if err != nil {
		return state.Profile{}, err
	}
	a, err := p.Attempt.BeginBrowserApproval(target.AccountID, target.NetworkID, now)
	if err != nil {
		return state.Profile{}, err
	}
	p.Attempt = &a
	p.Secrets.RegistrationRequest = string(raw)
	p.Approval = &state.ApprovalProgress{Base: base}
	return p, nil
}
