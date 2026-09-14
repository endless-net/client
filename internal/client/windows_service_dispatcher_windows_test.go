//go:build windows

package client

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

func TestWindowsSCMCopiesSessionBeforeCallbackReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	controls := make(chan windowsServiceControl, 1)
	data := windows.WTSSESSION_NOTIFICATION{SessionID: 7}
	data.Size = uint32(unsafe.Sizeof(data))
	if code := enqueueWindowsServiceControl(ctx, cancel, controls, uint32(svc.SessionChange), windows.WTS_SESSION_LOGOFF, &data); code != 0 {
		t.Fatal(code)
	}
	data.SessionID, data.Size = 99, 0
	control := <-controls
	if !control.SessionValid || control.SessionID != 7 || control.EventType != windows.WTS_SESSION_LOGOFF {
		t.Fatal("queued session retained callback data")
	}
	if _, err := copyWindowsServiceControl(uint32(svc.SessionChange), windows.WTS_SESSION_LOGOFF, &data); err == nil {
		t.Fatal("invalid notification size accepted")
	}
	if _, err := copyWindowsServiceControl(uint32(svc.SessionChange), windows.WTS_SESSION_LOGOFF, nil); err == nil {
		t.Fatal("null notification accepted")
	}
	if code := enqueueWindowsServiceControl(ctx, cancel, controls, uint32(svc.PowerEvent), 0x4, nil); code != 0 {
		t.Fatal(code)
	}
	if code := enqueueWindowsServiceControl(ctx, cancel, controls, uint32(svc.PowerEvent), 0x12, nil); code != uintptr(windows.ERROR_NOT_ENOUGH_QUOTA) || ctx.Err() == nil {
		t.Fatal("overflow failed to cancel runtime")
	}
}

func TestWindowsSCMStatusFailureCancelsAndJoinsRuntime(t *testing.T) {
	for _, failState := range []svc.State{0, svc.StartPending, svc.Running} {
		t.Run(fmt.Sprintf("state_%d", failState), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			controls := make(chan windowsServiceControl, 1)
			joined := make(chan struct{})
			service := &windowsService{run: func(ctx context.Context, _ <-chan RuntimeLifecycleEvent) error {
				<-ctx.Done()
				close(joined)
				return nil
			}}
			var states []svc.State
			reportErr := errors.New("synthetic status failure")
			err := pumpWindowsService(ctx, service, controls, func(status svc.Status) error {
				states = append(states, status.State)
				if status.State == svc.Stopped {
					select {
					case <-joined:
					default:
						t.Fatal("STOPPED preceded runtime cleanup")
					}
					if status.Accepts != 0 || (status.Win32ExitCode != 0) != (failState != 0) {
						t.Fatal("invalid terminal status")
					}
				}
				if status.State == failState {
					return reportErr
				}
				if failState == 0 && status.State == svc.Running {
					controls <- windowsServiceControl{Cmd: svc.Stop}
				}
				return nil
			})
			if (err != nil) != (failState != 0) || (failState != 0 && !errors.Is(err, reportErr)) {
				t.Fatal("status failure not returned", err)
			}
			stopped := 0
			for _, state := range states {
				if state == svc.Stopped {
					stopped++
				}
			}
			if stopped != 1 || states[len(states)-1] != svc.Stopped {
				t.Fatal("terminal status not exactly once and last")
			}
		})
	}
}
