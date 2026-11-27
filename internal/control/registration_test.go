package control

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
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
	wire "github.com/endless-net/coordinator/api/publicquery/v1"
	"github.com/endless-net/coordinator/api/publicquery/v1/publicqueryv1connect"
	signing "github.com/endless-net/signing/api/contract"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		if !ok || time.Until(deadline) > 30*time.Second || req.Method != http.MethodPost || req.URL.String() != f.p.ControlOrigin+publicqueryv1connect.EnrollmentServiceRegisterNodeProcedure || req.Header.Get("Authorization") != "Bearer "+f.p.Secrets.Access || req.Header.Get("Cookie") != "" || req.Header.Get("X-EndlessNet-Node-Credential") != "" {
			t.Fatal("wrong auth, route or deadline")
		}
		message := &wire.RegisterNodeRequest{}
		body := readRPC(t, req, message)
		if !matchesNativeProof(message, f.request) || bytes.Contains(body, []byte(f.p.Secrets.Access)) || coordinator.VerifyRegisterNodeIdentityProof(f.request) != nil {
			t.Fatal("original request changed or bearer leaked")
		}
		return rpcResponse(t, req, registrationMessage(f.result)), nil
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
	for _, name := range []string{"account", "network", "node", "operation", "proof", "device", "signature", "purpose", "scope", "expiry", "unknown"} {
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
				response := registrationMessage(f.result)
				if name == "unknown" {
					response.ProtoReflect().SetUnknown([]byte{0x78, 0x01})
				}
				return rpcResponse(t, req, response), nil
			})
			got, err := r.Register(context.Background(), f.p, f.original)
			if !errors.Is(err, ErrRegistrationProtocol) || got != (RegisteredResult{}) {
				t.Fatal("invalid result accepted", err)
			}
		})
	}
}

func TestJoinRegistrationReplaysFrozenProofWithoutSession(t *testing.T) {
	f := newRegistrationFixture(t)
	const key = "fixture-registration-key"
	f.p.Secrets.Access = ""
	f.p.Secrets.SessionExpiresAt = time.Time{}
	f.p.JoinTokenDigest = state.CredentialDigest(key)
	original, err := PrepareJoinRegistration(f.p, key)
	if err != nil {
		t.Fatal(err)
	}
	f.p.Secrets.RegistrationRequest = string(original)
	request, err := coordinator.DecodeRegisterNodeRequest(original, len(original))
	if err != nil || coordinator.VerifyRegisterNodeIdentityProof(request) != nil || request.SessionTokenBinding != "" {
		t.Fatal("invalid join proof", err)
	}
	f.result.RegistrationBinding = coordinator.RegistrationIdentityProofBinding(request)
	var first []byte
	calls := 0
	r := f.adapter(t, func(req *http.Request) (*http.Response, error) {
		calls++
		message := &wire.RegisterNodeRequest{}
		body := readRPC(t, req, message)
		if message.GetJoinToken() != key || !proto.Equal(message.Proof, registrationProof(request)) || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.Header.Get("X-EndlessNet-Node-Credential") != "" {
			t.Fatal("wrong join request")
		}
		if calls == 1 {
			first = bytes.Clone(body)
			return nil, io.ErrUnexpectedEOF
		}
		if !bytes.Equal(first, body) {
			t.Fatal("changed replay")
		}
		return rpcResponse(t, req, registrationMessage(f.result)), nil
	})
	if _, err := r.Register(t.Context(), f.p, original); !errors.Is(err, ErrRegistrationUnavailable) || calls != 1 {
		t.Fatal("lost reply", err, calls)
	}
	replay, err := PrepareRegistration(f.p)
	if err != nil || !bytes.Equal(replay, original) {
		t.Fatal("could not restore request", err)
	}
	got, err := r.Register(t.Context(), f.p, replay)
	if err != nil || calls != 2 || got.NodeID != "node" || got.Credential != f.result.NodeCredential {
		t.Fatal("failed to verify receipt", err, calls)
	}
	f.p.JoinTokenDigest = state.CredentialDigest("different-key")
	if _, err := r.Register(t.Context(), f.p, original); !errors.Is(err, ErrRegistrationProtocol) || calls != 2 {
		t.Fatal("changed key binding permitted a call")
	}
	// Exercise the protected-store commit too: restart the executor with only
	// persisted data after losing a reply, then inspect the terminal profile.
	f.p.JoinTokenDigest = state.CredentialDigest(key)
	f.p.RemoteInFlight, f.p.InFlightAt = false, time.Time{}
	block, err := aes.NewCipher(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	slot := &memory{}
	open := func() *state.Store {
		store, err := state.New(slot, testCodec{aead}, "e0802a1d-0c1c-43cc-86c5-cbe0ec3f25e8", 1<<20, 2<<20)
		if err != nil {
			t.Fatal(err)
		}
		return store
	}
	store := open()
	if err := store.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(t.Context(), 1, func(s *state.State) error {
		s.OwnerSID, s.ActiveProfileID = f.p.Attempt.Owner, f.p.ID
		s.Profiles[f.p.ID] = f.p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	config := RegistrationExecutorConfig{MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return f.now }}
	calls = 0
	for range 2 {
		e, err := NewRegistrationExecutor(open(), r, config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Reconcile(t.Context(), f.p.ID, f.p.Attempt.AttemptID); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.Add(time.Second)
	}
	saved, err := open().Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	p := saved.Profiles[f.p.ID]
	payload, err := json.Marshal(p)
	if err != nil || bytes.Contains(payload, []byte(key)) || p.Secrets.RegistrationRequest != "" ||
		p.Secrets.NodeCredential != f.result.NodeCredential || p.JoinReceipt == nil || p.JoinReceipt.Binding != f.result.RegistrationBinding || calls != 2 {
		t.Fatal("key not retired together with verified registration", err, calls)
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
				raw, _ := proto.Marshal(registrationMessage(f.result))
				status, media := 200, "application/proto"
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

func matchesNativeProof(m *wire.RegisterNodeRequest, expected coordinator.RegisterNodeRequest) bool {
	return proto.Equal(m.Proof, registrationProof(expected)) && m.GetNativeSession() != nil &&
		m.GetNativeSession().SessionTokenBinding == expected.SessionTokenBinding
}

func registrationMessage(r coordinator.RegisterNodeResponse) *wire.RegisterNodeResponse {
	return &wire.RegisterNodeResponse{Result: &wire.RegisterNodeResponse_Registration{
		Registration: testRegistration(r),
	}}
}

func testRegistration(r coordinator.RegisterNodeResponse) *wire.NodeRegistration {
	return &wire.NodeRegistration{IdempotencyId: r.IdempotencyID, NodeCredential: r.NodeCredential,
		Map: &wire.NetworkMapSnapshot{RegistrationBinding: r.RegistrationBinding,
			Revision: &wire.MapRevision{Network: r.Revision.Network, Global: r.Revision.Global},
			Network:  &wire.MapNetwork{Id: r.Network.ID, AccountId: r.Network.AccountID, CreatedAt: timestamppb.New(r.Network.CreatedAt)},
			Node: &wire.MapNode{Id: r.Node.ID, NetworkId: r.Node.NetworkID, Hostname: r.Node.Hostname,
				IdentityPublicKey: r.Node.IdentityPublicKey, PublicKey: r.Node.PublicKey, DeviceFingerprint: r.Node.DeviceFingerprint,
				CreatedAt: timestamppb.New(r.Node.CreatedAt), UpdatedAt: timestamppb.New(r.Node.UpdatedAt), LastSeen: timestamppb.New(r.Node.LastSeen),
			},
		},
	}
}
