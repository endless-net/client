//go:build linux

package client

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestExitLANClockSamplingOrderAndFailure(t *testing.T) {
	for _, scenario := range []string{"current", "cancel", "late_cancel", "read_error_1", "read_error_2", "read_error_3", "negative", "nanos", "overflow", "wall_overflow", "regression"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			read := func(id int32, out *unix.Timespec) error {
				want := []int32{unix.CLOCK_BOOTTIME, unix.CLOCK_REALTIME, unix.CLOCK_BOOTTIME}
				if calls >= len(want) || id != want[calls] {
					t.Fatal("clock sampling order changed")
				}
				calls++
				out.Sec = 100
				out.Nsec = 10
				if calls == 2 {
					out.Sec = 1_790_000_000
				}
				if calls == 3 {
					out.Nsec = 20
				}
				if (scenario == "read_error_1" && calls == 1) || (scenario == "read_error_2" && calls == 2) || (scenario == "read_error_3" && calls == 3) {
					return errors.New("clock error")
				}
				switch scenario {
				case "cancel":
					cancel()
				case "late_cancel":
					if calls == 3 {
						cancel()
					}
				case "negative":
					out.Sec = -1
				case "nanos":
					out.Nsec = 1e9
				case "overflow":
					out.Sec = 1 << 62
				case "wall_overflow":
					if calls == 2 {
						out.Sec = 1 << 62
					}
				case "regression":
					if calls == 3 {
						out.Sec = 99
					}
				}
				return nil
			}
			sample, err := captureExitLANClockWithRead(ctx, read)
			if scenario == "current" {
				if err != nil || calls != 3 || sample.bootBefore != 100e9+10 || sample.bootAfter != 100e9+20 {
					t.Fatal("incorrect clock sample", sample, err)
				}
			} else if err == nil {
				t.Fatal("invalid clock accepted")
			}
			if (scenario == "cancel" || scenario == "late_cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}
