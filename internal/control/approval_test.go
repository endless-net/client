package control

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

const approvalBase = "https://approval.example.test/devices"

type approvalFixture struct {
	registrationFixture
	view  coordinator.NodeEnrollmentRequest
	token string
}

func newApprovalFixture(t *testing.T, assigned bool) approvalFixture {
	t.Helper()
	f := newRegistrationFixture(t)
	f.p.RemoteInFlight = false
	target := ApprovalTarget{NetworkName: "Requested network"}
	if assigned {
		target.AccountID, target.NetworkID = f.p.Attempt.AccountID, f.p.Attempt.NetworkID
	} else {
		f.p.Attempt.Phase = enrollment.ChoosingContext
		f.p.Attempt.AccountID, f.p.Attempt.NetworkID = "", ""
	}
	raw, err := PrepareApproval(f.p, target)
	if err != nil {
		t.Fatal(err)
	}
	q, err := coordinator.DecodeCreateNodeEnrollmentRequest(raw, len(raw))
	if err != nil {
		t.Fatal(err)
	}
	f.original, f.request = raw, q
	f.p.RemoteInFlight = true
	f.p.Secrets.RegistrationRequest = string(raw)
	v := coordinator.NodeEnrollmentRequest{ID: "opaque-request-1", Status: "pending", AccountID: q.AccountID, NetworkID: q.NetworkID, NetworkName: q.NetworkName, Hostname: q.Hostname, PublicKey: q.PublicKey, IdentityPublicKey: q.IdentityPublicKey, DeviceFingerprint: q.DeviceFingerprint, CreatedAt: f.now, ExpiresAt: f.now.Add(enrollment.ApprovalTTL), ApprovalURL: approvalBase + "?enrollment_request=opaque-request-1"}
	return approvalFixture{registrationFixture: f, view: v, token: "poll_" + strings.Repeat("p", 43)}
}

func (f *approvalFixture) approval(t *testing.T, roundTrip transport) *Approval {
	t.Helper()
	a, err := NewApproval(&http.Client{Transport: roundTrip}, staticAnnouncement{f.announcement}, ApprovalConfig{RegistrationConfig: RegistrationConfig{MaxRequestBytes: 16384, MaxResponseBytes: 65536, MaxCredentialBytes: 8192, Now: func() time.Time { return f.now }}, ApprovalBase: approvalBase})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *approvalFixture) pending() ApprovalSnapshot {
	return ApprovalSnapshot{Request: f.view, PollToken: f.token, PollAfterSeconds: 2, ObservedAt: f.now}
}

func approvalResponse(t *testing.T, req *http.Request, value any) *http.Response {
	t.Helper()
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(encode(t, value))), Request: req}
}

func (f *approvalFixture) approve() {
	stamp := f.now
	f.view.Status = "approved"
	f.view.AccountID, f.view.NetworkID, f.view.NetworkName = "account", "network", "Resolved network"
	f.view.ApprovedByUserID, f.view.ApprovedAt = "owner", &stamp
	f.view.Tags = []string{"approved-tag"}
}

func TestApprovalCreatePollAndReplayKeepCredentialClassesAndOriginalBindings(t *testing.T) {
	for _, assigned := range []bool{false, true} {
		f := newApprovalFixture(t, assigned)
		calls := 0
		a := f.approval(t, func(req *http.Request) (*http.Response, error) {
			calls++
			deadline, ok := req.Context().Deadline()
			if !ok || time.Until(deadline) > 30*time.Second || req.Header.Get("Cookie") != "" || req.Header.Get("X-EndlessNet-Node-Credential") != "" {
				t.Fatal("missing budget or mixed credentials")
			}
			if req.Method == http.MethodPost {
				body, err := io.ReadAll(req.Body)
				if err != nil || !bytes.Equal(body, f.original) || req.URL.String() != f.p.ControlOrigin+"/nodes/enrollment-requests" || req.Header.Get("Authorization") != "" {
					t.Fatal("create input or credentials changed")
				}
				response := approvalResponse(t, req, coordinator.CreateNodeEnrollmentRequestResponse{Request: f.view, PollToken: f.token, PollAfterSeconds: 2})
				if calls == 1 {
					response.StatusCode = http.StatusCreated
				}
				return response, nil
			}
			if req.Method != http.MethodGet || req.URL.String() != f.p.ControlOrigin+"/nodes/enrollment-requests/opaque-request-1" || req.Header.Get("Authorization") != "Bearer "+f.token || req.ContentLength != 0 {
				t.Fatal("poll route or poll-only bearer changed")
			}
			return approvalResponse(t, req, coordinator.NodeEnrollmentRequestStatusResponse{Request: f.view}), nil
		})
		saved, err := a.Create(context.Background(), f.p, f.original, nil)
		if err != nil || saved.Request.ID != f.view.ID || saved.PollToken != f.token {
			t.Fatal("create result missing", err)
		}
		f.now = f.now.Add(2 * time.Second)
		f.approve()
		approved, err := a.Poll(context.Background(), f.p, f.original, saved)
		if err != nil || approved.Request.Status != "approved" || approved.Request.AccountID != "account" || approved.Request.NodeID != "" || approved.PollAfterSeconds != 0 {
			t.Fatal("approved observation lost or promoted to Node", err)
		}
		if _, err := a.Poll(context.Background(), f.p, f.original, approved); err != nil {
			t.Fatal("zero terminal poll hint rejected on next observation", err)
		}
		if _, err := a.Create(context.Background(), f.p, f.original, &approved); err != nil || calls != 4 {
			t.Fatal("original create replay rejected", err)
		}
		if f.p.Attempt.NodeID != "" || f.p.Secrets.NodeCredential != "" {
			t.Fatal("HTTP adapter persisted or invented registration")
		}
		for _, formatted := range []string{fmt.Sprint(saved), fmt.Sprintf("%#v", &saved)} {
			if strings.Contains(formatted, f.token) {
				t.Fatal("snapshot formatting leaked poll token")
			}
		}
	}
}

func TestApprovalRejectsForeignMalformedAndRegressedResponses(t *testing.T) {
	for _, name := range []string{"device", "hostname", "public", "identity", "id", "url", "created", "ttl", "account", "network", "name", "tags", "owner", "stamp", "status", "node", "hint", "regress", "token"} {
		t.Run(name, func(t *testing.T) {
			f := newApprovalFixture(t, true)
			f.approve()
			previous := f.pending()
			previous.PollAfterSeconds = 0
			// Detach decision timestamp/tags so mutation cannot modify prior input.
			stamp := *f.view.ApprovedAt
			f.view.ApprovedAt = &stamp
			switch name {
			case "device":
				f.view.DeviceFingerprint = "foreign"
			case "hostname":
				f.view.Hostname = "foreign"
			case "public":
				f.view.PublicKey = "foreign"
			case "identity":
				f.view.IdentityPublicKey = "foreign"
			case "id":
				f.view.ID = "other-request"
				f.view.ApprovalURL = approvalBase + "?enrollment_request=other-request"
			case "url":
				f.view.ApprovalURL = "https://attacker.invalid/?enrollment_request=" + f.view.ID
			case "created":
				f.view.CreatedAt = f.view.CreatedAt.Add(-time.Second)
			case "ttl":
				f.view.ExpiresAt = f.view.ExpiresAt.Add(time.Second)
			case "account":
				f.view.AccountID = "foreign"
			case "network":
				f.view.NetworkID = "foreign"
			case "name":
				f.view.NetworkName = "another resolved name"
			case "tags":
				f.view.Tags = []string{"other-tag"}
			case "owner":
				f.view.ApprovedByUserID = "other-owner"
			case "stamp":
				stamp = stamp.Add(-time.Nanosecond)
			case "status":
				f.view.Status = "unknown"
			case "node":
				f.view.NodeID = "invented-before-completion"
			case "hint":
				// A non-pending status poll has no next waiting interval.
			case "regress":
				f.view.Status = "pending"
				f.view.ApprovedAt, f.view.ApprovedByUserID, f.view.Tags = nil, "", nil
				f.view.NetworkName = f.request.NetworkName
			case "token":
				f.token = "changed_" + strings.Repeat("q", 43)
			}
			a := f.approval(t, func(req *http.Request) (*http.Response, error) {
				if name == "token" {
					return approvalResponse(t, req, coordinator.CreateNodeEnrollmentRequestResponse{Request: f.view, PollToken: f.token, PollAfterSeconds: 1}), nil
				}
				hint := 0
				if name == "hint" || name == "regress" {
					hint = 1
				}
				return approvalResponse(t, req, coordinator.NodeEnrollmentRequestStatusResponse{Request: f.view, PollAfterSeconds: hint}), nil
			})
			var err error
			if name == "token" {
				_, err = a.Create(context.Background(), f.p, f.original, &previous)
			} else {
				_, err = a.Poll(context.Background(), f.p, f.original, previous)
			}
			if !errors.Is(err, ErrApprovalProtocol) {
				t.Fatal("invalid or rebound response accepted", err)
			}
		})
	}
}

func TestApprovalNoDispatchWithoutFrozenClaimTrustSafeSelectorAndBudget(t *testing.T) {
	for _, name := range []string{"claim", "input", "trust", "token", "path", "overall", "recovery", "approval", "cancel", "future-observation", "future-marker", "hint"} {
		t.Run(name, func(t *testing.T) {
			f := newApprovalFixture(t, false)
			previous := f.pending()
			f.now = f.now.Add(2 * time.Second)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "claim":
				f.p.RemoteInFlight = false
			case "input":
				f.original = append(f.original, ' ')
			case "trust":
				f.p.Trust.AnnouncementID = "changed"
			case "token":
				previous.PollToken += "\r\nAuthorization: other"
			case "path":
				previous.Request.ID = "../another-route?secret=yes"
			case "overall":
				f.now = f.p.Attempt.Deadline
			case "recovery":
				f.p.Attempt.Uncertain = true
				f.p.Attempt.RecoveryUntil = f.now.Add(time.Second)
				f.now = f.now.Add(2 * time.Second)
			case "approval":
				f.now = f.view.ExpiresAt
			case "cancel":
				cancel()
			case "future-observation":
				previous.ObservedAt = f.now.Add(time.Second)
			case "future-marker":
				f.p.InFlightAt = f.now.Add(time.Second)
			case "hint":
				f.now = previous.ObservedAt.Add(time.Second)
			}
			a := f.approval(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid call dispatched")
				return nil, errors.New("unexpected")
			})
			if _, err := a.Poll(ctx, f.p, f.original, previous); err == nil {
				t.Fatal("invalid call accepted")
			}
		})
	}
}

func TestApprovalRecoveryCanObserveOriginalCommittedOutcomeAfterRequestExpiry(t *testing.T) {
	f := newApprovalFixture(t, false)
	previous := f.pending()
	f.now = f.now.Add(time.Second)
	f.approve()
	completed := f.now.Add(time.Second)
	f.view.Status, f.view.NodeID, f.view.CompletedAt = "enrolled", "original-node", &completed
	f.now = f.view.ExpiresAt.Add(10 * time.Second)
	f.p.Attempt.Uncertain = true
	f.p.Attempt.RecoveryUntil = f.now.Add(60 * time.Second)
	a := f.approval(t, func(req *http.Request) (*http.Response, error) {
		return approvalResponse(t, req, coordinator.NodeEnrollmentRequestStatusResponse{Request: f.view}), nil
	})
	got, err := a.Poll(context.Background(), f.p, f.original, previous)
	if err != nil || got.Request.NodeID != "original-node" || got.PollToken != previous.PollToken || got.Request.AccountID != "account" {
		t.Fatal("original outcome not recoverable", err)
	}
	if f.p.Attempt.NodeID != "" || f.p.Secrets.NodeCredential != "" {
		t.Fatal("status without signed credential became local registered state")
	}
}

func TestApprovalFailuresHaveNoRetryOrSecretDiagnostic(t *testing.T) {
	for _, name := range []string{"lost", "http", "media", "oversize", "duplicate", "late"} {
		t.Run(name, func(t *testing.T) {
			f := newApprovalFixture(t, false)
			calls := 0
			a := f.approval(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if name == "lost" {
					return nil, errors.New("secret-poll-and-session")
				}
				response := approvalResponse(t, req, coordinator.CreateNodeEnrollmentRequestResponse{Request: f.view, PollToken: f.token, PollAfterSeconds: 1})
				switch name {
				case "http":
					response.StatusCode = http.StatusForbidden
				case "media":
					response.Header.Set("Content-Type", "text/html")
				case "oversize":
					response.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 65537)))
				case "duplicate":
					response.Body = io.NopCloser(strings.NewReader(`{"request":{},"poll_token":"first","poll_token":"second","poll_after_seconds":1}`))
				case "late":
					f.now = f.p.Attempt.Deadline
				}
				return response, nil
			})
			_, err := a.Create(context.Background(), f.p, f.original, nil)
			if err == nil || calls != 1 || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), f.token) {
				t.Fatal("failure retried, leaked or promoted to success", err)
			}
		})
	}
}

func TestApprovalRequiresExplicitTrustedBrowserBaseWithoutConstructorIO(t *testing.T) {
	f := newApprovalFixture(t, false)
	for _, base := range []string{"", "http://approval.example.test/devices", "https://user:secret@approval.example.test/devices", approvalBase + "?poll_token=secret", approvalBase + "#secret", "https://APPROVAL.example.test/devices"} {
		_, err := NewApproval(&http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			t.Fatal("constructor performed IO")
			return nil, errors.New("unexpected")
		})}, staticAnnouncement{f.announcement}, ApprovalConfig{RegistrationConfig: RegistrationConfig{MaxRequestBytes: 16384, MaxResponseBytes: 65536, MaxCredentialBytes: 8192}, ApprovalBase: base})
		if !errors.Is(err, ErrApprovalProtocol) {
			t.Fatal("untrusted browser base accepted")
		}
	}
	_ = f.approval(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("valid constructor performed IO")
		return nil, errors.New("unexpected")
	})
}

func TestApprovalRejectedAndExpiredAreObservationsWithoutRegistration(t *testing.T) {
	for _, status := range []string{"rejected", "expired"} {
		t.Run(status, func(t *testing.T) {
			f := newApprovalFixture(t, false)
			previous := f.pending()
			f.now = f.now.Add(2 * time.Second)
			f.view.Status = status
			if status == "rejected" {
				stamp := f.now
				f.view.AccountID, f.view.RejectedByUserID, f.view.RejectedAt = "account", "owner", &stamp
				f.view.RejectionReason = "Device not permitted"
			} else {
				f.now = f.view.ExpiresAt.Add(time.Second)
				f.p.Attempt.Uncertain = true
				f.p.Attempt.RecoveryUntil = f.now.Add(time.Minute)
			}
			a := f.approval(t, func(req *http.Request) (*http.Response, error) {
				return approvalResponse(t, req, coordinator.NodeEnrollmentRequestStatusResponse{Request: f.view}), nil
			})
			got, err := a.Poll(context.Background(), f.p, f.original, previous)
			if err != nil || got.Request.Status != status || got.Request.NodeID != "" || got.PollAfterSeconds != 0 || f.p.Attempt.NodeID != "" {
				t.Fatal("terminal observation lost or promoted", err)
			}
		})
	}
}
