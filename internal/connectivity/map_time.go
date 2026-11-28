package connectivity

import (
	"time"

	signing "github.com/endless-net/signing/api/contract"
)

type mapTimeChallenge struct {
	nonce string
	sent  ClockSample
}

// mapTimeAuthority is local to one verified map and one process. The signed
// reading is a lower bound at receipt. The server may have sampled as early
// as send, so the entire suspend-inclusive exchange advances the upper bound.
type mapTimeAuthority struct {
	serverTime time.Time
	sent       ClockSample
}

func (a mapTimeAuthority) upper(now ClockSample) (time.Time, error) {
	if !now.valid() {
		return time.Time{}, ErrClock
	}
	if a.serverTime.IsZero() {
		return now.Wall, nil
	}
	if now.Epoch != a.sent.Epoch || now.Uptime < a.sent.UpperUptime || now.Wall.Before(a.sent.Wall) {
		return time.Time{}, ErrClock
	}
	return a.serverTime.Add(now.UpperUptime - a.sent.Uptime), nil
}

// WallDeadlineAdvance is the duration to subtract from signed deadlines when
// installing rules evaluated against the native wall clock. A fast local
// clock must never defer expiry. A slow one must include the full proof RTT.
func (m Map) WallDeadlineAdvance(sample ClockSample) (time.Duration, error) {
	if m.snapshot.MapSignature == nil || !m.verified.valid() || sample.Epoch != m.verified.Epoch ||
		sample.Uptime < m.verified.UpperUptime || sample.Wall.Before(m.verified.Wall) {
		return 0, ErrClock
	}
	upper, err := m.timeProof.upper(sample)
	if err != nil {
		return 0, err
	}
	// Even on a later snapshot without an opening proof, elapsed uptime
	// advances the original receipt across suspend or a slowed wall clock.
	fromReceipt := m.verified.Wall.Add(sample.UpperUptime - m.verified.Uptime)
	return max(0, upper.Sub(sample.Wall), fromReceipt.Sub(sample.Wall)), nil
}

func keyCurrentAt(key signing.BootstrapTrustKey, at time.Time) bool {
	return (key.NotBefore == nil || !at.Before(*key.NotBefore)) && (key.NotAfter == nil || at.Before(*key.NotAfter))
}
