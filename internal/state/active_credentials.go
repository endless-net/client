package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	"google.golang.org/protobuf/proto"
)

// ActiveSession is an adopted current session, separate from the immutable
// initial login/enrollment secrets. Operation contains private producer bytes.
type ActiveSession struct {
	RequestID, PreviousSessionID, PreviousRenewalSHA256 string
	Operation                                           []byte
}

type SessionCredentials struct {
	Access, Renewal, SessionID, UserID string
	ExpiresAt, RenewalExpiresAt        time.Time
}

// ActiveNodeCredential retains the exact signed request and admitted receipt.
// It never replaces the original registration request or Node credential.
type ActiveNodeCredential struct{ Request, Receipt string }

func (*ActiveSession) String() string          { return "[private active session]" }
func (*ActiveSession) GoString() string        { return "[private active session]" }
func (*ActiveNodeCredential) String() string   { return "[private active node credential]" }
func (*ActiveNodeCredential) GoString() string { return "[private active node credential]" }
func (SessionCredentials) String() string      { return "[private session credentials]" }
func (SessionCredentials) GoString() string    { return "[private session credentials]" }

func CredentialDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (p Profile) CurrentSession() (SessionCredentials, error) {
	if p.ActiveSession == nil {
		return SessionCredentials{Access: p.Secrets.Access, Renewal: p.Secrets.Renewal, SessionID: p.Secrets.SessionID, UserID: p.Secrets.UserID, ExpiresAt: p.Secrets.SessionExpiresAt, RenewalExpiresAt: p.Secrets.RenewalExpiresAt}, nil
	}
	a := p.ActiveSession
	var op sessionv1.SessionRenewal
	if len(a.Operation) == 0 || len(a.Operation) > 65536 || proto.Unmarshal(a.Operation, &op) != nil || len(op.ProtoReflect().GetUnknown()) != 0 || sessionv1.ValidateSessionRenewal(&op, &sessionv1.RenewSessionRequest{RequestId: a.RequestID, ExpectedSessionId: a.PreviousSessionID}, nil) != nil || op.State != sessionv1.SessionRenewalState_SESSION_RENEWAL_STATE_SUCCEEDED {
		return SessionCredentials{}, ErrCorrupt
	}
	r := op.GetResult()
	if r == nil || r.Session == nil || r.Session.ExpiresAt == nil || r.RenewalAuthorization == nil || r.RenewalAuthorization.ExpiresAt == nil || r.Session.UserId != p.Secrets.UserID || a.PreviousRenewalSHA256 == "" {
		return SessionCredentials{}, ErrCorrupt
	}
	return SessionCredentials{Access: r.AccessBearer, Renewal: r.RenewalAuthorization.Bearer, SessionID: r.Session.SessionId, UserID: r.Session.UserId, ExpiresAt: r.Session.ExpiresAt.AsTime(), RenewalExpiresAt: r.RenewalAuthorization.ExpiresAt.AsTime()}, nil
}

func (p Profile) CurrentNodeCredential() (string, time.Time, error) {
	if p.ActiveNodeCredential == nil {
		if p.Attempt == nil {
			return "", time.Time{}, ErrCorrupt
		}
		return p.Secrets.NodeCredential, p.Attempt.CredentialEnds, nil
	}
	r, err := coordinator.DecodeNodeCredentialRenewalResponse([]byte(p.ActiveNodeCredential.Receipt), 65536)
	if err != nil {
		return "", time.Time{}, ErrCorrupt
	}
	return r.NodeCredential, r.ExpiresAt, nil
}

func validateActiveCredentials(p Profile) error {
	if p.ActiveSession == nil && p.ActiveNodeCredential == nil {
		return nil
	}
	if p.Attempt == nil || p.Secrets.NodeCredential == "" || p.Secrets.UserID == "" {
		return ErrCorrupt
	}
	if p.ActiveSession != nil {
		if _, err := p.CurrentSession(); err != nil {
			return err
		}
	}
	if p.ActiveNodeCredential == nil {
		return nil
	}
	a := p.ActiveNodeCredential
	request, err := profileRenewalRequest(p, a.Request)
	if err != nil {
		return ErrCorrupt
	}
	r, err := coordinator.DecodeNodeCredentialRenewalResponse([]byte(a.Receipt), 65536)
	if err != nil || r.SchemaVersion != 1 || r.IdempotencyID != request.IdempotencyID || r.RenewalBinding != coordinator.RegistrationIdentityProofBinding(request) || r.RegistrationBinding != request.RegistrationBinding || r.PreviousCredentialSHA256 != coordinator.NodeCredentialSHA256(request.NodeCredential) || r.NodeID != p.Attempt.NodeID || r.NetworkID != p.Attempt.NetworkID || r.IssuedAt.IsZero() || !r.ExpiresAt.After(r.IssuedAt) {
		return ErrCorrupt
	}
	claims, err := coordinator.DecodeNodeCredential(r.NodeCredential, 16384)
	if err != nil || claims.NodeID != r.NodeID || claims.NetworkID != r.NetworkID || !claims.ExpiresAt.Equal(r.ExpiresAt) {
		return ErrCorrupt
	}
	return nil
}

func profileRenewalRequest(p Profile, raw string) (coordinator.RegisterNodeRequest, error) {
	request, err := coordinator.DecodeRegisterNodeRequest([]byte(raw), 65536)
	if err != nil || p.Attempt == nil || coordinator.ValidateNodeCredentialRenewalRequest(request, 16384) != nil || request.AccountID != p.Attempt.AccountID || request.NetworkID != p.Attempt.NetworkID || request.IdempotencyID == p.Secrets.RegistrationID {
		return request, ErrCorrupt
	}
	initial, err := coordinator.DecodeRegisterNodeRequest([]byte(p.Secrets.RegistrationRequest), 65536)
	if err != nil || request.IdentityPublicKey != initial.IdentityPublicKey || request.PublicKey != initial.PublicKey || request.Hostname != initial.Hostname || request.DeviceFingerprint != initial.DeviceFingerprint || request.RegistrationBinding != coordinator.RegistrationIdentityProofBinding(initial) {
		return request, ErrCorrupt
	}
	return request, nil
}

// Local transitions require an exact predecessor. Fresh server/trust verification
// is performed by the control adapter before this protected-state CAS.
func activeCredentialsUpdate(before, after Profile) error {
	if before.ActiveSession != nil && after.ActiveSession == nil || before.ActiveNodeCredential != nil && after.ActiveNodeCredential == nil {
		return ErrCorrupt
	}
	if !reflect.DeepEqual(before.ActiveSession, after.ActiveSession) && after.ActiveSession != nil {
		previous, err := before.CurrentSession()
		if err != nil {
			return err
		}
		next := after.ActiveSession
		if after.SessionRefresh == nil || after.SessionRefresh.RequestID != next.RequestID || !bytes.Equal(after.SessionRefresh.Operation, next.Operation) {
			return ErrCorrupt
		}
		if next.PreviousSessionID != previous.SessionID || next.PreviousRenewalSHA256 != CredentialDigest(previous.Renewal) || before.ActiveSession != nil && next.RequestID == before.ActiveSession.RequestID {
			return ErrCorrupt
		}
	}
	if !reflect.DeepEqual(before.ActiveNodeCredential, after.ActiveNodeCredential) && after.ActiveNodeCredential != nil {
		if after.NodeRenewal == nil || after.NodeRenewal.Request != after.ActiveNodeCredential.Request || after.NodeRenewal.Response != after.ActiveNodeCredential.Receipt {
			return ErrCorrupt
		}
		previous, _, err := before.CurrentNodeCredential()
		if err != nil {
			return err
		}
		request, err := coordinator.DecodeRegisterNodeRequest([]byte(after.ActiveNodeCredential.Request), 65536)
		if err != nil || !bytes.Equal([]byte(request.NodeCredential), []byte(previous)) {
			return ErrCorrupt
		}
	}
	return nil
}
