package control

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
)

var ErrRegistrationProtocol = errors.New("registration response invalid")
var ErrRegistrationUnavailable = errors.New("registration outcome unavailable")

type RegistrationConfig struct {
	MaxRequestBytes, MaxResponseBytes, MaxCredentialBytes int
	Now                                                   func() time.Time
}

type Registration struct {
	http          *http.Client
	announcements Announcements
	config        RegistrationConfig
}

// RegisteredResult is private daemon state, never an IPC response. A verified
// credential does not authorize applying the accompanying map or opening traffic.
type RegisteredResult struct {
	AccountID, NetworkID, NodeID, Credential string
	CredentialEnds                           time.Time
}

func (RegisteredResult) String() string   { return "[private registered result]" }
func (RegisteredResult) GoString() string { return "[private registered result]" }

func NewRegistration(client *http.Client, announcements Announcements, config RegistrationConfig) (*Registration, error) {
	if announcements == nil || config.MaxRequestBytes <= 0 || config.MaxResponseBytes <= 0 || config.MaxCredentialBytes <= 0 {
		return nil, ErrRegistrationProtocol
	}
	b, err := NewBootstrap(client, int64(config.MaxResponseBytes), config.Now)
	if err != nil {
		return nil, err
	}
	config.Now = b.now
	return &Registration{http: b.client, announcements: announcements, config: config}, nil
}

// PrepareRegistration builds the exact direct-session request from durable keys,
// selection and registration UUID. The caller must persist these bytes and the
// original session before marking a send in flight. It performs no network IO.
func PrepareRegistration(p state.Profile) ([]byte, error) {
	if p.Attempt == nil || p.Attempt.BrowserApproval || p.Approval != nil || p.Attempt.Validate() != nil || p.Attempt.Phase != enrollment.Registering || !identifier(p.Secrets.RegistrationID) || p.Attempt.AccountID == "" || p.Attempt.NetworkID == "" || !validProof(p.Secrets.Access) || !targetID(p.Hostname) {
		return nil, ErrRegistrationProtocol
	}
	_, _, err := NativeBinding(p)
	if err != nil {
		return nil, ErrRegistrationProtocol
	}
	// NativeBinding validates the persisted identity; the device fingerprint is
	// the key hash, not the native-login attempt digest returned by that helper.
	fingerprint, err := p.Keys.Fingerprint()
	if err != nil {
		return nil, ErrRegistrationProtocol
	}
	wg, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
	if err != nil {
		return nil, ErrRegistrationProtocol
	}
	request := coordinator.RegisterNodeRequest{
		SchemaVersion: coordinator.SchemaVersion, IdempotencyID: p.Secrets.RegistrationID,
		AccountID: p.Attempt.AccountID, NetworkID: p.Attempt.NetworkID, Hostname: p.Hostname,
		SessionTokenBinding: coordinator.RegistrationSessionTokenBinding(p.Secrets.Access),
		PublicKey:           base64.StdEncoding.EncodeToString(wg.PublicKey().Bytes()), DeviceFingerprint: hex.EncodeToString(fingerprint[:]),
	}
	if coordinator.SetRegisterNodeIdentityProof(&request, ed25519.PrivateKey(p.Keys.IdentityPrivate)) != nil {
		return nil, ErrRegistrationProtocol
	}
	if p.DefaultTarget != nil {
		original, err := coordinator.DecodeEnsureDefaultEnrollmentTargetRequest([]byte(p.DefaultTarget.Request), coordinator.DefaultTargetMaxBytes)
		receipt, receiptErr := coordinator.DecodeDefaultEnrollmentTarget([]byte(p.DefaultTarget.Receipt), coordinator.DefaultTargetMaxBytes)
		if err != nil || receiptErr != nil || receipt.MatchRegistration(original, request, receipt.ResolvedAt) != nil {
			return nil, ErrRegistrationProtocol
		}
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, ErrRegistrationProtocol
	}
	return raw, nil
}

// Register sends once, using only the original persisted request. Failure after
// dispatch has an unknown outcome; this adapter never retries, changes the UUID,
// interprets an HTTP error body as authority, or persists a success itself.
func (r *Registration) Register(ctx context.Context, p state.Profile, original []byte) (RegisteredResult, error) {
	if err := ctx.Err(); err != nil {
		return RegisteredResult{}, err
	}
	expected, err := PrepareRegistration(p)
	if err != nil || len(original) > r.config.MaxRequestBytes || !bytes.Equal(original, expected) || !p.RemoteInFlight || p.InFlightAt.IsZero() {
		return RegisteredResult{}, ErrRegistrationProtocol
	}
	request, err := coordinator.DecodeRegisterNodeRequest(original, r.config.MaxRequestBytes)
	if err != nil {
		return RegisteredResult{}, ErrRegistrationProtocol
	}
	if p.DefaultTarget != nil {
		resolver, err := coordinator.DecodeEnsureDefaultEnrollmentTargetRequest([]byte(p.DefaultTarget.Request), coordinator.DefaultTargetMaxBytes)
		receipt, receiptErr := coordinator.DecodeDefaultEnrollmentTarget([]byte(p.DefaultTarget.Receipt), coordinator.DefaultTargetMaxBytes)
		if err != nil || receiptErr != nil || receipt.MatchRegistration(resolver, request, r.config.Now()) != nil {
			return RegisteredResult{}, ErrRegistrationProtocol
		}
	}
	remaining := p.Attempt.Deadline.Sub(r.config.Now())
	for _, deadline := range []time.Time{p.Secrets.SessionExpiresAt, p.Attempt.RecoveryUntil} {
		if !deadline.IsZero() && (deadline.Equal(p.Secrets.SessionExpiresAt) || p.Attempt.Uncertain) && deadline.Sub(r.config.Now()) < remaining {
			remaining = deadline.Sub(r.config.Now())
		}
	}
	if p.Secrets.SessionExpiresAt.IsZero() || remaining <= 0 {
		return RegisteredResult{}, context.DeadlineExceeded
	}
	if remaining > 30*time.Second {
		remaining = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	a, err := r.announcements.Fetch(ctx, p.ControlOrigin)
	if err != nil || !a.MatchesProfile(p, r.config.Now()) {
		return RegisteredResult{}, ErrBootstrap
	}
	p.Trust = state.ServerTrust{KeyID: a.KeyID, AnnouncementID: a.ID, Bundle: a.Bundle}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.ControlOrigin+"/nodes/register", bytes.NewReader(original))
	if err != nil {
		return RegisteredResult{}, ErrRegistrationProtocol
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.Secrets.Access)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	response, err := r.http.Do(httpRequest)
	if err != nil {
		return RegisteredResult{}, registrationCallError(ctx)
	}
	defer func() { _ = response.Body.Close() }()
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || response.StatusCode != http.StatusOK {
		return RegisteredResult{}, ErrRegistrationUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, int64(r.config.MaxResponseBytes)+1))
	if err != nil || len(raw) > r.config.MaxResponseBytes {
		return RegisteredResult{}, registrationCallError(ctx)
	}
	if ctx.Err() != nil {
		return RegisteredResult{}, registrationCallError(ctx)
	}
	result, err := coordinator.DecodeRegisterNodeResponse(raw, r.config.MaxResponseBytes)
	if err != nil {
		return RegisteredResult{}, ErrRegistrationProtocol
	}
	if !a.MatchesProfile(p, r.config.Now()) {
		return RegisteredResult{}, ErrBootstrap
	}
	verified, err := r.verify(p, request, result)
	if ctx.Err() != nil {
		return RegisteredResult{}, registrationCallError(ctx)
	}
	return verified, err
}

func registrationCallError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrRegistrationUnavailable
}

func (r *Registration) verify(p state.Profile, request coordinator.RegisterNodeRequest, result coordinator.RegisterNodeResponse) (RegisteredResult, error) {
	now := r.config.Now()
	if !now.Before(p.Attempt.Deadline) || !now.Before(p.Secrets.SessionExpiresAt) || p.Attempt.Uncertain && !now.Before(p.Attempt.RecoveryUntil) {
		return RegisteredResult{}, context.DeadlineExceeded
	}
	return r.verifyResult(p, request, result, now)
}

// verifyResult checks the device/result binding and trusted credential purpose.
// Each caller must separately enforce its own credential class and live budgets;
// browser approval must not weaken the native-session checks above.
func (r *Registration) credentialKey(p state.Profile, credential string, now time.Time) (ed25519.PublicKey, error) {
	// Revalidate the retained, approved announcement at response time too.
	a := Announcement{Origin: p.ControlOrigin, KeyID: p.Trust.KeyID, ID: p.Trust.AnnouncementID, Bundle: p.Trust.Bundle}
	if a.Validate(now) != nil {
		return nil, ErrBootstrap
	}
	var bundles signing.ServerKeyResponse
	if json.Unmarshal(a.Bundle, &bundles) != nil {
		return nil, ErrBootstrap
	}
	claims, err := coordinator.DecodeNodeCredential(credential, r.config.MaxCredentialBytes)
	if err != nil {
		return nil, ErrRegistrationProtocol
	}
	key, err := signing.ResolveBootstrapKey(bundles.NodeCredentialTrustBundle, claims.KeyID, now)
	if err != nil {
		return nil, ErrRegistrationProtocol
	}
	public, err := base64.RawURLEncoding.Strict().DecodeString(key.PublicKey)
	if err != nil {
		return nil, ErrRegistrationProtocol
	}
	return ed25519.PublicKey(public), nil
}

func (r *Registration) verifyResult(p state.Profile, request coordinator.RegisterNodeRequest, result coordinator.RegisterNodeResponse, now time.Time) (RegisteredResult, error) {
	public, err := r.credentialKey(p, result.NodeCredential, now)
	if err != nil {
		return RegisteredResult{}, err
	}

	claims, err := coordinator.VerifyRegistrationResult(request, result, r.config.MaxCredentialBytes, ed25519.PublicKey(public), "node:register", now)
	if err != nil || !targetID(result.Node.ID) || strings.TrimSpace(result.NodeCredential) != result.NodeCredential {
		return RegisteredResult{}, ErrRegistrationProtocol
	}
	return RegisteredResult{AccountID: result.Network.AccountID, NetworkID: result.Network.ID, NodeID: result.Node.ID, Credential: result.NodeCredential, CredentialEnds: claims.ExpiresAt}, nil
}
