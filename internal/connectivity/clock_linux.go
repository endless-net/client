package connectivity

import (
	"math"
	"time"

	"golang.org/x/sys/unix"
)

type LinuxClock struct{ epoch string }

func NewLinuxClock(epoch string) (*LinuxClock, error) {
	if !identifier(epoch) {
		return nil, ErrConfig
	}
	clock := &LinuxClock{epoch: epoch}
	if _, err := clock.Sample(); err != nil {
		return nil, err
	}
	return clock, nil
}

func (c *LinuxClock) Sample() (ClockSample, error) {
	var before, after unix.Timespec
	if c == nil || unix.ClockGettime(unix.CLOCK_BOOTTIME, &before) != nil {
		return ClockSample{}, ErrClock
	}
	wall := time.Now().UTC()
	if unix.ClockGettime(unix.CLOCK_BOOTTIME, &after) != nil {
		return ClockSample{}, ErrClock
	}
	for _, sample := range []unix.Timespec{before, after} {
		if sample.Sec < 0 || sample.Nsec < 0 || sample.Nsec >= int64(time.Second) ||
			sample.Sec > (math.MaxInt64-sample.Nsec)/int64(time.Second) {
			return ClockSample{}, ErrClock
		}
	}
	result := ClockSample{Wall: wall, Uptime: time.Duration(before.Nano()), UpperUptime: time.Duration(after.Nano()), Epoch: c.epoch}
	if !result.valid() {
		return ClockSample{}, ErrClock
	}
	return result, nil
}
