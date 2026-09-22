//go:build windows

package client

import (
	"context"
	"errors"
	"log"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

func RunWindowsService(name string, run func(context.Context, <-chan RuntimeLifecycleNotification) error) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return errors.New("windows service mode requires the Windows Service Control Manager")
	}
	return runWindowsSCM(name, &windowsService{run: run, enumerate: EnumerateWindowsUserSessions, queryOwner: QueryWindowsSessionOwner})
}

type windowsService struct {
	run        func(context.Context, <-chan RuntimeLifecycleNotification) error
	enumerate  func() ([]uint32, error)
	queryOwner func(uint32) (string, error)
}

func (s *windowsService) execute(parent context.Context, requests <-chan windowsServiceControl, changes chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPowerEvent | svc.AcceptSessionChange
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	errCh := make(chan error, 1)
	events := make(chan RuntimeLifecycleNotification, 64)
	changes <- svc.Status{State: svc.StartPending}
	if s.enumerate == nil || s.queryOwner == nil {
		return false, 1
	}
	sessions, err := s.enumerate()
	if err != nil {
		return false, 1
	}
	owners := newWindowsSessionOwners(s.queryOwner)
	for _, session := range sessions {
		if err := owners.seed(session); err != nil {
			log.Print("windows initial session owner unavailable")
		}
	}
	owners.finishSeeding()
	if ctx.Err() != nil {
		return false, 1
	}
	go func() {
		errCh <- s.run(ctx, events)
	}()
	changes <- svc.Status{State: svc.Running, Accepts: accepts}
	lookupRetry := time.NewTicker(time.Second)
	defer lookupRetry.Stop()

	for {
		select {
		case <-lookupRetry.C:
			if ctx.Err() == nil {
				_, _ = owners.retryUnresolved()
			}
		case <-ctx.Done():
			<-errCh
			return false, 1
		case request, ok := <-requests:
			if !ok {
				cancel()
				<-errCh
				return false, 1
			}
			switch request.Cmd {
			case svc.Interrogate:
				changes <- svc.Status{State: svc.Running, Accepts: accepts}
			case svc.PowerEvent, svc.SessionChange:
				var event RuntimeLifecycleNotification
				if request.Cmd == svc.SessionChange {
					if !request.SessionValid {
						continue
					}
					switch request.EventType {
					case windows.WTS_SESSION_LOGON:
						if err := owners.logon(request.SessionID); err != nil {
							log.Print("windows session owner lookup unavailable")
						}
						continue
					case windows.WTS_SESSION_LOGOFF:
						owner, err := owners.logoff(request.SessionID)
						if err != nil {
							log.Print("windows logoff owner unavailable")
							continue
						}
						event = RuntimeLifecycleNotification{Event: RuntimeUserLogoff, SessionOwner: owner}
					default:
						continue
					}
				} else {
					switch request.EventType {
					case 0x4: // PBT_APMSUSPEND
						event.Event = RuntimeSuspend
						event.Completion = request.Completion
						event.Deadline = request.Deadline
					case 0x12: // PBT_APMRESUMEAUTOMATIC, including unattended wake
						event.Event = RuntimeResume
					default:
						continue
					}
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
