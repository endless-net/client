package client

import (
	"errors"
	"reflect"
	"testing"
)

func TestWindowsSessionLookupRetryIsBoundedAndFair(t *testing.T) {
	var calls []uint32
	ready := false
	owners := newWindowsSessionOwners(func(id uint32) (string, error) {
		calls = append(calls, id)
		if !ready || id == 1 {
			return "", errors.New("unavailable")
		}
		return "recovered-SID", nil
	})
	for _, id := range []uint32{1, 2, 3} {
		if err := owners.seed(id); err == nil {
			t.Fatal("missing failure")
		}
	}
	owners.finishSeeding()
	calls = nil
	ready = true
	for range 4 {
		attempted, _ := owners.retryUnresolved()
		if !attempted {
			t.Fatal("unresolved owner not retried")
		}
	}
	if !reflect.DeepEqual(calls, []uint32{1, 2, 3, 1}) {
		t.Fatal("retry was unbounded or starved a session", calls)
	}
	for _, id := range []uint32{2, 3} {
		if owner, err := owners.logoff(id); err != nil || owner != "recovered-SID" {
			t.Fatal("recovered owner not delivered", err)
		}
	}
	if owner, err := owners.logoff(1); err == nil || owner != "" {
		t.Fatal("failed lookup inferred an owner")
	}
	if attempted, err := owners.retryUnresolved(); attempted || err != nil {
		t.Fatal("departed session was retried", err)
	}
}

func TestWindowsSessionRetryCannotRestoreDepartedOrReplacedOwner(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		started, finish := make(chan struct{}), make(chan struct{})
		calls := 0
		owners := newWindowsSessionOwners(func(uint32) (string, error) {
			calls++
			switch calls {
			case 1:
				return "", errors.New("temporarily unavailable")
			case 2:
				close(started)
				<-finish
				return "stale-SID", nil
			default:
				return "current-SID", nil
			}
		})
		if err := owners.seed(7); err == nil {
			t.Fatal("missing initial failure")
		}
		owners.finishSeeding()
		done := make(chan error, 1)
		go func() { _, err := owners.retryUnresolved(); done <- err }()
		<-started
		if attempted, err := owners.retryUnresolved(); attempted || err != nil {
			t.Fatal("duplicate concurrent lookup", err)
		}
		if replaced {
			if err := owners.logon(7); err != nil {
				t.Fatal(err)
			}
		} else {
			_, _ = owners.logoff(7)
		}
		close(finish)
		if err := <-done; err == nil {
			t.Fatal("stale retry committed")
		}
		owner, err := owners.logoff(7)
		if replaced {
			if err != nil || owner != "current-SID" {
				t.Fatal("replacement was overwritten", err)
			}
		} else if err == nil || owner != "" {
			t.Fatal("retry resurrected departed owner")
		}
	}
}
