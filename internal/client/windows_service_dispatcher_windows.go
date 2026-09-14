//go:build windows

package client

import (
	"context"
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

// No callback-owned pointer may cross into the asynchronous service loop.
type windowsServiceControl struct {
	Cmd          svc.Cmd
	EventType    uint32
	SessionID    uint32
	SessionValid bool
}

func copyWindowsServiceControl(command, eventType uint32, data *windows.WTSSESSION_NOTIFICATION) (windowsServiceControl, error) {
	control := windowsServiceControl{Cmd: svc.Cmd(command), EventType: eventType}
	if control.Cmd != svc.SessionChange {
		return control, nil
	}
	if data == nil || data.Size != uint32(unsafe.Sizeof(*data)) || data.SessionID == 0 || data.SessionID == ^uint32(0) {
		return windowsServiceControl{}, errors.New("windows session notification invalid")
	}
	control.SessionID, control.SessionValid = data.SessionID, true
	return control, nil
}

func enqueueWindowsServiceControl(ctx context.Context, cancel context.CancelFunc, controls chan<- windowsServiceControl, command, eventType uint32, data *windows.WTSSESSION_NOTIFICATION) uintptr {
	if ctx.Err() != nil {
		return uintptr(windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL)
	}
	control, err := copyWindowsServiceControl(command, eventType, data)
	if err != nil {
		cancel()
		return uintptr(windows.ERROR_INVALID_DATA)
	}
	select {
	case <-ctx.Done():
		return uintptr(windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL)
	case controls <- control:
		return 0
	default:
		cancel()
		return uintptr(windows.ERROR_NOT_ENOUGH_QUOTA)
	}
}

func runWindowsSCM(name string, service *windowsService) error {
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	result := make(chan error, 1)
	mainCallback := windows.NewCallback(func(_ uint32, _ **uint16) uintptr {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		controls := make(chan windowsServiceControl, 64)
		controlCallback := windows.NewCallback(func(command, eventType uint32, data *windows.WTSSESSION_NOTIFICATION, _ uintptr) uintptr {
			return enqueueWindowsServiceControl(ctx, cancel, controls, command, eventType, data)
		})
		handle, err := windows.RegisterServiceCtrlHandlerEx(namePointer, controlCallback, 0)
		if err != nil {
			result <- err
			return 0
		}
		result <- pumpWindowsService(ctx, service, controls, func(status svc.Status) error {
			native := windows.SERVICE_STATUS{
				ServiceType: windows.SERVICE_WIN32_OWN_PROCESS, CurrentState: uint32(status.State),
				ControlsAccepted: uint32(status.Accepts), CheckPoint: status.CheckPoint, WaitHint: status.WaitHint,
				Win32ExitCode: status.Win32ExitCode, ServiceSpecificExitCode: status.ServiceSpecificExitCode,
			}
			return windows.SetServiceStatus(handle, &native)
		})
		return 0
	})
	table := []windows.SERVICE_TABLE_ENTRY{{ServiceName: namePointer, ServiceProc: mainCallback}, {}}
	err = windows.StartServiceCtrlDispatcher(&table[0])
	runtime.KeepAlive(table)
	runtime.KeepAlive(namePointer)
	if err != nil {
		return err
	}
	select {
	case err := <-result:
		return err
	default:
		return errors.New("windows service main was not invoked")
	}
}

// Keep consuming status after a reporting failure so cancellation can join the
// runtime. Only report STOPPED after all runtime cleanup, and exactly once.
func pumpWindowsService(parent context.Context, service *windowsService, controls <-chan windowsServiceControl, report func(svc.Status) error) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	changes := make(chan svc.Status)
	type exitStatus struct {
		specific bool
		code     uint32
	}
	done := make(chan exitStatus, 1)
	go func() { specific, code := service.execute(ctx, controls, changes); done <- exitStatus{specific, code} }()
	var failure error
	for {
		select {
		case status := <-changes:
			if failure == nil {
				if err := report(status); err != nil {
					failure = err
					cancel()
				}
			}
		case exit := <-done:
			status := svc.Status{State: svc.Stopped, Win32ExitCode: exit.code}
			if exit.specific {
				status.Win32ExitCode = uint32(windows.ERROR_SERVICE_SPECIFIC_ERROR)
				status.ServiceSpecificExitCode = exit.code
			}
			if failure != nil && status.Win32ExitCode == 0 {
				status.Win32ExitCode = 1
			}
			if exit.code != 0 {
				failure = errors.Join(failure, errors.New("windows service runtime failed"))
			}
			return errors.Join(failure, report(status))
		}
	}
}
