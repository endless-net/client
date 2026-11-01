package enrollment

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func newAttempt(t *testing.T) Attempt {
	t.Helper()
	a, err := New(Binding{
		AttemptID: "c76b4188-757d-4c2a-a30d-e4826bdc3bce",
		RequestID: "7cc87d29-3317-4652-aa54-0efc23536a64",
		ProfileID: "profile", Owner: "os-principal", ControlOrigin: "https://control.example",
		DeviceKeyHash: sha256.Sum256([]byte("persisted device key")),
	}, time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func registering(t *testing.T) Attempt {
	t.Helper()
	a := newAttempt(t)
	a, err := a.LoginSucceeded(a.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.Select("account", "network", a.CreatedAt.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestVerifiedCommittedResultResolvesConcurrentCancelOnlyWithinRecovery(t *testing.T) {
	a := registering(t)
	now := a.ObservedAt.Add(time.Second)
	a, err := a.MarkUncertain(now)
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.Cancel(now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.ResolveCommittedRegistration("original-node", now.Add(time.Hour), now.Add(time.Second))
	if err != nil || got.Phase != Registered || got.Uncertain || got.Validate() != nil || !got.RecoveryUntil.Equal(a.RecoveryUntil) || !got.Deadline.Equal(a.Deadline) {
		t.Fatal("committed cancel race unresolved", err)
	}
	for _, at := range []time.Time{a.RecoveryUntil, a.Deadline} {
		if _, err := a.ResolveCommittedRegistration("node", at.Add(time.Hour), at); !errors.Is(err, ErrTransition) {
			t.Fatal("elapsed cancel revived", err)
		}
	}
	a.Uncertain = false
	if _, err := a.ResolveCommittedRegistration("node", now.Add(time.Hour), now); !errors.Is(err, ErrTransition) {
		t.Fatal("cancel without remote mutation revived", err)
	}
}

func TestApprovalIsNotRegistration(t *testing.T) {
	a := registering(t)
	deadline := a.CreatedAt.Add(12 * time.Minute)
	a, err := a.AwaitApproval(deadline, a.CreatedAt.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.Approved(a.CreatedAt.Add(3 * time.Minute))
	if err != nil || a.Phase != Completing || a.NodeID != "" {
		t.Fatalf("approval treated as registration: %v", err)
	}
	a, err = a.Complete("node-original", a.CreatedAt.Add(time.Hour), a.CreatedAt.Add(4*time.Minute))
	if err != nil || a.Phase != Registered || a.Uncertain {
		t.Fatalf("committed result rejected: %v", err)
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	replayed, err := a.Complete(a.NodeID, a.CredentialEnds, a.CreatedAt.Add(5*time.Minute))
	if err != nil || replayed != a {
		t.Fatalf("original result changed: %v", err)
	}
	if _, err := a.Complete("new-node", a.CredentialEnds, a.CreatedAt.Add(5*time.Minute)); !errors.Is(err, ErrBinding) {
		t.Fatalf("second Node accepted: %v", err)
	}
}

func TestRecoverySurvivesRestartWithoutExtension(t *testing.T) {
	a := registering(t)
	a, err := a.MarkUncertain(a.CreatedAt.Add(3 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	deadline, recovery := a.Deadline, a.RecoveryUntil
	for i := 1; i < 120; i++ {
		// Simulate rehydration of the same persisted record, not New/Enroll.
		a, err = a.Resume(a.Binding, a.CreatedAt.Add(3*time.Minute+time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		a, err = a.MarkUncertain(a.ObservedAt)
		if err != nil || a.Deadline != deadline || a.RecoveryUntil != recovery {
			t.Fatalf("restart/timeout extended deadline: %v", err)
		}
	}
	a, err = a.Observe(recovery)
	if err != nil || a.Phase != Expired || !a.Uncertain || a.NodeID != "" {
		t.Fatalf("unknown remote outcome treated as success/rollback: %v", err)
	}
	if _, err := a.Complete("late-node", recovery.Add(time.Hour), recovery); !errors.Is(err, ErrTransition) {
		t.Fatalf("late result restarted expired attempt: %v", err)
	}
}

func TestRecoveryCanReturnOriginalCommittedResult(t *testing.T) {
	a := registering(t)
	a, err := a.MarkUncertain(a.CreatedAt.Add(3 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.Complete("original-node", a.CreatedAt.Add(time.Hour), a.CreatedAt.Add(4*time.Minute))
	if err != nil || a.NodeID != "original-node" || a.Uncertain || a.Phase != Registered {
		t.Fatalf("recovered commit rejected: %v", err)
	}
}

func TestContextSelectionAndBindingCannotChange(t *testing.T) {
	a := registering(t)
	if _, err := a.Select("other", "network", a.ObservedAt); !errors.Is(err, ErrBinding) {
		t.Fatalf("context changed mid-attempt: %v", err)
	}
	for name, mutate := range map[string]func(*Binding){
		"key":    func(b *Binding) { b.DeviceKeyHash[0] ^= 1 },
		"owner":  func(b *Binding) { b.Owner = "other-user" },
		"origin": func(b *Binding) { b.ControlOrigin = "https://other.example" },
		"UUID":   func(b *Binding) { b.RequestID = "5ed78079-6197-49e3-82cf-ae5740279a87" },
	} {
		t.Run(name, func(t *testing.T) {
			b := a.Binding
			mutate(&b)
			if _, err := a.Resume(b, a.ObservedAt); !errors.Is(err, ErrBinding) {
				t.Fatalf("foreign continuation accepted: %v", err)
			}
		})
	}
}

func TestNoDeadlineExtensionAndNoFalseRevoke(t *testing.T) {
	a := registering(t)
	approval := a.CreatedAt.Add(12 * time.Minute)
	a, err := a.AwaitApproval(approval, a.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AwaitApproval(approval.Add(time.Second), a.ObservedAt.Add(time.Second)); !errors.Is(err, ErrBinding) {
		t.Fatalf("approval replay extended deadline: %v", err)
	}
	unknown, err := a.MarkUncertain(a.ObservedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := unknown.Cancel(unknown.ObservedAt)
	if err != nil || cancelled.Phase != Cancelled || !cancelled.Uncertain {
		t.Fatalf("cancel falsely proved remote revoke: %v", err)
	}
	denied, err := a.Reject(a.ObservedAt)
	if err != nil || denied.Phase != Denied || denied.NodeID != "" {
		t.Fatalf("denial treated as success: %v", err)
	}
	if _, err := denied.Complete("node", approval.Add(time.Hour), approval); !errors.Is(err, ErrTransition) {
		t.Fatalf("denied attempt registered: %v", err)
	}
	expired, err := a.Observe(approval)
	if err != nil || expired.Phase != Expired || expired.Uncertain {
		t.Fatalf("known approval deadline ignored: %v", err)
	}
}

func TestCorruptStateAndClockRollbackRejected(t *testing.T) {
	a := newAttempt(t)
	for name, corrupt := range map[string]func(*Attempt){
		"origin":   func(v *Attempt) { v.ControlOrigin = "http://control.example" },
		"UUID":     func(v *Attempt) { v.RequestID = "bad" },
		"deadline": func(v *Attempt) { v.Deadline = v.Deadline.Add(time.Second) },
		"key":      func(v *Attempt) { v.DeviceKeyHash = [32]byte{} },
		"node":     func(v *Attempt) { v.NodeID = "uncommitted" },
		"recovery": func(v *Attempt) { v.Uncertain = true },
	} {
		t.Run(name, func(t *testing.T) {
			bad := a
			corrupt(&bad)
			if _, err := bad.Observe(a.CreatedAt); err == nil {
				t.Fatal("corrupt store accepted")
			}
		})
	}
	if _, err := a.Observe(a.CreatedAt.Add(-time.Second)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("clock rollback accepted: %v", err)
	}
	expired, err := a.Observe(a.LoginUntil)
	if err != nil || expired.Phase != Expired {
		t.Fatalf("login deadline ignored: %v", err)
	}
}

func TestContextSelectionConsumesOverallBudget(t *testing.T) {
	a := newAttempt(t)
	a, err := a.LoginSucceeded(a.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	deadline := a.Deadline
	for minute := 2; minute < 22; minute++ {
		a, err = a.Resume(a.Binding, a.CreatedAt.Add(time.Duration(minute)*time.Minute))
		if err != nil || a.Deadline != deadline || a.Phase != ChoosingContext {
			t.Fatalf("context selection changed deadline: %v", err)
		}
	}
	a, err = a.Observe(deadline)
	if err != nil || a.Phase != Expired {
		t.Fatalf("context selection waited indefinitely: %v", err)
	}
}

func TestLateTimeoutDoesNotProveRemoteExpiry(t *testing.T) {
	a := newAttempt(t)
	// A call started before the login deadline may time out just after it.
	unknown, err := a.MarkUncertain(a.LoginUntil.Add(time.Second))
	if err != nil || !unknown.Uncertain || unknown.Phase != AwaitingLogin {
		t.Fatalf("late timeout falsely proved remote expiry: %v", err)
	}
	if unknown.RecoveryUntil != a.LoginUntil.Add(time.Second+RecoveryTTL) {
		t.Fatal("incorrect first recovery deadline")
	}
	late, err := a.MarkUncertain(a.Deadline.Add(time.Second))
	if err != nil || !late.Uncertain || late.Phase != Expired {
		t.Fatalf("overall deadline lost uncertainty: %v", err)
	}
}

func TestFirstRecoveryWindowIsNotRestartedAfterProgress(t *testing.T) {
	a := newAttempt(t)
	a, err := a.MarkUncertain(a.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	firstDeadline := a.RecoveryUntil
	a, err = a.LoginSucceeded(a.CreatedAt.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.Select("account", "network", a.CreatedAt.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.MarkUncertain(a.CreatedAt.Add(5 * time.Minute))
	if err != nil || a.RecoveryUntil != firstDeadline || a.Phase != Expired || !a.Uncertain {
		t.Fatalf("progress gave a second recovery budget: %v", err)
	}
}
