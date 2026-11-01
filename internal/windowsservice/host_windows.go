//go:build windows

// Package windowsservice connects Windows SCM to the public Fx lifecycle. It
// does not install/update a service, migrate state, or compose a partial client.
package windowsservice

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"time"

	"go.uber.org/fx"
	"golang.org/x/sys/windows/svc"
)

const ServiceName = "endlessnet-client"

const (
	exitStart uint32 = iota + 1
	exitStop
	exitController
	exitDependency
)

var (
	ErrConfig     = errors.New("invalid Windows service configuration")
	ErrController = errors.New("windows service controller unavailable")
	ErrFailed     = errors.New("windows service lifecycle failed")
)

type Config struct {
	StartTimeout, StopTimeout         time.Duration
	StatusTimeout, CheckpointInterval time.Duration
}

type Host struct {
	app    *fx.App
	config Config
	used   atomic.Bool
	exit   atomic.Uint32
}

var _ svc.Handler = (*Host)(nil)

// New starts nothing. The caller supplies the complete application with explicit
// matching Fx budgets and safe structured logging. Hooks own their resources and
// must honor cancellation; a timeout is a failed stop, not a successful cleanup.
func New(app *fx.App, c Config) (*Host, error) {
	maxPending := time.Duration(math.MaxUint32) * time.Millisecond
	if app == nil || app.Err() != nil || c.StartTimeout < time.Millisecond || c.StartTimeout > maxPending || c.StopTimeout < time.Millisecond || c.StopTimeout > maxPending || c.StartTimeout != app.StartTimeout() || c.StopTimeout != app.StopTimeout() || c.StatusTimeout < time.Millisecond || c.StatusTimeout > c.StartTimeout || c.StatusTimeout > c.StopTimeout || c.CheckpointInterval < time.Millisecond || c.CheckpointInterval > c.StartTimeout || c.CheckpointInterval > c.StopTimeout {
		return nil, ErrConfig
	}
	return &Host{app: app, config: c}, nil
}

// Run is only for an explicit SCM entrypoint. There is no interactive/console
// fallback, service creation, process exit, or disclosure of application errors.
func Run(app *fx.App, c Config) error {
	h, err := New(app, c)
	if err != nil {
		return err
	}
	if err := svc.Run(ServiceName, h); err != nil {
		return ErrController
	}
	if h.exit.Load() != 0 {
		return ErrFailed
	}
	return nil
}

func waitHint(budget time.Duration) uint32 {
	return uint32((budget + time.Millisecond - 1) / time.Millisecond)
}

func (h *Host) report(status chan<- svc.Status, value svc.Status) bool {
	timer := time.NewTimer(h.config.StatusTimeout)
	defer timer.Stop()
	select {
	case status <- value:
		return true
	case <-timer.C:
		return false
	}
}

func result(code uint32) (bool, uint32) { return code != 0, code }

// Fx's hook context cannot bound an arbitrary blocked event logger. Wait for the
// Start API only within a separate cancellation grace budget. An unacknowledged
// startup is a failed process lifecycle; it never claims successful cleanup.
func (h *Host) abortStart(status chan<- svc.Status, cancel context.CancelFunc, done <-chan error, code uint32) uint32 {
	cancel()
	_ = h.report(status, svc.Status{State: svc.StopPending, CheckPoint: 1, WaitHint: waitHint(h.config.StopTimeout)})
	timer := time.NewTimer(h.config.StopTimeout)
	defer timer.Stop()
	select {
	case err := <-done:
		if err == nil {
			return h.stop(status, code)
		}
		return code
	case <-timer.C:
		return code
	}
}

func (h *Host) stop(status chan<- svc.Status, code uint32) uint32 {
	current := svc.Status{State: svc.StopPending, CheckPoint: 1, WaitHint: waitHint(h.config.StopTimeout)}
	reporting := h.report(status, current)
	if !reporting && code == 0 {
		code = exitController
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.config.StopTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.app.Stop(ctx) }()
	tick := time.NewTicker(h.config.CheckpointInterval)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			if (err != nil || ctx.Err() != nil) && code == 0 {
				return exitStop
			}
			return code
		case <-ctx.Done():
			if code == 0 {
				return exitStop
			}
			return code
		case <-tick.C:
			if reporting {
				current.CheckPoint++
				reporting = h.report(status, current)
				if !reporting && code == 0 {
					code = exitController
				}
			}
		}
	}
}

// Execute reports Running only after mandatory Fx OnStart hooks completed.
// Fx owns startup rollback. Returning a nonzero code on startup cancellation or
// timeout prevents an incomplete startup/cleanup from being reported as clean.
// Successful SCM Stop/Shutdown invokes bounded Fx Stop; unexpected application
// shutdown is a service failure, including a worker that omitted Fx.ExitCode.
func (h *Host) Execute(_ []string, commands <-chan svc.ChangeRequest, status chan<- svc.Status) (specific bool, code uint32) {
	if !h.used.CompareAndSwap(false, true) {
		return true, exitController
	}
	defer func() { h.exit.Store(code) }()
	current := svc.Status{State: svc.StartPending, CheckPoint: 1, WaitHint: waitHint(h.config.StartTimeout)}
	if !h.report(status, current) {
		return true, exitController
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.config.StartTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.app.Start(ctx) }()
	tick := time.NewTicker(h.config.CheckpointInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return result(h.abortStart(status, cancel, done, exitStart))
		case err := <-done:
			if err != nil {
				// Start performs rollback; a failed start is never followed by
				// concurrent Stop while its hooks may still be unwinding.
				_ = h.report(status, svc.Status{State: svc.StopPending, CheckPoint: 1, WaitHint: waitHint(h.config.StopTimeout)})
				return true, exitStart
			}
			if ctx.Err() != nil {
				return result(h.stop(status, exitStart))
			}
			// Fx retains a broadcast made during startup. Subscribe only now:
			// failed Start must not leave an OS signal receiver running.
			shutdown := h.app.Wait()
			select {
			case <-shutdown:
				return result(h.stop(status, exitDependency))
			default:
			}
			current = svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
			if !h.report(status, current) {
				return result(h.stop(status, exitController))
			}
			return result(h.running(commands, status, shutdown, current))
		case command, open := <-commands:
			if !open || command.Cmd == svc.Stop || command.Cmd == svc.Shutdown {
				return result(h.abortStart(status, cancel, done, exitStart))
			}
			if command.Cmd == svc.Interrogate && !h.report(status, current) {
				return result(h.abortStart(status, cancel, done, exitController))
			}
		case <-tick.C:
			current.CheckPoint++
			if !h.report(status, current) {
				return result(h.abortStart(status, cancel, done, exitController))
			}
		}
	}
}

func (h *Host) running(commands <-chan svc.ChangeRequest, status chan<- svc.Status, shutdown <-chan fx.ShutdownSignal, current svc.Status) uint32 {
	for {
		select {
		case <-shutdown:
			return h.stop(status, exitDependency)
		case command, open := <-commands:
			if !open {
				return h.stop(status, exitController)
			}
			switch command.Cmd {
			case svc.Stop, svc.Shutdown:
				return h.stop(status, 0)
			case svc.Interrogate:
				if !h.report(status, current) {
					return h.stop(status, exitController)
				}
			}
		}
	}
}
