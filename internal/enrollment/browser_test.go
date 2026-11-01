package enrollment

import (
	"errors"
	"testing"
	"time"
)

func TestBrowserApprovalKeepsOneDeadlineAndBindsContextOnce(t *testing.T) {
	a := newAttempt(t)
	now := a.CreatedAt.Add(time.Minute)
	a, err := a.LoginSucceeded(now)
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.BeginBrowserApproval("", "", now)
	if err != nil || a.Validate() != nil || a.Phase != PendingApproval || a.AccountID != "" || !a.ApprovalUntil.Equal(now.Add(ApprovalTTL)) {
		t.Fatal("unassigned browser transition invalid", err)
	}
	deadline := a.ApprovalUntil
	if _, err := a.BeginBrowserApproval("", "", now.Add(time.Second)); !errors.Is(err, ErrTransition) {
		t.Fatal("approval restarted its deadline", err)
	}
	if _, err := a.Complete("node", now.Add(time.Hour), now); !errors.Is(err, ErrInvalid) {
		t.Fatal("unassigned observation registered a Node", err)
	}
	a, err = a.BindBrowserContext("owner-account", "owner-network", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.BindBrowserContext("other-account", "owner-network", now.Add(time.Second)); !errors.Is(err, ErrBinding) {
		t.Fatal("resolved browser context changed", err)
	}
	a, err = a.Approved(now.Add(time.Second))
	if err != nil || a.Phase != Completing || a.NodeID != "" || !a.ApprovalUntil.Equal(deadline) {
		t.Fatal("approval granted registration or extended wait", err)
	}
	a, err = a.Observe(deadline)
	if err != nil || a.Phase != Expired {
		t.Fatal("original browser expiry ignored", err)
	}
	if _, err := a.BindBrowserContext("owner-account", "owner-network", deadline); !errors.Is(err, ErrTransition) {
		t.Fatal("expired browser context revived", err)
	}
}

func TestBrowserCancelledUnknownContextUsesOriginalRecoveryWindow(t *testing.T) {
	a := newAttempt(t)
	now := a.CreatedAt.Add(time.Minute)
	a, err := a.LoginSucceeded(now)
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.BeginBrowserApproval("", "", now)
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.MarkUncertain(now)
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.Cancel(now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	recovery := a.RecoveryUntil
	if _, err := a.BindBrowserContext("account", "network", recovery); !errors.Is(err, ErrTransition) {
		t.Fatal("cancelled recovery budget extended", err)
	}
	a, err = a.BindBrowserContext("account", "network", now.Add(2*time.Second))
	if err != nil || a.Phase != Cancelled || !a.RecoveryUntil.Equal(recovery) {
		t.Fatal("original committed context not recoverable", err)
	}
	a, err = a.ResolveCommittedRegistration("node", now.Add(time.Hour), now.Add(2*time.Second))
	if err != nil || a.Validate() != nil || a.Phase != Registered || !a.RecoveryUntil.Equal(recovery) {
		t.Fatal("verified committed browser result could not resolve cancel", err)
	}
}
