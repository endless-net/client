package connectivity

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type leaseClock struct {
	mu     sync.Mutex
	sample ClockSample
	err    error
}

func (c *leaseClock) Sample() (ClockSample, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sample, c.err
}
func (c *leaseClock) advance(wall, uptime time.Duration) {
	c.mu.Lock()
	c.sample.Wall = c.sample.Wall.Add(wall)
	c.sample.Uptime += uptime
	c.sample.UpperUptime += uptime
	c.mu.Unlock()
}

func leaseFixture(t *testing.T, ttl time.Duration) (*mapFixture, Map, *leaseClock, *Lease, context.CancelFunc) {
	t.Helper()
	f := newMapFixture(t)
	clock := &leaseClock{sample: ClockSample{Wall: f.now, Uptime: time.Hour, UpperUptime: time.Hour, Epoch: "fixture-runtime"}}
	f.config.Clock = clock
	end := f.now.Add(ttl)
	f.value.Peers[0].EndpointExpiresAt = &end
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	m, err := v.Verify(context.Background(), f.sign(t, nil, f.now, end), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	l, err := NewLease(ctx, clock, f.profile.ID, f.profile.Connection.Generation)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return f, m, clock, l, cancel
}

func TestLeaseCurrentMapExposesOnlyAcceptedImmutableResultAndDoesNotGrantTraffic(t *testing.T) {
	_, m, _, l, cancel := leaseFixture(t, 30*time.Second)
	defer cancel()
	if !l.CurrentMap().ExpiresAt().IsZero() {
		t.Fatal("new lease exposed unconfirmed input")
	}
	if err := l.Confirm(m); err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{fmt.Sprint(l), fmt.Sprintf("%+v", l), fmt.Sprintf("%#v", l)} {
		if rendered != "[private connectivity lease]" {
			t.Fatal("lease formatting exposed retained sealed state")
		}
	}
	view, err := l.CurrentMap().Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	original := view.Node.ID
	view.Node.ID = "foreign"
	copy, err := l.CurrentMap().Snapshot()
	if err != nil || copy.Node.ID != original {
		t.Fatal("reader mutated confirmed map", err)
	}
	bad := m
	bad.profileID = "foreign"
	if l.Confirm(bad) == nil || l.CurrentMap().Cursor() != m.Cursor() {
		t.Fatal("rejected input replaced confirmed map")
	}
	l.Unavailable()
	if l.CurrentMap().Cursor() != m.Cursor() || l.Permissions().NewFlows {
		t.Fatal("map availability implied online authority")
	}
	l.Revoke()
	if l.Permissions().ExistingFlows {
		t.Fatal("retained sealed map bypassed revocation")
	}
}

func TestLeaseOfflinePreservesOnlyExistingFlowsUntilOriginalExpiry(t *testing.T) {
	_, m, clock, l, cancel := leaseFixture(t, 300*time.Second)
	defer cancel()
	if l.Permit(false) || l.Permit(true) {
		t.Fatal("new lease restored cached authority")
	}
	if err := l.Confirm(m); err != nil {
		t.Fatal(err)
	}
	if !l.Permit(false) || !l.Permit(true) {
		t.Fatal("fresh authoritative map denied")
	}
	clock.advance(100*time.Second, 100*time.Second)
	l.Unavailable()
	if l.Permit(false) || !l.Permit(true) {
		t.Fatal("offline widened new flows or lost selected existing-flow window")
	}
	clock.advance(199*time.Second, 199*time.Second)
	if !l.Permit(true) {
		t.Fatal("existing flow ended before approved original interval")
	}
	if err := l.Confirm(m); !errors.Is(err, ErrReplay) || l.Permit(false) {
		t.Fatal("replay re-enabled new flows", err)
	}
	clock.advance(time.Second, time.Second)
	if l.Permit(true) || l.Permit(false) {
		t.Fatal("existing flow continued beyond 300s from confirmation")
	}
}

func TestLeaseClipsShorterSignatureExpiryAndDelayedApplication(t *testing.T) {
	_, m, clock, l, cancel := leaseFixture(t, 30*time.Second)
	defer cancel()
	clock.advance(20*time.Second, 20*time.Second)
	if err := l.Confirm(m); err != nil {
		t.Fatal(err)
	}
	l.Unavailable()
	clock.advance(9*time.Second, 9*time.Second)
	if !l.Permit(true) {
		t.Fatal("valid remaining interval lost")
	}
	clock.advance(time.Second, time.Second)
	if l.Permit(true) {
		t.Fatal("Confirm call time extended original verified receipt/expiry")
	}
}

func TestLeaseSuspendRollbackRebootAndClockFailureNeverExtendAuthority(t *testing.T) {
	for _, name := range []string{"suspend-wall-stalled", "wall-forward", "wall-backward", "uptime-backward", "epoch-change", "clock-error", "uncertain-sampling"} {
		t.Run(name, func(t *testing.T) {
			_, m, clock, l, cancel := leaseFixture(t, 300*time.Second)
			defer cancel()
			if err := l.Confirm(m); err != nil {
				t.Fatal(err)
			}
			l.Unavailable()
			switch name {
			case "suspend-wall-stalled":
				clock.advance(0, 301*time.Second)
			case "wall-forward":
				clock.advance(301*time.Second, time.Second)
			case "wall-backward":
				clock.advance(-time.Nanosecond, time.Second)
			case "uptime-backward":
				clock.advance(time.Second, -time.Second)
			case "epoch-change":
				clock.mu.Lock()
				clock.sample.Epoch = "other-runtime"
				clock.mu.Unlock()
			case "clock-error":
				clock.mu.Lock()
				clock.err = errors.New("private-clock-diagnostic")
				clock.mu.Unlock()
			case "uncertain-sampling":
				clock.mu.Lock()
				clock.sample.UpperUptime = clock.sample.Uptime - time.Nanosecond
				clock.mu.Unlock()
			}
			if l.Permit(true) || l.Permit(false) {
				t.Fatal("unsafe/expired clock permitted traffic")
			}
			p := l.Permissions()
			if name != "suspend-wall-stalled" && name != "wall-forward" && !p.ClockUnsafe {
				t.Fatal("clock fault not visible")
			}
		})
	}
}

func TestLeaseRevocationAndIntentCancellationHaveNoGrace(t *testing.T) {
	for _, name := range []string{"signed-revoke", "explicit-denial", "disconnect"} {
		t.Run(name, func(t *testing.T) {
			f, m, clock, l, cancel := leaseFixture(t, time.Minute)
			defer cancel()
			if err := l.Confirm(m); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "signed-revoke":
				clock.advance(time.Second, time.Second)
				f.now = f.now.Add(time.Second)
				f.value.Node.Status = "revoked"
				f.value.Peers = []coordinator.Peer{}
				f.value.Revision.Network++
				f.value.Network.Revision = f.value.Revision.Network
				v, err := NewVerifier(f.profile, f.config)
				if err != nil {
					t.Fatal(err)
				}
				cursor := m.Cursor()
				denial, err := v.Verify(context.Background(), f.sign(t, &cursor, f.now, f.now.Add(time.Minute)), &cursor)
				if err != nil {
					t.Fatal(err)
				}
				if err := l.Confirm(denial); !errors.Is(err, ErrRevoked) {
					t.Fatal(err)
				}
			case "explicit-denial":
				l.Revoke()
			case "disconnect":
				cancel()
			}
			if l.Permit(false) || l.Permit(true) {
				t.Fatal("revoke/cancel gave existing-flow grace")
			}
			if err := l.Confirm(m); err == nil {
				t.Fatal("old map restored an ended intent/lease")
			}
		})
	}
}

func TestLeaseRequiresExactIntentAndReceiptClockAndNeverRestoresFromCursor(t *testing.T) {
	f, m, clock, l, cancel := leaseFixture(t, time.Minute)
	defer cancel()
	other, err := NewLease(context.Background(), clock, f.profile.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Confirm(m); !errors.Is(err, ErrMap) {
		t.Fatal("old intent map rebound to new generation", err)
	}
	clock.advance(20*time.Second, 20*time.Second)
	restarted, err := NewLease(context.Background(), clock, f.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Permit(true) || restarted.Permit(false) {
		t.Fatal("new lease used cached cursor as grant")
	}
	if err := l.Confirm(m); err != nil {
		t.Fatal(err)
	}
	clock.advance(40*time.Second, 40*time.Second)
	if l.Permit(true) {
		t.Fatal("delayed application moved signed expiry")
	}
}

func TestVerifierRetainsDurableCursorFloorWhenCallerOmitsOrLowersIt(t *testing.T) {
	f := newMapFixture(t)
	raw := f.sign(t, nil, f.now, f.now.Add(time.Minute))
	f.profile.Connection.MapCursor = &state.MapCursor{Revision: f.value.Revision, PayloadHash: f.value.MapSignature.PayloadHash, IssuedAt: f.now}
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), raw, nil); !errors.Is(err, ErrReplay) {
		t.Fatal("omitted cursor reset durable high-water mark", err)
	}
	older := *f.profile.Connection.MapCursor
	older.Revision.Network--
	if _, err := v.Verify(context.Background(), raw, &older); !errors.Is(err, ErrStale) {
		t.Fatal("caller lowered durable floor", err)
	}
}

func TestLeaseOnlyFreshConfirmationReenablesNewFlows(t *testing.T) {
	f, m, clock, l, cancel := leaseFixture(t, time.Minute)
	defer cancel()
	if err := l.Confirm(m); err != nil {
		t.Fatal(err)
	}
	clock.advance(10*time.Second, 10*time.Second)
	f.now = f.now.Add(10 * time.Second)
	l.Unavailable()
	if l.Permit(false) {
		t.Fatal("unavailable control permitted a new flow")
	}
	end := f.now.Add(time.Minute)
	f.value.Peers[0].EndpointExpiresAt = &end
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	cursor := m.Cursor()
	fresh, err := v.Verify(context.Background(), f.sign(t, &cursor, f.now, end), &cursor)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Confirm(fresh); err != nil || !l.Permit(false) {
		t.Fatal("fresh checked authority did not restore new-flow permission", err)
	}
}

func TestLeaseSamplingUncertaintyCanOnlyShortenDeadline(t *testing.T) {
	f, m, clock, _, cancel := leaseFixture(t, 30*time.Second)
	defer cancel()
	clock.mu.Lock()
	clock.sample.Uptime += time.Millisecond
	clock.sample.UpperUptime = clock.sample.Uptime
	clock.sample.Wall = clock.sample.Wall.Add(time.Millisecond)
	clock.mu.Unlock()
	l, err := NewLease(context.Background(), clock, f.profile.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Confirm(m); err != nil {
		t.Fatal(err)
	}
	l.Unavailable()
	clock.mu.Lock()
	clock.sample.Wall = m.verified.Wall.Add(30*time.Second - time.Millisecond)
	clock.sample.Uptime = m.verified.Uptime + 30*time.Second - time.Millisecond
	clock.sample.UpperUptime = clock.sample.Uptime + time.Millisecond
	clock.mu.Unlock()
	if l.Permit(true) {
		t.Fatal("sampling upper bound extended traffic beyond the conservative deadline")
	}
}
