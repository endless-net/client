package client

import (
	"errors"
	"testing"
)

func TestWindowsSessionOwnersConsumeIdentityAndRejectStaleLookup(t *testing.T) {
	for _, scenario := range []string{"logoff", "new_logon", "failed_logon"} {
		t.Run(scenario, func(t *testing.T) {
			started, finish := make(chan struct{}), make(chan struct{})
			calls := 0
			owners := newWindowsSessionOwners(func(uint32) (string, error) {
				calls++
				if calls == 1 {
					close(started)
					<-finish
					return "old-SID", nil
				}
				if scenario == "failed_logon" {
					return "", errors.New("denied")
				}
				return "new-SID", nil
			})
			done := make(chan error, 1)
			go func() { done <- owners.seed(7) }()
			<-started
			if scenario == "logoff" {
				if identity, err := owners.logoff(7); err == nil || identity != "" {
					t.Fatal("unresolved identity used")
				}
			} else if err := owners.logon(7); (err != nil) != (scenario == "failed_logon") {
				t.Fatal(err)
			}
			close(finish)
			if err := <-done; err == nil {
				t.Fatal("stale lookup restored an old owner")
			}
			identity, err := owners.logoff(7)
			if scenario == "new_logon" {
				if err != nil || identity != "new-SID" {
					t.Fatal("new session owner lost", err)
				}
			} else if err == nil || identity != "" {
				t.Fatal("unknown logoff acquired an owner")
			}
			if _, err := owners.logoff(7); err == nil {
				t.Fatal("duplicate logoff reused identity")
			}
		})
	}
}

func TestWindowsSessionOwnerSeedingAndBounds(t *testing.T) {
	calls := 0
	owners := newWindowsSessionOwners(func(uint32) (string, error) { calls++; return "SID", nil })
	for _, invalid := range []uint32{0, ^uint32(0)} {
		if err := owners.seed(invalid); err == nil {
			t.Fatal("invalid session accepted")
		}
	}
	if calls != 0 {
		t.Fatal("queried invalid session")
	}
	if err := owners.logon(1); err != nil {
		t.Fatal(err)
	}
	if err := owners.seed(1); err != nil || calls != 1 {
		t.Fatal("enumeration replaced a notification")
	}
	for id := uint32(2); id <= maxWindowsUserSessions; id++ {
		if err := owners.seed(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := owners.logon(maxWindowsUserSessions + 1); err == nil || calls != maxWindowsUserSessions {
		t.Fatal("unbounded session lookup")
	}
	if err := owners.logon(1); err != nil {
		t.Fatal("replacement incorrectly exceeded capacity", err)
	}
	owners.finishSeeding()
	if _, err := owners.logoff(1); err != nil {
		t.Fatal(err)
	}
	if err := owners.logon(maxWindowsUserSessions + 1); err != nil {
		t.Fatal("consumed session did not release capacity", err)
	}
}

func TestWindowsSessionEnumerationCannotResurrectLogoff(t *testing.T) {
	calls := 0
	owners := newWindowsSessionOwners(func(uint32) (string, error) { calls++; return "old-SID", nil })
	if _, err := owners.logoff(7); err == nil {
		t.Fatal("unknown owner accepted")
	}
	if err := owners.seed(7); err != nil || calls != 0 {
		t.Fatal("enumeration queried an already departed user")
	}
	owners.finishSeeding()
	if len(owners.owners) != 0 {
		t.Fatal("startup tombstones retained")
	}
	if err := owners.seed(7); err != nil || calls != 0 {
		t.Fatal("late enumeration resurrected logoff")
	}
	if err := owners.logon(7); err != nil || calls != 1 {
		t.Fatal("session ID could not be reused")
	}
	if identity, err := owners.logoff(7); err != nil || identity != "old-SID" {
		t.Fatal("bound owner lost", err)
	}
}
