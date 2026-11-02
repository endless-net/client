package connectivity

import (
	"errors"
	"testing"
	"time"
)

func TestSourceGateCannotBeReopenedByOlderNativeCompletion(t *testing.T) {
	_, m, clock, l, cancel := leaseFixture(t, 300*time.Second)
	defer cancel()
	if err := l.Confirm(m); err != nil {
		t.Fatal(err)
	}
	old, _ := l.SourceSignal()
	first := l.BlockSource()
	l.PausePackets(false)
	if l.Permit(false) || l.Permit(true) {
		t.Fatal("native pause cleared source barrier")
	}
	second := l.BlockSource()
	if err := l.EndSourceTransition(first); !errors.Is(err, ErrSourceChanged) {
		t.Fatal("older completion reopened newer source change", err)
	}
	if err := l.EndSourceTransition(old); !errors.Is(err, ErrSourceChanged) {
		t.Fatal(err)
	}
	if err := l.EndSourceTransition(second); err != nil {
		t.Fatal(err)
	}
	if l.Permit(false) || !l.Permit(true) {
		t.Fatal("source completion created fresh authority or lost finite existing flows")
	}
	clock.advance(300*time.Second, 300*time.Second)
	if l.Permit(true) {
		t.Fatal("source barrier reset original authority deadline")
	}
	last := l.BlockSource()
	l.Revoke()
	if err := l.EndSourceTransition(last); !errors.Is(err, ErrRevoked) {
		t.Fatal("source transition resurrected revoke", err)
	}
}
