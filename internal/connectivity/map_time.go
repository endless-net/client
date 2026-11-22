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

func keyCurrentAt(key signing.BootstrapTrustKey, at time.Time) bool {
	return (key.NotBefore == nil || !at.Before(*key.NotBefore)) && (key.NotAfter == nil || at.Before(*key.NotAfter))
}
