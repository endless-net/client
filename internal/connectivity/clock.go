package connectivity

import "time"

// ClockSample brackets the wall-clock read with suspend-inclusive uptime.
// Deadlines use the receipt's lower and the current sample's upper bound, so
// sampling latency can only shorten access. Epoch identifies this daemon run;
// samples and verified maps are never restored as grants across restart.
type ClockSample struct {
	Wall                time.Time
	Uptime, UpperUptime time.Duration
	Epoch               string
}

type Clock interface{ Sample() (ClockSample, error) }

func (s ClockSample) valid() bool {
	return !s.Wall.IsZero() && s.Uptime >= 0 && s.UpperUptime >= s.Uptime && identifier(s.Epoch)
}
