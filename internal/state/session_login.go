package state

import (
	"bytes"
	"reflect"
	"strings"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	identityapi "github.com/endless-net/identity/api/v1"
	"google.golang.org/protobuf/proto"
)

type SessionLoginPhase string

const (
	SessionLoginPreparing SessionLoginPhase = "preparing"
	SessionLoginPending   SessionLoginPhase = "awaiting_confirmation"
	SessionLoginSucceeded SessionLoginPhase = "succeeded"
	SessionLoginDenied    SessionLoginPhase = "denied"
	SessionLoginExpired   SessionLoginPhase = "expired"
	SessionLoginFailed    SessionLoginPhase = "failed"
	SessionLoginUncertain SessionLoginPhase = "uncertain"
)

const (
	SessionLoginControlUnavailable = "client.session.control_unavailable"
	SessionLoginRetrying           = "client.session.retrying"
	SessionLoginOutcomeUnknown     = "client.session.outcome_unknown"
	SessionLoginDeadlineExceeded   = "client.session.login_expired"
	SessionLoginRejected           = "client.session.login_denied"
	SessionLoginInvalidResponse    = "client.session.invalid_response"
	SessionLoginForeignUser        = "client.session.foreign_user"
	SessionLoginAccessDenied       = "client.session.access_denied"
	SessionLoginCapacity           = "client.session.state_capacity"
)

var sessionLoginReasons = map[string]bool{"": true, SessionLoginControlUnavailable: true, SessionLoginRetrying: true, SessionLoginOutcomeUnknown: true, SessionLoginDeadlineExceeded: true, SessionLoginRejected: true, SessionLoginInvalidResponse: true, SessionLoginForeignUser: true, SessionLoginAccessDenied: true, SessionLoginCapacity: true}

// SessionLogin is private protected state, never an IPC DTO.
type SessionLogin struct {
	Binding                                       enrollment.Binding
	CreatedAt, Deadline, ObservedAt               time.Time
	SentAt, InFlightAt, RecoveryUntil, NextPollAt time.Time
	Phase                                         SessionLoginPhase
	Reason                                        string
	RequestProof, PollProof                       string
	Transaction                                   []byte
}

// Receipts retain correlation and outcome, without proof/bearer or native bytes.
type SessionLoginReceipt struct {
	Binding                                            enrollment.Binding
	CreatedAt, Deadline, CompletedAt, SessionExpiresAt time.Time
	Phase                                              SessionLoginPhase
	Reason                                             string
}

type NativeSession struct {
	Binding              enrollment.Binding
	CreatedAt, AdoptedAt time.Time
	Transaction          []byte
}

func (*SessionLogin) String() string    { return "[private session login]" }
func (*SessionLogin) GoString() string  { return "[private session login]" }
func (*NativeSession) String() string   { return "[private native session]" }
func (*NativeSession) GoString() string { return "[private native session]" }

func NewSessionLogin(p Profile, request, attempt, proof string, now time.Time) (*SessionLogin, error) {
	now = now.UTC()
	current, sessionErr := p.CurrentSession()
	if sessionErr != nil || proof == p.Secrets.RequestProof || proof == p.Secrets.PollProof || proof == current.Access || proof == current.Renewal {
		return nil, ErrCorrupt
	}
	if p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || p.Secrets.UserID == "" || p.Secrets.NodeCredential == "" || !validID(request) || !validID(attempt) || request == p.Attempt.RequestID || attempt == p.Attempt.AttemptID || !sessionLoginProof(proof) || now.IsZero() {
		return nil, ErrCorrupt
	}
	fingerprint, err := p.Keys.Fingerprint()
	if err != nil {
		return nil, err
	}
	l := &SessionLogin{Binding: enrollment.Binding{AttemptID: attempt, RequestID: request, ProfileID: p.ID, Owner: p.Attempt.Owner, ControlOrigin: p.ControlOrigin, DeviceKeyHash: fingerprint}, CreatedAt: now, ObservedAt: now, Deadline: now.Add(enrollment.LoginTTL), Phase: SessionLoginPreparing, RequestProof: proof}
	p.SessionLogin = l
	if _, err := p.SessionLoginShadow(); err != nil {
		return nil, err
	}
	return l, nil
}

func sessionLoginProof(value string) bool {
	return len(value) >= 32 && !strings.ContainsAny(value, " \t\r\n")
}

func (l *SessionLogin) Terminal() bool {
	return l.Phase == SessionLoginSucceeded || l.Phase == SessionLoginDenied || l.Phase == SessionLoginExpired || l.Phase == SessionLoginFailed
}

func (l *SessionLogin) EffectiveDeadline() time.Time {
	deadline := l.Deadline
	var tx identityapi.NativeLoginTransaction
	if len(l.Transaction) != 0 && proto.Unmarshal(l.Transaction, &tx) == nil && tx.ExpiresAt != nil && tx.ExpiresAt.CheckValid() == nil && tx.ExpiresAt.AsTime().Before(deadline) {
		deadline = tx.ExpiresAt.AsTime()
	}
	return deadline
}

func (l *SessionLogin) Receipt() SessionLoginReceipt {
	r := SessionLoginReceipt{Binding: l.Binding, CreatedAt: l.CreatedAt, Deadline: l.EffectiveDeadline(), CompletedAt: l.ObservedAt, Phase: l.Phase, Reason: l.Reason}
	var tx identityapi.NativeLoginTransaction
	if l.Phase == SessionLoginSucceeded && proto.Unmarshal(l.Transaction, &tx) == nil && tx.GetResult() != nil && tx.GetResult().Session != nil && tx.GetResult().Session.ExpiresAt != nil {
		r.SessionExpiresAt = tx.GetResult().Session.ExpiresAt.AsTime()
	}
	return r
}

// The adapter receives a temporary view. Original enrollment is never replaced.
func (p Profile) SessionLoginShadow() (Profile, error) {
	l := p.SessionLogin
	if l == nil {
		return Profile{}, ErrCorrupt
	}
	a, err := enrollment.New(l.Binding, l.CreatedAt)
	if err != nil {
		return Profile{}, ErrCorrupt
	}
	a.ObservedAt = l.ObservedAt
	if l.Phase == SessionLoginUncertain && !l.RecoveryUntil.IsZero() {
		a.Uncertain = true
		a.RecoveryUntil = l.RecoveryUntil
	}
	p.Attempt = &a
	p.Secrets = RemoteSecrets{RequestProof: l.RequestProof, PollProof: l.PollProof}
	p.Login = LoginProgress{}
	if len(l.Transaction) != 0 {
		var tx identityapi.NativeLoginTransaction
		if proto.Unmarshal(l.Transaction, &tx) != nil || tx.CreatedAt == nil || tx.ExpiresAt == nil || tx.ReplayExpiresAt == nil {
			return Profile{}, ErrCorrupt
		}
		p.Secrets.TransactionID = tx.TransactionId
		p.Login.CreatedAt = tx.CreatedAt.AsTime()
		p.Login.ExpiresAt = tx.ExpiresAt.AsTime()
		p.Login.ReplayUntil = tx.ReplayExpiresAt.AsTime()
	}
	if _, _, err = NativeLoginBinding(p); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func loginBinding(p Profile, b enrollment.Binding, created time.Time) bool {
	if p.Attempt == nil || b.Owner != p.Attempt.Owner || b.ProfileID != p.ID || b.ControlOrigin != p.ControlOrigin || b.DeviceKeyHash != p.Attempt.DeviceKeyHash || b.AttemptID == p.Attempt.AttemptID || b.RequestID == p.Attempt.RequestID || created.Before(p.Attempt.CreatedAt) {
		return false
	}
	_, err := enrollment.New(b, created)
	return err == nil
}

func nativeTransaction(p Profile, b enrollment.Binding, created time.Time, raw []byte) (*identityapi.NativeLoginTransaction, error) {
	if len(raw) == 0 || !loginBinding(p, b, created) {
		return nil, ErrCorrupt
	}
	a, err := enrollment.New(b, created)
	if err != nil {
		return nil, ErrCorrupt
	}
	shadow := p
	shadow.Attempt = &a
	_, binding, err := NativeLoginBinding(shadow)
	if err != nil {
		return nil, err
	}
	var tx identityapi.NativeLoginTransaction
	if proto.Unmarshal(raw, &tx) != nil || len(tx.ProtoReflect().GetUnknown()) != 0 || !validID(tx.TransactionId) || tx.RequestId != b.RequestID || !bytes.Equal(tx.AttemptBindingSha256, binding[:]) || tx.CreatedAt == nil || tx.CreatedAt.CheckValid() != nil || tx.ExpiresAt == nil || tx.ExpiresAt.CheckValid() != nil || tx.ReplayExpiresAt == nil || tx.ReplayExpiresAt.CheckValid() != nil || !tx.ExpiresAt.AsTime().Equal(tx.CreatedAt.AsTime().Add(enrollment.LoginTTL)) || tx.ReplayExpiresAt.AsTime().Before(tx.ExpiresAt.AsTime().Add(720*time.Second)) {
		return nil, ErrCorrupt
	}
	switch tx.State {
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_PENDING:
		action := tx.GetBrowserAction()
		if action == nil || action.ExpiresAt == nil || action.ExpiresAt.CheckValid() != nil || !action.ExpiresAt.AsTime().Equal(tx.ExpiresAt.AsTime()) || tx.PollAfterSeconds == 0 || (action.VerificationUrl != enrollment.NativeConsentOrigin(p.ControlOrigin)+"/auth/native-login/"+tx.TransactionId && action.VerificationUrl != enrollment.NativeConsentOrigin(p.ControlOrigin)+"/api/v1/auth/native-login/"+tx.TransactionId) {
			return nil, ErrCorrupt
		}
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_AUTHORIZED:
		r := tx.GetResult()
		if r == nil || r.Session == nil || r.Session.UserId != p.Secrets.UserID || r.Session.ExpiresAt == nil || r.RenewalAuthorization == nil || r.RenewalAuthorization.ExpiresAt == nil || !r.Session.RenewalSupported || sessionv1.ValidateSessionResponse(&sessionv1.GetSessionResponse{Session: r.Session, RenewalAuthorization: r.RenewalAuthorization}) != nil || (r.Session.State != sessionv1.UserSessionState_USER_SESSION_STATE_ACTIVE && r.Session.State != sessionv1.UserSessionState_USER_SESSION_STATE_EXPIRING) || r.AccessBearer == "" || strings.ContainsAny(r.AccessBearer, " \t\r\n") || r.AccessBearer == r.RenewalAuthorization.Bearer || tx.PollAfterSeconds != 0 {
			return nil, ErrCorrupt
		}
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_DENIED, identityapi.NativeLoginState_NATIVE_LOGIN_STATE_CANCELLED, identityapi.NativeLoginState_NATIVE_LOGIN_STATE_EXPIRED:
		if tx.Outcome != nil || tx.PollAfterSeconds != 0 {
			return nil, ErrCorrupt
		}
	default:
		return nil, ErrCorrupt
	}
	return &tx, nil
}

// CheckSessionLoginTransaction verifies the protected-state envelope shape and
// binding before the executor attempts an atomic adoption. Transport/trust and
// live authority remain the caller's responsibility.
func (p Profile) CheckSessionLoginTransaction(raw []byte) error {
	if p.SessionLogin == nil {
		return ErrCorrupt
	}
	_, err := nativeTransaction(p, p.SessionLogin.Binding, p.SessionLogin.CreatedAt, raw)
	return err
}

func validateSessionLogins(p Profile) error {
	if p.SessionLogin == nil && len(p.SessionLoginHistory) == 0 && len(p.SessionRefreshHistory) == 0 {
		return nil
	}
	if p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || p.Secrets.UserID == "" || p.Secrets.NodeCredential == "" {
		return ErrCorrupt
	}
	for request, r := range p.SessionLoginHistory {
		if !sessionLoginReasons[r.Reason] {
			return ErrCorrupt
		}
		if request != r.Binding.RequestID || !loginBinding(p, r.Binding, r.CreatedAt) || r.CompletedAt.Before(r.CreatedAt) || !r.Deadline.After(r.CreatedAt) || r.Deadline.After(r.CreatedAt.Add(enrollment.LoginTTL)) || (r.Phase != SessionLoginSucceeded && r.Phase != SessionLoginDenied && r.Phase != SessionLoginExpired && r.Phase != SessionLoginFailed && r.Phase != SessionLoginUncertain) || (r.Phase == SessionLoginSucceeded) != !r.SessionExpiresAt.IsZero() || r.Phase == SessionLoginSucceeded && (!r.CompletedAt.Before(r.Deadline) || !r.CompletedAt.Before(r.SessionExpiresAt)) {
			return ErrCorrupt
		}
	}
	for request, refresh := range p.SessionRefreshHistory {
		if refresh == nil || request != refresh.RequestID {
			return ErrCorrupt
		}
		shadow := p
		shadow.SessionRefresh = refresh
		shadow.SessionRefreshHistory = nil
		if validateCredentialProgress(shadow) != nil {
			return ErrCorrupt
		}
	}
	l := p.SessionLogin
	if l == nil {
		return nil
	}
	if !sessionLoginReasons[l.Reason] {
		return ErrCorrupt
	}
	if !loginBinding(p, l.Binding, l.CreatedAt) || !sessionLoginProof(l.RequestProof) || l.ObservedAt.Before(l.CreatedAt) || !l.Deadline.Equal(l.CreatedAt.Add(enrollment.LoginTTL)) || !l.SentAt.IsZero() && (l.SentAt.Before(l.CreatedAt) || !l.SentAt.Before(l.Deadline)) || !l.InFlightAt.IsZero() && (l.SentAt.IsZero() || l.InFlightAt.Before(l.SentAt) || l.InFlightAt.After(l.ObservedAt)) || !l.RecoveryUntil.IsZero() && (!l.RecoveryUntil.After(l.CreatedAt) || l.RecoveryUntil.After(l.Deadline)) || !l.NextPollAt.IsZero() && (l.NextPollAt.Before(l.ObservedAt) || l.NextPollAt.After(l.EffectiveDeadline())) {
		return ErrCorrupt
	}
	if _, exists := p.SessionLoginHistory[l.Binding.RequestID]; exists {
		return ErrCorrupt
	}
	var tx *identityapi.NativeLoginTransaction
	if len(l.Transaction) != 0 {
		var err error
		tx, err = nativeTransaction(p, l.Binding, l.CreatedAt, l.Transaction)
		if err != nil || !sessionLoginProof(l.PollProof) || l.PollProof == l.RequestProof || l.SentAt.IsZero() {
			return ErrCorrupt
		}
	} else if l.PollProof != "" {
		return ErrCorrupt
	}
	switch l.Phase {
	case SessionLoginPreparing:
		if tx != nil {
			return ErrCorrupt
		}
	case SessionLoginPending:
		if tx == nil || tx.State != identityapi.NativeLoginState_NATIVE_LOGIN_STATE_PENDING || !l.ObservedAt.Before(l.EffectiveDeadline()) {
			return ErrCorrupt
		}
	case SessionLoginSucceeded:
		if tx == nil || tx.State != identityapi.NativeLoginState_NATIVE_LOGIN_STATE_AUTHORIZED || !l.ObservedAt.Before(l.EffectiveDeadline()) || !l.ObservedAt.Before(tx.GetResult().Session.ExpiresAt.AsTime()) || !l.ObservedAt.Before(tx.GetResult().RenewalAuthorization.ExpiresAt.AsTime()) || !l.InFlightAt.IsZero() || !l.NextPollAt.IsZero() || l.Reason != "" || tx.GetResult().AccessBearer == l.RequestProof || tx.GetResult().AccessBearer == l.PollProof || tx.GetResult().RenewalAuthorization.Bearer == l.RequestProof || tx.GetResult().RenewalAuthorization.Bearer == l.PollProof {
			return ErrCorrupt
		}
	case SessionLoginDenied:
		if tx == nil || (tx.State != identityapi.NativeLoginState_NATIVE_LOGIN_STATE_DENIED && tx.State != identityapi.NativeLoginState_NATIVE_LOGIN_STATE_CANCELLED) {
			return ErrCorrupt
		}
	case SessionLoginExpired, SessionLoginFailed, SessionLoginUncertain:
		if l.Reason == "" || tx != nil && tx.State == identityapi.NativeLoginState_NATIVE_LOGIN_STATE_AUTHORIZED {
			return ErrCorrupt
		}
	default:
		return ErrCorrupt
	}
	if l.Terminal() && !l.NextPollAt.IsZero() {
		return ErrCorrupt
	}
	return nil
}

func nativeSessionAdoption(before, after Profile) bool {
	old, next := before.SessionLogin, after.SessionLogin
	a := after.ActiveSession
	if old == nil || next == nil || a == nil || a.Native == nil || old.Terminal() || old.InFlightAt.IsZero() || next.Phase != SessionLoginSucceeded || old.Binding != next.Binding || a.RequestID != next.Binding.RequestID || a.Native.Binding != next.Binding || !a.Native.CreatedAt.Equal(next.CreatedAt) || !a.Native.AdoptedAt.Equal(next.ObservedAt) || !bytes.Equal(a.Native.Transaction, next.Transaction) {
		return false
	}
	previous, err := before.CurrentSession()
	if err != nil || a.PreviousSessionID != previous.SessionID || a.PreviousRenewalSHA256 != CredentialDigest(previous.Renewal) {
		return false
	}
	current, err := after.CurrentSession()
	return err == nil && current.UserID == previous.UserID && current.SessionID != previous.SessionID && current.Access != previous.Access && current.Renewal != previous.Renewal
}

func sessionLoginsUpdate(before, after Profile) error {
	for request, old := range before.SessionLoginHistory {
		if next, ok := after.SessionLoginHistory[request]; !ok || !reflect.DeepEqual(old, next) {
			return ErrCorrupt
		}
	}
	for request, old := range before.SessionRefreshHistory {
		if next, ok := after.SessionRefreshHistory[request]; !ok || !reflect.DeepEqual(old, next) {
			return ErrCorrupt
		}
	}
	for request, next := range after.SessionRefreshHistory {
		if _, ok := before.SessionRefreshHistory[request]; !ok && (before.SessionRefresh == nil || before.SessionRefresh.RequestID != request || !reflect.DeepEqual(before.SessionRefresh, next) || !nativeSessionAdoption(before, after)) {
			return ErrCorrupt
		}
	}
	old, next := before.SessionLogin, after.SessionLogin
	for request, receipt := range after.SessionLoginHistory {
		if _, ok := before.SessionLoginHistory[request]; !ok && (old == nil || next == nil || old.Binding == next.Binding || request != old.Binding.RequestID || !reflect.DeepEqual(receipt, old.Receipt())) {
			return ErrCorrupt
		}
	}
	if old == nil && next == nil {
		return nil
	}
	if next == nil {
		return ErrCorrupt
	}
	if old == nil || old.Binding != next.Binding {
		if next.Phase != SessionLoginPreparing || next.Reason != "" || !next.ObservedAt.Equal(next.CreatedAt) || !next.SentAt.IsZero() || !next.InFlightAt.IsZero() || !next.RecoveryUntil.IsZero() || !next.NextPollAt.IsZero() || len(next.Transaction) != 0 || next.PollProof != "" {
			return ErrCorrupt
		}
		if old != nil {
			if next.CreatedAt.Before(old.Deadline) || !reflect.DeepEqual(after.SessionLoginHistory[old.Binding.RequestID], old.Receipt()) {
				return ErrCorrupt
			}
		}
		if _, exists := before.SessionLoginHistory[next.Binding.RequestID]; exists {
			return ErrCorrupt
		}
		return nil
	}
	if !old.CreatedAt.Equal(next.CreatedAt) || !old.Deadline.Equal(next.Deadline) || next.ObservedAt.Before(old.ObservedAt) || old.RequestProof != next.RequestProof || old.PollProof != "" && old.PollProof != next.PollProof || !old.SentAt.IsZero() && !old.SentAt.Equal(next.SentAt) || !old.InFlightAt.IsZero() && !next.InFlightAt.IsZero() && !old.InFlightAt.Equal(next.InFlightAt) || !old.RecoveryUntil.IsZero() && !old.RecoveryUntil.Equal(next.RecoveryUntil) || old.Terminal() && !reflect.DeepEqual(old, next) {
		return ErrCorrupt
	}
	if len(old.Transaction) != 0 {
		var a, b identityapi.NativeLoginTransaction
		if proto.Unmarshal(old.Transaction, &a) != nil || proto.Unmarshal(next.Transaction, &b) != nil || a.TransactionId != b.TransactionId || !proto.Equal(a.CreatedAt, b.CreatedAt) || !proto.Equal(a.ExpiresAt, b.ExpiresAt) || !proto.Equal(a.ReplayExpiresAt, b.ReplayExpiresAt) || a.State != identityapi.NativeLoginState_NATIVE_LOGIN_STATE_PENDING && !bytes.Equal(old.Transaction, next.Transaction) {
			return ErrCorrupt
		}
	}
	if next.Phase == SessionLoginSucceeded && !nativeSessionAdoption(before, after) {
		return ErrCorrupt
	}
	return nil
}
