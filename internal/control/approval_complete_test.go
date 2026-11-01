package control

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
)

func (f *approvalFixture) signCompletion(t *testing.T, keyID, scope string, expires time.Time) {
	t.Helper()
	claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: keyID, NetworkID: f.result.Network.ID, NodeID: f.result.Node.ID, Scopes: []string{scope}, ExpiresAt: expires}
	payload, err := coordinator.NodeCredentialSigningPayload(keyID, claims.NetworkID, claims.NodeID, claims.Scopes, expires)
	if err != nil {
		t.Fatal(err)
	}
	claims.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.key, payload))
	f.result.NodeCredential = "enc_" + base64.RawURLEncoding.EncodeToString(encode(t, claims))
}

func (f *approvalFixture) enrolled(t *testing.T) {
	t.Helper()
	claims, err := coordinator.DecodeNodeCredential(f.result.NodeCredential, 8192)
	if err != nil {
		t.Fatal(err)
	}
	stamp := f.now
	f.view.Status, f.view.NodeID, f.view.CompletedAt = "enrolled", "node", &stamp
	f.result = coordinator.RegisterNodeResponse{SchemaVersion: coordinator.SchemaVersion, IdempotencyID: f.request.IdempotencyID, RegistrationBinding: coordinator.RegistrationIdentityProofBinding(f.request),
		Network: coordinator.Network{ID: f.view.NetworkID, AccountID: f.view.AccountID},
		Node:    coordinator.Node{ID: "node", NetworkID: f.view.NetworkID, Hostname: f.request.Hostname, IdentityPublicKey: f.request.IdentityPublicKey, PublicKey: f.request.PublicKey, DeviceFingerprint: f.request.DeviceFingerprint}}
	f.signCompletion(t, claims.KeyID, "node:register", f.now.Add(time.Hour))
}

func TestApprovalCompleteVerifiesOriginalDeviceReceiptAndPurposeCredential(t *testing.T) {
	for _, assigned := range []bool{false, true} {
		f := newApprovalFixture(t, assigned)
		f.approve()
		previous := f.pending()
		previous.PollAfterSeconds = 0
		f.now = f.now.Add(time.Second)
		f.enrolled(t)
		// Approval completion has its own poll credential class, not an expired
		// native session. The native adapter retains its independent live gate.
		f.p.Secrets.SessionExpiresAt = f.now.Add(-time.Second)
		calls := 0
		a := f.approval(t, func(req *http.Request) (*http.Response, error) {
			calls++
			body, err := io.ReadAll(req.Body)
			deadline, bounded := req.Context().Deadline()
			if err != nil || len(body) != 0 || req.ContentLength != 0 || req.Method != http.MethodPost || req.URL.String() != f.p.ControlOrigin+"/nodes/enrollment-requests/opaque-request-1/complete" || req.Header.Get("Authorization") != "Bearer "+f.token || req.Header.Get("Content-Type") != "" || req.Header.Get("Cookie") != "" || req.Header.Get("X-EndlessNet-Node-Credential") != "" || !bounded || time.Until(deadline) > 30*time.Second {
				t.Fatal("completion body, binding, credential class or budget changed")
			}
			return approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: &f.result}), nil
		})
		got, err := a.Complete(context.Background(), f.p, f.original, previous)
		if err != nil || got.Registered == nil || got.Registered.AccountID != "account" || got.Registered.NetworkID != "network" || got.Registered.NodeID != "node" || got.Registered.Credential != f.result.NodeCredential || got.Snapshot.PollToken != f.token || got.Snapshot.Request.Status != "enrolled" || calls != 1 {
			t.Fatal("verified completion missing", err)
		}
		if f.p.Attempt.NodeID != "" || f.p.Secrets.NodeCredential != "" {
			t.Fatal("transport mutated local durable state")
		}
		for _, value := range []any{got, &got, got.Registered, *got.Registered} {
			if strings.Contains(fmt.Sprintf("%#v", value), f.token) || strings.Contains(fmt.Sprint(value), f.result.NodeCredential) {
				t.Fatal("completion formatting leaked authority")
			}
		}
	}
}

func TestApprovalCompleteRejectsUnboundUnsignedOrPartialRegistration(t *testing.T) {
	for _, name := range []string{"missing", "approved-with-credential", "account", "network", "node", "node-network", "operation", "binding", "wg", "identity", "fingerprint", "hostname", "signature", "purpose", "scope", "expired", "credential-size", "owner", "receipt", "completed", "duplicate", "created-status"} {
		t.Run(name, func(t *testing.T) {
			f := newApprovalFixture(t, false)
			f.approve()
			previous := f.pending()
			previous.PollAfterSeconds = 0
			f.now = f.now.Add(time.Second)
			f.enrolled(t)
			claims, err := coordinator.DecodeNodeCredential(f.result.NodeCredential, 8192)
			if err != nil {
				t.Fatal(err)
			}
			registration := &f.result
			switch name {
			case "missing":
				registration = nil
			case "approved-with-credential":
				f.view.Status, f.view.NodeID, f.view.CompletedAt = "approved", "", nil
			case "account":
				f.result.Network.AccountID = "foreign"
			case "network":
				f.result.Network.ID = "foreign"
			case "node":
				f.result.Node.ID = "foreign"
			case "node-network":
				f.result.Node.NetworkID = "foreign"
			case "operation":
				f.result.IdempotencyID = "foreign"
			case "binding":
				f.result.RegistrationBinding = "foreign"
			case "wg":
				f.result.Node.PublicKey = "foreign"
			case "identity":
				f.result.Node.IdentityPublicKey = "foreign"
			case "fingerprint":
				f.result.Node.DeviceFingerprint = "foreign"
			case "hostname":
				f.result.Node.Hostname = "foreign"
			case "signature":
				f.key = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{12}, 32))
				f.signCompletion(t, claims.KeyID, "node:register", f.now.Add(time.Hour))
			case "purpose":
				var bundles signing.ServerKeyResponse
				if json.Unmarshal(f.announcement.Bundle, &bundles) != nil {
					t.Fatal("trust fixture invalid")
				}
				f.signCompletion(t, bundles.TrustBundle.ActiveKeyID, "node:register", f.now.Add(time.Hour))
			case "scope":
				f.signCompletion(t, claims.KeyID, "node:map", f.now.Add(time.Hour))
			case "expired":
				f.signCompletion(t, claims.KeyID, "node:register", f.now)
			case "owner":
				f.view.ApprovedByUserID = "other-owner"
			case "receipt":
				f.view.NetworkName = "other approval"
			case "completed":
				stamp := f.now.Add(-2 * time.Second)
				f.view.CompletedAt = &stamp
			}
			a := f.approval(t, func(req *http.Request) (*http.Response, error) {
				response := approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: registration})
				if name == "duplicate" {
					raw := encode(t, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: registration})
					raw = bytes.Replace(raw, []byte(`"schema_version":2`), []byte(`"schema_version":2,"schema_version":2`), 1)
					response.Body = io.NopCloser(bytes.NewReader(raw))
				}
				if name == "created-status" {
					response.StatusCode = http.StatusCreated
				}
				return response, nil
			})
			if name == "credential-size" {
				a.registration.config.MaxCredentialBytes = 1
			}
			got, err := a.Complete(context.Background(), f.p, f.original, previous)
			if err == nil || got.Registered != nil || got.Snapshot.Request.ID != "" || got.Snapshot.PollToken != "" {
				t.Fatal("unverified completion escaped")
			}
		})
	}
}

func TestApprovalCompleteKeepsApprovedWithoutRegistrationAndRecoversOriginalResult(t *testing.T) {
	f := newApprovalFixture(t, false)
	f.approve()
	previous := f.pending()
	previous.PollAfterSeconds = 0
	calls := 0
	a := f.approval(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view}), nil
		}
		if calls == 2 {
			return nil, errors.New("secret-poll-session-node")
		}
		if req.Header.Get("Authorization") != "Bearer "+previous.PollToken || req.ContentLength != 0 {
			t.Fatal("recovery changed original authority or operation")
		}
		return approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: &f.result}), nil
	})
	got, err := a.Complete(context.Background(), f.p, f.original, previous)
	if err != nil || got.Registered != nil || got.Snapshot.Request.Status != "approved" {
		t.Fatal("approval became premature registration", err)
	}
	f.now = f.now.Add(time.Second)
	f.enrolled(t)
	f.now = f.view.ExpiresAt.Add(-10 * time.Second)
	if _, err := a.Complete(context.Background(), f.p, f.original, previous); !errors.Is(err, ErrRegistrationUnavailable) || strings.Contains(err.Error(), "secret") || calls != 2 {
		t.Fatal("lost response retried or leaked", err)
	}
	// The future executor, not the transport, persists the original uncertainty
	// deadline. This fixture supplies that existing private state explicitly.
	f.p.Attempt.Uncertain = true
	f.p.Attempt.RecoveryUntil = f.now.Add(enrollment.RecoveryTTL)
	f.now = f.view.ExpiresAt.Add(time.Second)
	got, err = a.Complete(context.Background(), f.p, f.original, previous)
	if err != nil || got.Registered == nil || got.Registered.NodeID != "node" || calls != 3 || got.Snapshot.PollToken != previous.PollToken {
		t.Fatal("original post-expiry result not recovered", err)
	}
	f.now = f.p.Attempt.RecoveryUntil
	if _, err := a.Complete(context.Background(), f.p, f.original, previous); !errors.Is(err, context.DeadlineExceeded) || calls != 3 {
		t.Fatal("recovery deadline extended or call dispatched", err)
	}
}

func TestApprovalCompleteNeverDispatchesPendingOrExpiredNormalWaiting(t *testing.T) {
	f := newApprovalFixture(t, false)
	a := f.approval(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("completion dispatched without live approval")
		return nil, errors.New("unexpected")
	})
	if _, err := a.Complete(context.Background(), f.p, f.original, f.pending()); err == nil {
		t.Fatal("pending accepted for completion")
	}
	f.approve()
	previous := f.pending()
	previous.PollAfterSeconds = 0
	f.now = f.view.ExpiresAt
	if _, err := a.Complete(context.Background(), f.p, f.original, previous); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expired normal waiting extended", err)
	}
}

func TestDirectRegistrationRetainsNativeSessionGateAfterSharedCredentialVerification(t *testing.T) {
	for _, expiry := range []string{"missing", "expired"} {
		t.Run(expiry, func(t *testing.T) {
			f := newRegistrationFixture(t)
			if expiry == "missing" {
				f.p.Secrets.SessionExpiresAt = time.Time{}
			} else {
				f.p.Secrets.SessionExpiresAt = f.now
			}
			r := f.adapter(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("native session gate bypassed")
				return nil, errors.New("unexpected")
			})
			if got, err := r.Register(context.Background(), f.p, f.original); !errors.Is(err, context.DeadlineExceeded) || got != (RegisteredResult{}) {
				t.Fatal("native dispatch accepted missing/expired session", err)
			}
			if got, err := r.verify(f.p, f.request, f.result); !errors.Is(err, context.DeadlineExceeded) || got != (RegisteredResult{}) {
				t.Fatal("native result acceptance lost session gate", err)
			}
		})
	}
}

func TestApprovalCompleteRejectsCancelledAndLateVerifiedBody(t *testing.T) {
	for _, mode := range []string{"cancel", "late"} {
		t.Run(mode, func(t *testing.T) {
			f := newApprovalFixture(t, true)
			f.approve()
			previous := f.pending()
			previous.PollAfterSeconds = 0
			f.now = f.now.Add(time.Second)
			f.enrolled(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := f.approval(t, func(req *http.Request) (*http.Response, error) {
				response := approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: &f.result})
				if mode == "cancel" {
					cancel()
				} else {
					f.now = f.p.Attempt.Deadline
				}
				return response, nil
			})
			got, err := a.Complete(ctx, f.p, f.original, previous)
			if err == nil || got.Registered != nil || got.Snapshot.Request.ID != "" {
				t.Fatal("late or cancelled result accepted")
			}
		})
	}
}
