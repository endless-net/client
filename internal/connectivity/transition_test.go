package connectivity

import (
	"errors"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
)

func TestLeasePauseCannotMintRenewOrRestoreRevokedAuthority(t *testing.T) {
	_, m, clock, l, cancel := leaseFixture(t, 300*time.Second)
	defer cancel()
	l.PausePackets(true)
	l.PausePackets(false)
	if l.Permit(true) || l.Permit(false) {
		t.Fatal("pause created authority on a fresh lease")
	}
	if err := l.Confirm(m); err != nil {
		t.Fatal(err)
	}
	l.PausePackets(true)
	if l.Permit(true) || l.Permit(false) {
		t.Fatal("policy transition did not quiesce packet I/O")
	}
	clock.advance(200*time.Second, 200*time.Second)
	l.Unavailable()
	l.PausePackets(false)
	if !l.Permit(true) || l.Permit(false) {
		t.Fatal("resume widened offline grant")
	}
	clock.advance(100*time.Second, 100*time.Second)
	l.PausePackets(true)
	l.PausePackets(false)
	if l.Permit(true) {
		t.Fatal("pause renewed original receipt deadline")
	}
	l.Revoke()
	l.PausePackets(true)
	l.PausePackets(false)
	if l.Permit(true) || l.Permit(false) {
		t.Fatal("resume restored terminally revoked grant")
	}
}

func TestVerifiedMapBindingRejectsChangedTrustIntentAndGeneration(t *testing.T) {
	f, m, _, _, cancel := leaseFixture(t, time.Minute)
	defer cancel()
	if err := m.CheckBinding(f.profile, f.profile.Connection.Generation); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*state.Profile){
		func(p *state.Profile) { p.ID = "another-profile" },
		func(p *state.Profile) { p.Trust.AnnouncementID = "changed-anchor" },
		func(p *state.Profile) { c := *p.Connection; c.Intent = state.IntentDisconnected; p.Connection = &c },
		func(p *state.Profile) { c := *p.Connection; c.Generation++; p.Connection = &c },
	} {
		p := f.profile
		change(&p)
		if err := m.CheckBinding(p, f.profile.Connection.Generation); !errors.Is(err, state.ErrStale) {
			t.Fatal("stale verifier result matched current profile", err)
		}
	}
}
