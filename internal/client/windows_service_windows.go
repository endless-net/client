//go:build windows

package client

import (
	"context"
	"errors"

	"golang.org/x/sys/windows/svc"
)

func RunWindowsService(name string, run func(context.Context) error) error {
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
	run func(context.Context) error
}

func (s *windowsService) Execute(args []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	changes <- svc.Status{State: svc.StartPending}
	go func() {
		errCh <- s.run(ctx)
	}()
	changes <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
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
