package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
)

func (f *approvalFixture) approvalExecutor(t *testing.T, trip transport) *ApprovalExecutor {
	t.Helper()
	if err := saveApprovalProfile(t, f, f.p); err != nil {
		t.Fatal(err)
	}
	e, err := NewApprovalExecutor(f.store, f.approval(t, trip), ApprovalExecutorConfig{MaxCommitRetries: 3, RetryInterval: time.Second, WriteBudget: time.Second, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func approvalSendClaim(t *testing.T, f *approvalFixture, req *http.Request) {
	t.Helper()
	p := f.persisted(t)
	if !p.RemoteInFlight || p.InFlightAt.IsZero() || !p.Attempt.BrowserApproval || p.Secrets.RegistrationRequest != string(f.original) || p.Secrets.NodeCredential != "" {
		t.Fatal("approval RPC preceded its exact durable send claim")
	}
	if req.Header.Get("Cookie") != "" || req.Header.Get("X-EndlessNet-Node-Credential") != "" {
		t.Fatal("approval mixed credentials")
	}
	if deadline, ok := req.Context().Deadline(); !ok || time.Until(deadline) > 30*time.Second {
		t.Fatal("approval RPC not bounded")
	}
}

func TestApprovalExecutorLostCreateAndCompleteRecoverOriginal(t *testing.T) {
	for _, lost := range []string{"create", "complete"} {
		t.Run(lost, func(t *testing.T) {
			f, _ := newApprovalStateFixture(t, false)
			createCalls, pollCalls, completeCalls := 0, 0, 0
			e := f.approvalExecutor(t, func(req *http.Request) (*http.Response, error) {
				approvalSendClaim(t, &f, req)
				switch {
				case req.URL.Path == "/nodes/enrollment-requests":
					createCalls++
					raw, err := io.ReadAll(req.Body)
					if err != nil || !bytes.Equal(raw, f.original) || req.Header.Get("Authorization") != "" {
						t.Fatal("Create changed original device input", err)
					}
					if lost == "create" && createCalls == 1 {
						return nil, errors.New("private lost Create reply")
					}
					return approvalResponse(t, req, coordinator.CreateNodeEnrollmentRequestResponse{Request: f.view, PollToken: f.token, PollAfterSeconds: 2}), nil
				case req.Method == http.MethodGet:
					pollCalls++
					if req.Header.Get("Authorization") != "Bearer "+f.token {
						t.Fatal("poll replaced original capability")
					}
					f.approve()
					return approvalResponse(t, req, coordinator.NodeEnrollmentRequestStatusResponse{Request: f.view}), nil
				default:
					completeCalls++
					if completeCalls == 1 {
						f.enrolled(t)
					}
					if req.URL.Path != "/nodes/enrollment-requests/"+f.view.ID+"/complete" || req.Header.Get("Authorization") != "Bearer "+f.token {
						t.Fatal("Complete changed original capability/path")
					}
					if lost == "complete" && completeCalls == 1 {
						return nil, errors.New("private lost Complete reply")
					}
					return approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: &f.result}), nil
				}
			})
			deadline := f.p.Attempt.ApprovalUntil
			var recovery time.Time
			for step := 0; step < 6; step++ {
				due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
				if err != nil {
					t.Fatal("original approval reconciliation failed", step, err)
				}
				p := f.persisted(t)
				if !p.Attempt.ApprovalUntil.Equal(deadline) || p.Secrets.RegistrationRequest != string(f.original) || p.Attempt.Binding != f.p.Attempt.Binding {
					t.Fatal("recovery replaced original identity/input/deadline")
				}
				if p.Attempt.Uncertain {
					if recovery.IsZero() {
						recovery = p.Attempt.RecoveryUntil
					}
					if !p.Attempt.RecoveryUntil.Equal(recovery) {
						t.Fatal("recovery deadline extended")
					}
				}
				if p.Attempt.Phase == enrollment.Registered {
					if !due.IsZero() || p.RemoteInFlight || p.Attempt.Uncertain || p.Secrets.NodeCredential != f.result.NodeCredential || p.Attempt.NodeID != "node" {
						t.Fatal("credential/outcome was not committed together")
					}
					calls := createCalls + pollCalls + completeCalls
					if _, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || calls != createCalls+pollCalls+completeCalls {
						t.Fatal("registered attempt called remote again", err)
					}
					if pollCalls != 1 || createCalls != map[string]int{"create": 2, "complete": 1}[lost] || completeCalls != map[string]int{"create": 1, "complete": 2}[lost] {
						t.Fatal("unexpected call sequence", createCalls, pollCalls, completeCalls)
					}
					return
				}
				if due.IsZero() || !due.After(f.now) {
					t.Fatal("pending approval lost finite schedule", step)
				}
				// Polling before a saved server hint must perform no remote call.
				calls := createCalls + pollCalls + completeCalls
				if _, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || calls != createCalls+pollCalls+completeCalls {
					t.Fatal("pending hint bypassed", err)
				}
				f.now = due
				e, err = NewApprovalExecutor(f.store, e.remote, e.config)
				if err != nil {
					t.Fatal(err)
				}
			}
			t.Fatal("approval did not recover original result")
		})
	}
}

func TestApprovalExecutorUncertainClaimPreventsSendAndKeepsRecoveryBound(t *testing.T) {
	f, _ := newApprovalStateFixture(t, false)
	calls := 0
	e := f.approvalExecutor(t, func(req *http.Request) (*http.Response, error) {
		calls++
		approvalSendClaim(t, &f, req)
		return nil, errors.New("unavailable")
	})
	start := f.now
	f.memory.lose = true
	if _, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); !errors.Is(err, state.ErrUncertain) || calls != 0 {
		t.Fatal("uncertain local claim dispatched", err)
	}
	f.now = f.now.Add(time.Second)
	if _, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || calls != 1 {
		t.Fatal("original claim did not recover", err)
	}
	p := f.persisted(t)
	if !p.Attempt.RecoveryUntil.Equal(start.Add(enrollment.RecoveryTTL)) || !p.Attempt.Uncertain {
		t.Fatal("restart granted another recovery budget")
	}
	f.now = p.Attempt.RecoveryUntil
	if due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || !due.IsZero() || calls != 1 {
		t.Fatal("recovery expired into another call", err)
	}
	p = f.persisted(t)
	if p.Attempt.Phase != enrollment.Expired || p.Secrets.NodeCredential != "" {
		t.Fatal("unresolved outcome became registered")
	}
}

func TestApprovalExecutorAcceptsObservedTimeBeforeLocalCommitTime(t *testing.T) {
	f, _ := newApprovalStateFixture(t, false)
	e := f.approvalExecutor(t, nil)
	p := f.p
	p.RemoteInFlight, p.InFlightAt = true, f.now
	result := ApprovalCompletion{Snapshot: f.pending()}
	f.now = f.now.Add(time.Millisecond)
	next, err := e.apply(p, result, nil)
	if err != nil || next.Attempt.ObservedAt.Before(result.Snapshot.ObservedAt) || next.Approval.Snapshot == nil || next.RemoteInFlight {
		t.Fatal("valid earlier observation rejected at commit", err)
	}
}

func TestApprovalExecutorInvalidCompleteNeverInstallsCredential(t *testing.T) {
	f, _ := newApprovalStateFixture(t, true)
	f.approve()
	p := f.p
	a, err := p.Attempt.Approved(f.now)
	if err != nil {
		t.Fatal(err)
	}
	p.Attempt = &a
	snapshot := f.pending()
	snapshot.PollAfterSeconds = 0
	p.Approval.Snapshot = &snapshot
	f.p = p
	calls := 0
	e := f.approvalExecutor(t, func(req *http.Request) (*http.Response, error) {
		calls++
		approvalSendClaim(t, &f, req)
		f.enrolled(t)
		f.result.NodeCredential = "enc_" + strings.Repeat("A", 64)
		return approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: &f.result}), nil
	})
	if due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || !due.IsZero() {
		t.Fatal("invalid Complete did not block", err)
	}
	p = f.persisted(t)
	if p.Approval.BlockedReason != "client.approval.invalid_response" || p.Secrets.NodeCredential != "" || p.Attempt.NodeID != "" || !p.Attempt.Uncertain {
		t.Fatal("invalid credential escaped into durable state")
	}
	if _, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || calls != 1 {
		t.Fatal("invalid credential automatically retried", err)
	}
}

func TestApprovalExecutorRecoversLocallyCommittedSnapshotAndCredential(t *testing.T) {
	for _, phase := range []string{"create", "complete"} {
		t.Run(phase, func(t *testing.T) {
			f, _ := newApprovalStateFixture(t, true)
			if phase == "complete" {
				f.approve()
				a, err := f.p.Attempt.Approved(f.now)
				if err != nil {
					t.Fatal(err)
				}
				f.p.Attempt = &a
				s := f.pending()
				s.PollAfterSeconds = 0
				f.p.Approval.Snapshot = &s
			}
			calls := 0
			e := f.approvalExecutor(t, func(req *http.Request) (*http.Response, error) {
				calls++
				approvalSendClaim(t, &f, req)
				f.memory.lose = true
				if phase == "complete" {
					f.enrolled(t)
					return approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: &f.result}), nil
				}
				return approvalResponse(t, req, coordinator.CreateNodeEnrollmentRequestResponse{Request: f.view, PollToken: f.token, PollAfterSeconds: 2}), nil
			})
			if _, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); !errors.Is(err, state.ErrUncertain) {
				t.Fatal("lost local result ACK misreported", err)
			}
			p := f.persisted(t)
			if p.Approval.Snapshot == nil || p.RemoteInFlight {
				t.Fatal("durable original snapshot missing")
			}
			restarted, err := NewApprovalExecutor(f.store, e.remote, e.config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := restarted.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || calls != 1 {
				t.Fatal("locally committed result dispatched again", err)
			}
			if phase == "complete" && (p.Attempt.Phase != enrollment.Registered || p.Secrets.NodeCredential != f.result.NodeCredential) {
				t.Fatal("credential and receipt were not committed together")
			}
		})
	}
}

func TestApprovalExecutorConcurrentCancelKeepsOriginalCommittedResult(t *testing.T) {
	f, _ := newApprovalStateFixture(t, true)
	f.approve()
	a, err := f.p.Attempt.Approved(f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.p.Attempt = &a
	snapshot := f.pending()
	snapshot.PollAfterSeconds = 0
	f.p.Approval.Snapshot = &snapshot
	e := f.approvalExecutor(t, func(req *http.Request) (*http.Response, error) {
		approvalSendClaim(t, &f, req)
		p := f.persisted(t)
		a, err := p.Attempt.RecoverLostCall(p.InFlightAt, f.now)
		if err != nil {
			t.Fatal(err)
		}
		a, err = a.Cancel(f.now)
		if err != nil {
			t.Fatal(err)
		}
		p.Attempt = &a
		if err := saveApprovalProfile(t, &f, p); err != nil {
			t.Fatal(err)
		}
		f.enrolled(t)
		return approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: &f.result}), nil
	})
	if due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || !due.IsZero() {
		t.Fatal("verified original outcome could not resolve cancel", err)
	}
	p := f.persisted(t)
	if p.Attempt.Phase != enrollment.Registered || p.Attempt.Uncertain || p.Secrets.NodeCredential != f.result.NodeCredential || !p.Attempt.RecoveryUntil.Equal(f.now.Add(enrollment.RecoveryTTL)) {
		t.Fatal("cancel lost original committed outcome")
	}
}

func TestApprovalExecutorPollExpiryDoesNotRenewRequest(t *testing.T) {
	f, _ := newApprovalStateFixture(t, false)
	start := f.now
	polls := 0
	e := f.approvalExecutor(t, func(req *http.Request) (*http.Response, error) {
		approvalSendClaim(t, &f, req)
		if req.Method == http.MethodPost {
			return approvalResponse(t, req, coordinator.CreateNodeEnrollmentRequestResponse{Request: f.view, PollToken: f.token, PollAfterSeconds: 2}), nil
		}
		polls++
		if polls == 1 {
			return nil, errors.New("lost poll reply")
		}
		f.view.Status = "expired"
		return approvalResponse(t, req, coordinator.NodeEnrollmentRequestStatusResponse{Request: f.view}), nil
	})
	if _, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil {
		t.Fatal(err)
	}
	f.now = start.Add(599 * time.Second)
	if _, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil {
		t.Fatal(err)
	}
	f.now = start.Add(601 * time.Second)
	if due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || !due.IsZero() {
		t.Fatal("original expired observation not resolved", err)
	}
	p := f.persisted(t)
	if p.Attempt.Phase != enrollment.Expired || p.Attempt.Uncertain || p.RemoteInFlight || p.Secrets.NodeCredential != "" || p.Approval.Snapshot.Request.Status != "expired" || !p.Attempt.ApprovalUntil.Equal(start.Add(enrollment.ApprovalTTL)) || polls != 2 {
		t.Fatal("expiry created a new wait/request or Node")
	}
}

func TestApprovalExecutorEnrolledObservationWaitsForVerifiedComplete(t *testing.T) {
	f, _ := newApprovalStateFixture(t, false)
	calls := 0
	e := f.approvalExecutor(t, func(req *http.Request) (*http.Response, error) {
		calls++
		approvalSendClaim(t, &f, req)
		if calls == 1 {
			return approvalResponse(t, req, coordinator.CreateNodeEnrollmentRequestResponse{Request: f.view, PollToken: f.token, PollAfterSeconds: 2}), nil
		}
		if calls == 2 {
			f.approve()
			f.enrolled(t)
			return approvalResponse(t, req, coordinator.NodeEnrollmentRequestStatusResponse{Request: f.view}), nil
		}
		return approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: &f.result}), nil
	})
	due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if err != nil {
		t.Fatal(err)
	}
	f.now = due
	due, err = e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if err != nil {
		t.Fatal(err)
	}
	p := f.persisted(t)
	if p.Attempt.Phase != enrollment.Completing || p.Approval.Snapshot.Request.Status != "enrolled" || p.Secrets.NodeCredential != "" || p.Attempt.NodeID != "" || due.IsZero() {
		t.Fatal("enrolled poll granted registration")
	}
	f.now = due
	if due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || !due.IsZero() {
		t.Fatal("verified Complete did not finish", err)
	}
	if p := f.persisted(t); p.Attempt.Phase != enrollment.Registered || p.Secrets.NodeCredential != f.result.NodeCredential || calls != 3 {
		t.Fatal("verified Complete result missing")
	}
}

func TestApprovalExecutorCancelledAttemptDispatchesNothing(t *testing.T) {
	f, _ := newApprovalStateFixture(t, false)
	a, err := f.p.Attempt.Cancel(f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.p.Attempt = &a
	calls := 0
	e := f.approvalExecutor(t, func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected") })
	if due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || !due.IsZero() || calls != 0 {
		t.Fatal("cancelled attempt issued a remote call", err)
	}
}

func TestApprovalExecutorLostCompleteCrossesApprovalExpiryWithinOriginalRecovery(t *testing.T) {
	f, _ := newApprovalStateFixture(t, true)
	start := f.now
	f.approve()
	a, err := f.p.Attempt.Approved(f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.p.Attempt = &a
	snapshot := f.pending()
	snapshot.PollAfterSeconds = 0
	f.p.Approval.Snapshot = &snapshot
	calls := 0
	e := f.approvalExecutor(t, func(req *http.Request) (*http.Response, error) {
		calls++
		approvalSendClaim(t, &f, req)
		if calls == 1 {
			f.enrolled(t)
			f.now = start.Add(600 * time.Second)
			return nil, errors.New("lost original Complete reply at approval deadline")
		}
		return approvalResponse(t, req, coordinator.CompleteNodeEnrollmentRequestResponse{Request: f.view, Registration: &f.result}), nil
	})
	f.now = start.Add(599 * time.Second)
	due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt)
	if err != nil || due.IsZero() {
		t.Fatal("expiry discarded original uncertain operation", err)
	}
	p := f.persisted(t)
	if p.Attempt.Phase != enrollment.Completing || !p.Attempt.Uncertain || !p.Attempt.RecoveryUntil.Equal(start.Add(719*time.Second)) {
		t.Fatal("recovery did not retain the original send time")
	}
	f.now = due
	if due, err := e.Reconcile(context.Background(), f.p.ID, loginAttempt); err != nil || !due.IsZero() {
		t.Fatal("original committed replay after expiry failed", err)
	}
	p = f.persisted(t)
	if p.Attempt.Phase != enrollment.Registered || p.Secrets.NodeCredential != f.result.NodeCredential || !p.Attempt.ApprovalUntil.Equal(start.Add(600*time.Second)) || !p.Attempt.RecoveryUntil.Equal(start.Add(719*time.Second)) || calls != 2 {
		t.Fatal("expiry replay replaced the original operation or deadlines")
	}
}

func TestApprovalExecutorRechecksPurposeTrustAtLocalCredentialCommit(t *testing.T) {
	f, _ := newApprovalStateFixture(t, true)
	var bundle signing.ServerKeyResponse
	if err := json.Unmarshal(f.announcement.Bundle, &bundle); err != nil {
		t.Fatal(err)
	}
	end := f.now.Add(time.Second)
	bundle.NodeCredentialTrustBundle.Keys[0].NotAfter = &end
	announcement, err := DecodeAnnouncement(f.p.ControlOrigin, encode(t, bundle), f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.announcement = announcement
	f.p.Trust = state.ServerTrust{KeyID: announcement.KeyID, AnnouncementID: announcement.ID, Bundle: announcement.Bundle}
	f.approve()
	a, err := f.p.Attempt.Approved(f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.p.Attempt = &a
	snapshot := f.pending()
	snapshot.PollAfterSeconds = 0
	f.p.Approval.Snapshot = &snapshot
	e := f.approvalExecutor(t, nil)
	p := f.p
	p.RemoteInFlight, p.InFlightAt = true, f.now
	f.enrolled(t)
	result := RegisteredResult{AccountID: f.view.AccountID, NetworkID: f.view.NetworkID, NodeID: f.view.NodeID, Credential: f.result.NodeCredential, CredentialEnds: f.now.Add(time.Hour)}
	completion := ApprovalCompletion{Snapshot: f.pending(), Registered: &result}
	completion.Snapshot.PollAfterSeconds = 0
	f.now = end.Add(time.Millisecond)
	if next, err := e.apply(p, completion, nil); !errors.Is(err, ErrBootstrap) || next.Secrets.NodeCredential != "" {
		t.Fatal("expired purpose trust granted a durable credential", err)
	}
	if current := f.persisted(t); current.Secrets.NodeCredential != "" || current.Approval.Snapshot.Request.Status != "approved" {
		t.Fatal("failed commit check changed protected state")
	}
}

func TestApprovalExecutorUsesVerifiedCompletionBundleAfterKeyRotation(t *testing.T) {
	f, _ := newApprovalStateFixture(t, true)
	current := f.announcement
	// This unit fixture models the private proof returned by strict Fetch. Crypto,
	// protected checkpoint and HTTP are exercised by bootstrap_signed_test.go.
	current.verifiedID, current.verifiedOrigin = current.ID, current.Origin
	current.verifiedFrom, current.verifiedUntil = f.now.Add(-time.Minute), f.now.Add(time.Hour)
	var previous signing.ServerKeyResponse
	if err := json.Unmarshal(current.Bundle, &previous); err != nil {
		t.Fatal(err)
	}
	previous.NodeCredentialTrustBundle = fixture(t).NodeCredentialTrustBundle
	old, err := DecodeAnnouncement(f.p.ControlOrigin, encode(t, previous), f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.p.Trust = state.ServerTrust{KeyID: old.KeyID, AnnouncementID: old.ID, Bundle: old.Bundle}
	f.approve()
	a, err := f.p.Attempt.Approved(f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.p.Attempt = &a
	snapshot := f.pending()
	snapshot.PollAfterSeconds = 0
	f.p.Approval.Snapshot = &snapshot
	e := f.approvalExecutor(t, nil)
	p := f.p
	p.RemoteInFlight, p.InFlightAt = true, f.now
	f.enrolled(t)
	claims, err := coordinator.DecodeNodeCredential(f.result.NodeCredential, 65536)
	if err != nil {
		t.Fatal(err)
	}
	result := RegisteredResult{AccountID: f.view.AccountID, NetworkID: f.view.NetworkID, NodeID: f.view.NodeID, Credential: f.result.NodeCredential, CredentialEnds: claims.ExpiresAt}
	completion := ApprovalCompletion{Snapshot: f.pending(), Registered: &result, trust: current}
	completion.Snapshot.PollAfterSeconds = 0
	next, err := e.apply(p, completion, nil)
	if err != nil || next.Attempt.Phase != enrollment.Registered || next.Secrets.NodeCredential != f.result.NodeCredential {
		t.Fatal("new verified signing key permanently blocked by prior pin", err)
	}
	completion.trust.verifiedUntil = f.now
	if next, err := e.apply(p, completion, nil); !errors.Is(err, ErrBootstrap) || next.Secrets.NodeCredential != "" {
		t.Fatal("expired metadata reached local credential commit", err)
	}
}
