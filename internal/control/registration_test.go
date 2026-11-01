package control

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
)

type registrationFixture struct {
	store        *state.Store
	memory       *memory
	p            state.Profile
	request      coordinator.RegisterNodeRequest
	result       coordinator.RegisterNodeResponse
	announcement Announcement
	key          ed25519.PrivateKey
	now          time.Time
	original     []byte
}

func newRegistrationFixture(t *testing.T) registrationFixture {
	t.Helper()
	e, server, clock := loginFixture(t)
	s, err := e.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Profiles[loginProfile]
	a, err := p.Attempt.LoginSucceeded(*clock)
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.Select("account", "network", *clock)
	if err != nil {
		t.Fatal(err)
	}
	p.Attempt = &a
	p.Hostname = "device"
	p.RemoteInFlight, p.InFlightAt = true, *clock
	p.Secrets.RegistrationID = "83e11eba-fd12-46ba-a977-9038c0f6a2f0"
	p.Secrets.Access = strings.Repeat("a", 43)
	p.Secrets.SessionExpiresAt = clock.Add(time.Hour)
	original, err := PrepareRegistration(p)
	if err != nil {
		t.Fatal(err)
	}
	request, err := coordinator.DecodeRegisterNodeRequest(original, len(original))
	if err != nil {
		t.Fatal(err)
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32))
	public := key.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(public)
	bundles := fixture(t)
	bundles.NodeCredentialTrustBundle.Keys[0].KeyID = "ed25519:" + base64.RawURLEncoding.EncodeToString(digest[:])
	bundles.NodeCredentialTrustBundle.Keys[0].PublicKey = base64.RawURLEncoding.EncodeToString(public)
	bundles.NodeCredentialTrustBundle.ActiveKeyID = bundles.NodeCredentialTrustBundle.Keys[0].KeyID
	announcement, err := DecodeAnnouncement(p.ControlOrigin, encode(t, bundles), *clock)
	if err != nil {
		t.Fatal(err)
	}
	p.Trust = state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}
	f := registrationFixture{store: e.store, memory: server.memory, p: p, request: request, original: original, announcement: announcement, key: key, now: *clock}
	f.result = coordinator.RegisterNodeResponse{SchemaVersion: 2, IdempotencyID: request.IdempotencyID, RegistrationBinding: coordinator.RegistrationIdentityProofBinding(request),
		Network: coordinator.Network{ID: request.NetworkID, AccountID: request.AccountID}, Node: coordinator.Node{ID: "node", NetworkID: request.NetworkID, IdentityPublicKey: request.IdentityPublicKey, PublicKey: request.PublicKey, DeviceFingerprint: request.DeviceFingerprint}}
	f.sign(t, bundles.NodeCredentialTrustBundle.ActiveKeyID, "node:register", f.now.Add(time.Hour))
	return f
}

func (f *registrationFixture) sign(t *testing.T, keyID, scope string, expires time.Time) {
	t.Helper()
	claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: keyID, NetworkID: f.request.NetworkID, NodeID: "node", Scopes: []string{scope}, ExpiresAt: expires}
	payload, err := coordinator.NodeCredentialSigningPayload(keyID, claims.NetworkID, claims.NodeID, claims.Scopes, claims.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	claims.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.key, payload))
	f.result.NodeCredential = "enc_" + base64.RawURLEncoding.EncodeToString(encode(t, claims))
}

func (f *registrationFixture) adapter(t *testing.T, roundTrip transport) *Registration {
	t.Helper()
	r, err := NewRegistration(&http.Client{Transport: roundTrip}, staticAnnouncement{f.announcement}, RegistrationConfig{MaxRequestBytes: 16384, MaxResponseBytes: 65536, MaxCredentialBytes: 8192, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDirectRegistrationOriginalRequestAndVerifiedDisconnectedResult(t *testing.T) {
	f := newRegistrationFixture(t)
	calls := 0
	r := f.adapter(t, func(req *http.Request) (*http.Response, error) {
		calls++
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > 30*time.Second || req.Method != http.MethodPost || req.URL.String() != f.p.ControlOrigin+"/nodes/register" || req.Header.Get("Authorization") != "Bearer "+f.p.Secrets.Access || req.Header.Get("Cookie") != "" || req.Header.Get("X-EndlessNet-Node-Credential") != "" {
			t.Fatal("wrong auth, route or deadline")
		}
		body, err := io.ReadAll(req.Body)
		if err != nil || !bytes.Equal(body, f.original) || bytes.Contains(body, []byte(f.p.Secrets.Access)) || coordinator.VerifyRegisterNodeIdentityProof(f.request) != nil {
			t.Fatal("original request changed or bearer leaked")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(encode(t, f.result))), Request: req}, nil
	})
	got, err := r.Register(context.Background(), f.p, f.original)
	if err != nil || got.NodeID != "node" || got.AccountID != "account" || got.NetworkID != "network" || got.Credential != f.result.NodeCredential || calls != 1 {
		t.Fatal("verified result missing", err)
	}
	// Result intentionally has no map, routes, peers, connected flag or UI grant.
	if f.p.Attempt.NodeID != "" {
		t.Fatal("adapter mutated durable state")
	}
}

func TestDirectRegistrationRejectsForeignOrInvalidCredential(t *testing.T) {
	for _, name := range []string{"account", "network", "node", "operation", "proof", "device", "signature", "purpose", "scope", "expiry", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			f := newRegistrationFixture(t)
			switch name {
			case "account":
				f.result.Network.AccountID = "foreign"
			case "network":
				f.result.Network.ID = "foreign"
			case "node":
				f.result.Node.ID = "foreign"
			case "operation":
				f.result.IdempotencyID = "foreign"
			case "proof":
				f.result.RegistrationBinding = "foreign"
			case "device":
				f.result.Node.PublicKey = "foreign"
			case "signature":
				claims, _ := coordinator.DecodeNodeCredential(f.result.NodeCredential, 8192)
				f.key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, 32))
				f.sign(t, claims.KeyID, "node:register", f.now.Add(time.Hour))
			case "purpose":
				var bundle signing.ServerKeyResponse
				if json.Unmarshal(f.announcement.Bundle, &bundle) != nil {
					t.Fatal("bundle")
				}
				f.sign(t, bundle.TrustBundle.ActiveKeyID, "node:register", f.now.Add(time.Hour))
			case "scope":
				claims, _ := coordinator.DecodeNodeCredential(f.result.NodeCredential, 8192)
				f.sign(t, claims.KeyID, "node:map", f.now.Add(time.Hour))
			case "expiry":
				claims, _ := coordinator.DecodeNodeCredential(f.result.NodeCredential, 8192)
				f.sign(t, claims.KeyID, "node:register", f.now)
			}
			r := f.adapter(t, func(req *http.Request) (*http.Response, error) {
				raw := encode(t, f.result)
				if name == "duplicate" {
					raw = bytes.Replace(raw, []byte(`"schema_version":2`), []byte(`"schema_version":2,"schema_version":2`), 1)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw)), Request: req}, nil
			})
			got, err := r.Register(context.Background(), f.p, f.original)
			if !errors.Is(err, ErrRegistrationProtocol) || got != (RegisteredResult{}) {
				t.Fatal("invalid result accepted", err)
			}
		})
	}
}

func TestDirectRegistrationNoSendWithoutOriginalClaimTrustAndBudget(t *testing.T) {
	for _, name := range []string{"request", "session", "claim", "trust", "deadline", "recovery", "cancel"} {
		t.Run(name, func(t *testing.T) {
			f := newRegistrationFixture(t)
			r := f.adapter(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid call dispatched")
				return nil, errors.New("unexpected")
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "request":
				f.original = append(f.original, ' ')
			case "session":
				f.p.Secrets.Access = strings.Repeat("b", 43)
			case "claim":
				f.p.RemoteInFlight = false
			case "trust":
				f.p.Trust.AnnouncementID = "changed"
			case "deadline":
				f.now = f.p.Attempt.Deadline
			case "recovery":
				f.p.Attempt.Uncertain = true
				f.p.Attempt.RecoveryUntil = f.now
				f.now = f.now.Add(time.Second)
			case "cancel":
				cancel()
			}
			got, err := r.Register(ctx, f.p, f.original)
			if err == nil || got != (RegisteredResult{}) {
				t.Fatal("unverified result", err)
			}
		})
	}
}

func TestDirectRegistrationLostResponseHasNoRetryOrSecretError(t *testing.T) {
	f := newRegistrationFixture(t)
	calls := 0
	r := f.adapter(t, func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("secret-bearer-and-tenant")
	})
	got, err := r.Register(context.Background(), f.p, f.original)
	if !errors.Is(err, ErrRegistrationUnavailable) || got != (RegisteredResult{}) || calls != 1 || strings.Contains(err.Error(), "secret") {
		t.Fatal("unknown response promoted or leaked", err)
	}
}

func TestDirectRegistrationResponseBudgetsAndLateResult(t *testing.T) {
	for _, name := range []string{"oversize", "credential_size", "http_error", "content_type", "late", "cancelled_body"} {
		t.Run(name, func(t *testing.T) {
			f := newRegistrationFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			r := f.adapter(t, func(req *http.Request) (*http.Response, error) {
				calls++
				raw, status, media := encode(t, f.result), 200, "application/json"
				switch name {
				case "oversize":
					raw = bytes.Repeat([]byte("x"), 65537)
				case "http_error":
					status, raw = 403, []byte("secret-upstream-details")
				case "content_type":
					media = "text/plain"
				case "late":
					f.now = f.p.Secrets.SessionExpiresAt
				case "cancelled_body":
					cancel()
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{media}}, Body: io.NopCloser(bytes.NewReader(raw)), Request: req}, nil
			})
			if name == "credential_size" {
				r.config.MaxCredentialBytes = 1
			}
			got, err := r.Register(ctx, f.p, f.original)
			if err == nil || got != (RegisteredResult{}) || calls != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe result", err)
			}
		})
	}
}
