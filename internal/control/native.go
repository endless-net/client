package control

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	sessionv1 "github.com/endless-net/identity/api/session/v1"
	identityapi "github.com/endless-net/identity/api/v1"
	"github.com/endless-net/identity/api/v1/identityapiconnect"
)

var ErrNativeProtocol = errors.New("native login response invalid")

type Native struct {
	http          *http.Client
	announcements Announcements
	maxBytes      int
	consentPrefix string
	now           func() time.Time
}

func NewNative(client *http.Client, announcements Announcements, maxBytes int, consentPrefix string, now func() time.Time) (*Native, error) {
	if announcements == nil || maxBytes <= 0 || (consentPrefix != "/auth/native-login/" && consentPrefix != "/api/v1/auth/native-login/") {
		return nil, ErrNativeProtocol
	}
	// Reuse this public adapter's restricted HTTP policy; no cookie/redirect/
	// workload-certificate transport is introduced for credential-bearing calls.
	b, err := NewBootstrap(client, int64(maxBytes), now)
	if err != nil {
		return nil, err
	}
	return &Native{http: b.client, announcements: announcements, maxBytes: maxBytes, consentPrefix: consentPrefix, now: b.now}, nil
}

// NativeBinding implements the caller-owned digest of its durable attempt.
// Ordered fields/uint32 lengths are specified by published native_login.proto.
func NativeBinding(p state.Profile) (string, [32]byte, error) {
	if p.Attempt == nil || p.Attempt.Validate() != nil {
		return "", [32]byte{}, ErrNativeProtocol
	}
	fingerprint, err := p.Keys.Fingerprint()
	if err != nil || fingerprint != p.Attempt.DeviceKeyHash || p.ID != p.Attempt.ProfileID || p.ControlOrigin != p.Attempt.ControlOrigin {
		return "", [32]byte{}, ErrNativeProtocol
	}
	public := "enp_" + base64.RawURLEncoding.EncodeToString(p.Keys.IdentityPrivate[ed25519.SeedSize:])
	h := sha256.New()
	for _, field := range []string{"endlessnet-native-login-003-v1", p.ControlOrigin, p.ID, p.Attempt.AttemptID, p.Attempt.RequestID, public} {
		if uint64(len(field)) > uint64(^uint32(0)) {
			return "", [32]byte{}, ErrNativeProtocol
		}
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(field)))
		_, _ = h.Write(size[:])
		_, _ = h.Write([]byte(field))
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return public, digest, nil
}

func (n *Native) trusted(ctx context.Context, p state.Profile) error {
	a, err := n.announcements.Fetch(ctx, p.ControlOrigin)
	if err != nil || !a.MatchesProfile(p, n.now()) {
		return ErrBootstrap
	}
	return nil
}

// Call makes one attempt only. The executor persists InFlight and the original
// request/proof before calling; retries/recovery must use that same profile.
func (n *Native) Call(ctx context.Context, p state.Profile, cancelLogin bool) (*identityapi.NativeLoginTransaction, string, error) {
	if p.Attempt == nil {
		return nil, "", ErrNativeProtocol
	}
	remaining := p.Attempt.LoginUntil.Sub(n.now())
	for _, deadline := range []time.Time{p.Attempt.Deadline, p.Attempt.RecoveryUntil} {
		if !deadline.IsZero() && (p.Attempt.Uncertain || deadline.Equal(p.Attempt.Deadline)) && deadline.Sub(n.now()) < remaining {
			remaining = deadline.Sub(n.now())
		}
	}
	if remaining <= 0 {
		return nil, "", context.DeadlineExceeded
	}
	if remaining > 30*time.Second {
		remaining = 30 * time.Second
	}
	ctx, stop := context.WithTimeout(ctx, remaining)
	defer stop()
	if err := n.trusted(ctx, p); err != nil {
		return nil, "", err
	}
	public, binding, err := NativeBinding(p)
	if err != nil {
		return nil, "", err
	}
	client := identityapiconnect.NewNativeLoginServiceClient(n.http, p.ControlOrigin, connect.WithReadMaxBytes(n.maxBytes))
	var transaction *identityapi.NativeLoginTransaction
	poll := p.Secrets.PollProof
	if p.Secrets.TransactionID == "" {
		if cancelLogin || !validProof(p.Secrets.RequestProof) {
			return nil, "", ErrNativeProtocol
		}
		req := connect.NewRequest(&identityapi.StartNativeLoginRequest{RequestId: p.Attempt.RequestID, AttemptBindingSha256: binding[:], DeviceIdentityPublicKey: public})
		req.Header().Set("Authorization", "Bearer "+p.Secrets.RequestProof)
		response, err := client.StartNativeLogin(ctx, req)
		if err != nil {
			return nil, "", nativeError(err)
		}
		transaction = response.Msg.Transaction
		poll = response.Msg.PollAuthorization
		if !validProof(poll) || poll == p.Secrets.RequestProof {
			return nil, "", ErrNativeProtocol
		}
	} else {
		if !validProof(poll) {
			return nil, "", ErrNativeProtocol
		}
		if cancelLogin {
			req := connect.NewRequest(&identityapi.CancelNativeLoginRequest{TransactionId: p.Secrets.TransactionID})
			req.Header().Set("Authorization", "Bearer "+poll)
			response, err := client.CancelNativeLogin(ctx, req)
			if err != nil {
				return nil, "", nativeError(err)
			}
			transaction = response.Msg.Transaction
		} else {
			req := connect.NewRequest(&identityapi.GetNativeLoginRequest{TransactionId: p.Secrets.TransactionID})
			req.Header().Set("Authorization", "Bearer "+poll)
			response, err := client.GetNativeLogin(ctx, req)
			if err != nil {
				return nil, "", nativeError(err)
			}
			transaction = response.Msg.Transaction
		}
	}
	if err := n.validate(p, transaction, binding, poll); err != nil {
		return nil, "", err
	}
	return transaction, poll, nil
}

func validProof(value string) bool { return len(value) >= 32 && !strings.ContainsAny(value, " \t\r\n") }
func nativeError(err error) error {
	return connect.NewError(connect.CodeOf(err), errors.New("native login call failed"))
}

func (n *Native) validate(p state.Profile, t *identityapi.NativeLoginTransaction, binding [32]byte, poll string) error {
	if t == nil || !identifier(t.TransactionId) || t.RequestId != p.Attempt.RequestID || !bytes.Equal(t.AttemptBindingSha256, binding[:]) || t.CreatedAt == nil || t.CreatedAt.CheckValid() != nil || t.ExpiresAt == nil || t.ExpiresAt.CheckValid() != nil || t.ReplayExpiresAt == nil || t.ReplayExpiresAt.CheckValid() != nil || !t.ExpiresAt.AsTime().Equal(t.CreatedAt.AsTime().Add(enrollment.LoginTTL)) || t.ReplayExpiresAt.AsTime().Before(t.ExpiresAt.AsTime().Add(720*time.Second)) {
		return ErrNativeProtocol
	}
	if p.Secrets.TransactionID != "" && (p.Secrets.TransactionID != t.TransactionId || !p.Login.CreatedAt.Equal(t.CreatedAt.AsTime()) || !p.Login.ExpiresAt.Equal(t.ExpiresAt.AsTime()) || !p.Login.ReplayUntil.Equal(t.ReplayExpiresAt.AsTime())) {
		return ErrNativeProtocol
	}
	switch t.State {
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_PENDING:
		a := t.GetBrowserAction()
		if a == nil || a.ExpiresAt == nil || a.ExpiresAt.CheckValid() != nil || !a.ExpiresAt.AsTime().Equal(t.ExpiresAt.AsTime()) || a.VerificationUrl != enrollment.NativeConsentOrigin(p.ControlOrigin)+n.consentPrefix+t.TransactionId || t.PollAfterSeconds == 0 || !n.now().Before(t.ExpiresAt.AsTime()) {
			return ErrNativeProtocol
		}
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_AUTHORIZED:
		r := t.GetResult()
		if r == nil || r.Session == nil || r.Session.ExpiresAt == nil || r.RenewalAuthorization == nil || r.RenewalAuthorization.ExpiresAt == nil || !r.Session.RenewalSupported || sessionv1.ValidateSessionResponse(&sessionv1.GetSessionResponse{Session: r.Session, RenewalAuthorization: r.RenewalAuthorization}) != nil || (r.Session.State != sessionv1.UserSessionState_USER_SESSION_STATE_ACTIVE && r.Session.State != sessionv1.UserSessionState_USER_SESSION_STATE_EXPIRING) || !n.now().Before(r.Session.ExpiresAt.AsTime()) || !n.now().Before(r.RenewalAuthorization.ExpiresAt.AsTime()) || r.AccessBearer == "" || strings.ContainsAny(r.AccessBearer, " \t\r\n") || r.AccessBearer == poll || r.AccessBearer == p.Secrets.RequestProof || r.RenewalAuthorization.Bearer == r.AccessBearer || r.RenewalAuthorization.Bearer == poll || r.RenewalAuthorization.Bearer == p.Secrets.RequestProof || t.PollAfterSeconds != 0 {
			return ErrNativeProtocol
		}
	case identityapi.NativeLoginState_NATIVE_LOGIN_STATE_DENIED, identityapi.NativeLoginState_NATIVE_LOGIN_STATE_CANCELLED, identityapi.NativeLoginState_NATIVE_LOGIN_STATE_EXPIRED:
		if t.Outcome != nil || t.PollAfterSeconds != 0 {
			return ErrNativeProtocol
		}
	default:
		return ErrNativeProtocol
	}
	return nil
}

func identifier(value string) bool {
	if len(value) != 36 || value == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
