//go:build windows

package client

import (
	"context"
	"errors"

	"golang.org/x/sys/windows/svc"
)

func RunWindowsService(name string, run func(context.Context, <-chan RuntimeLifecycleEvent) error) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return errors.New("windows service mode requires the Windows Service Control Manager")
	}
	return svc.Run(name, &windowsService{run: run})
}

type windowsService struct {
	run func(context.Context, <-chan RuntimeLifecycleEvent) error
}

func (s *windowsService) Execute(args []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPowerEvent
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	events := make(chan RuntimeLifecycleEvent, 64)
	changes <- svc.Status{State: svc.StartPending}
	go func() {
		errCh <- s.run(ctx, events)
	}()
	changes <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case request, ok := <-requests:
			if !ok {
				cancel()
				<-errCh
				return false, 1
			}
			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.PowerEvent:
				var event RuntimeLifecycleEvent
				switch request.EventType {
				case 0x4: // PBT_APMSUSPEND
					event = RuntimeSuspend
				case 0x12: // PBT_APMRESUMEAUTOMATIC, including unattended wake
					event = RuntimeResume
				default:
					continue
				}
				// Only copied scalar data crosses this boundary. SessionChange's
				// forwarded EventData pointer is not safe to dereference here.
				select {
				case events <- event:
				default:
					// Losing a suspend would allow ordinary apply to continue.
					changes <- svc.Status{State: svc.StopPending}
					cancel()
					<-errCh
					return false, 1
				}
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-errCh; err != nil {
					return false, 1
				}
				return false, 0
			default:
			}
		case err := <-errCh:
			if err != nil {
				return false, 1
			}
			return false, 0
		}
	}
}
