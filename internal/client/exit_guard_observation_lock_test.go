package client

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func TestExitGuardObservationCancellationWhileLockHeld(t *testing.T) {
	for _, observation := range []string{"contained", "absent", "selected"} {
		for _, precancel := range []bool{false, true} {
			t.Run(observation+map[bool]string{false: "/concurrent", true: "/precancel"}[precancel], func(t *testing.T) {
				var calls atomic.Int32
				guard, err := newLinuxExitGuard("exit0", 51820, func(context.Context, string, string, ...string) ([]byte, error) {
					calls.Add(1)
					return nil, errors.New("unexpected native observation")
				})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				guard.mu.Lock()
				defer guard.mu.Unlock()
				if precancel {
					cancel()
				}
				started := make(chan struct{})
				result := make(chan error, 1)
				go func() {
					close(started)
					switch observation {
					case "contained":
						result <- guard.ObserveContained(ctx)
					case "absent":
						result <- guard.ObserveAbsent(ctx)
					case "selected":
						result <- guard.Observe(ctx, api.ExitFamilyIPv4Only)
					}
				}()
				<-started
				if !precancel {
					select {
					case err := <-result:
						t.Fatalf("observation escaped held lock: %v", err)
					case <-time.After(30 * time.Millisecond):
					}
					cancel()
				}
				// Keep the lock held until cancellation completes. An ordinary
				// Mutex.Lock cannot pass this assertion, even with pre-cancel.
				select {
				case err := <-result:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("lost cancellation: %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("cancelled observation remained blocked on guard lock")
				}
				if calls.Load() != 0 {
					t.Fatal("cancelled waiter reached native runner")
				}
			})
		}
	}
}
