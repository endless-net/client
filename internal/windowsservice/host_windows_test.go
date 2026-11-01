//go:build windows

package windowsservice

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"golang.org/x/sys/windows/svc"
)

type outcome struct {
	specific bool
	code     uint32
}
type harness struct {
	commands chan svc.ChangeRequest
	status   chan svc.Status
	done     chan outcome
}

type blockedStartLogger struct{ entered, release chan struct{} }

func (l *blockedStartLogger) LogEvent(event fxevent.Event) {
	if _, ok := event.(*fxevent.Started); ok {
		close(l.entered)
		<-l.release
	}
}

func TestSCMStartupAPIItselfCannotExceedCancellationGrace(t *testing.T) {
	logger := &blockedStartLogger{entered: make(chan struct{}), release: make(chan struct{})}
	app := application(config(), fx.WithLogger(func() fxevent.Logger { return logger }))
	_, f := execute(t, app)
	select {
	case <-logger.entered:
	case <-time.After(time.Second):
		t.Fatal("Fx startup did not reach logger")
	}
	// Fx has already completed its hooks, but its Start API is stuck outside
	// the hook deadline. The host must fail without advertising Running.
	waitExit(t, f, exitStart)
	noRunning(t, f)
	close(logger.release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := app.Stop(ctx); err != nil {
		t.Fatal("fixture cleanup failed")
	}
}

func config() Config {
	return Config{StartTimeout: 300 * time.Millisecond, StopTimeout: 300 * time.Millisecond, StatusTimeout: 30 * time.Millisecond, CheckpointInterval: 10 * time.Millisecond}
}

func application(c Config, opts ...fx.Option) *fx.App {
	return fx.New(append([]fx.Option{fx.NopLogger, fx.StartTimeout(c.StartTimeout), fx.StopTimeout(c.StopTimeout)}, opts...)...)
}

func execute(t *testing.T, app *fx.App) (*Host, harness) {
	t.Helper()
	h, err := New(app, config())
	if err != nil {
		t.Fatal(err)
	}
	f := harness{commands: make(chan svc.ChangeRequest, 8), status: make(chan svc.Status, 64), done: make(chan outcome, 1)}
	go func() { specific, code := h.Execute(nil, f.commands, f.status); f.done <- outcome{specific, code} }()
	return h, f
}

func waitState(t *testing.T, f harness, state svc.State) svc.Status {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case s := <-f.status:
			if s.State == state {
				return s
			}
		case got := <-f.done:
			t.Fatalf("service exited before status: %d", got.code)
		case <-deadline.C:
			t.Fatal("service status deadline")
		}
	}
}

func waitExit(t *testing.T, f harness, expected uint32) {
	t.Helper()
	select {
	case got := <-f.done:
		if got.code != expected || got.specific != (expected != 0) {
			t.Fatalf("unexpected service exit: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("service exit exceeded budget")
	}
}

func noRunning(t *testing.T, f harness) {
	t.Helper()
	for {
		select {
		case s := <-f.status:
			if s.State == svc.Running {
				t.Fatal("failed startup reported Running")
			}
		default:
			return
		}
	}
}

func TestSCMRunningAfterFxStartAndBoundedStop(t *testing.T) {
	entered, release, stopping, finish := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var cleanups atomic.Int32
	app := application(config(), fx.Invoke(func(lc fx.Lifecycle) {
		lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, OnStop: func(ctx context.Context) error {
			close(stopping)
			select {
			case <-finish:
				cleanups.Add(1)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}})
	}))
	_, f := execute(t, app)
	first := waitState(t, f, svc.StartPending)
	if first.Accepts != 0 || first.CheckPoint != 1 || first.WaitHint != 300 {
		t.Fatal("invalid startup status")
	}
	<-entered
	f.commands <- svc.ChangeRequest{Cmd: svc.Interrogate}
	if next := waitState(t, f, svc.StartPending); next.Accepts != 0 {
		t.Fatal("start prematurely accepted controls")
	}
	noRunning(t, f)
	close(release)
	running := waitState(t, f, svc.Running)
	if running.Accepts != svc.AcceptStop|svc.AcceptShutdown {
		t.Fatal("wrong accepted controls")
	}
	f.commands <- svc.ChangeRequest{Cmd: svc.Pause}
	f.commands <- svc.ChangeRequest{Cmd: svc.Interrogate}
	if s := waitState(t, f, svc.Running); s.CheckPoint != 0 {
		t.Fatal("unsupported control changed service mode")
	}
	f.commands <- svc.ChangeRequest{Cmd: svc.Stop}
	if s := waitState(t, f, svc.StopPending); s.Accepts != 0 || s.WaitHint != 300 {
		t.Fatal("invalid drain status")
	}
	<-stopping
	select {
	case <-f.done:
		t.Fatal("stop returned before Fx cleanup")
	default:
	}
	close(finish)
	waitExit(t, f, 0)
	if cleanups.Load() != 1 {
		t.Fatal("cleanup did not run exactly once")
	}
}

func TestSCMStartFailureRollsBackWithoutRunning(t *testing.T) {
	var cleaned atomic.Int32
	app := application(config(), fx.Invoke(func(lc fx.Lifecycle) {
		lc.Append(fx.Hook{OnStart: func(context.Context) error { return nil }, OnStop: func(context.Context) error { cleaned.Add(1); return nil }})
		lc.Append(fx.Hook{OnStart: func(context.Context) error { return errors.New("synthetic upstream secret") }})
	}))
	_, f := execute(t, app)
	waitExit(t, f, exitStart)
	noRunning(t, f)
	if cleaned.Load() != 1 {
		t.Fatal("completed startup hook was not rolled back")
	}
}

func TestSCMStartTimeoutAndCancellationFailClosed(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "SCM-stop-during-start"}[cancelEarly], func(t *testing.T) {
			entered, finished := make(chan struct{}), make(chan struct{})
			app := application(config(), fx.Invoke(func(lc fx.Lifecycle) {
				lc.Append(fx.Hook{OnStart: func(ctx context.Context) error { close(entered); <-ctx.Done(); close(finished); return ctx.Err() }})
			}))
			_, f := execute(t, app)
			<-entered
			if cancelEarly {
				f.commands <- svc.ChangeRequest{Cmd: svc.Stop}
			}
			waitExit(t, f, exitStart)
			noRunning(t, f)
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("startup hook ignored cancellation")
			}
		})
	}
}

func TestSCMStopFailureAndTimeoutNeverReportCleanExit(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "cleanup-failure", true: "cleanup-deadline"}[timeout], func(t *testing.T) {
			finished := make(chan struct{})
			app := application(config(), fx.Invoke(func(lc fx.Lifecycle) {
				lc.Append(fx.Hook{OnStop: func(ctx context.Context) error {
					defer close(finished)
					if timeout {
						<-ctx.Done()
						return ctx.Err()
					}
					return errors.New("synthetic cleanup secret")
				}})
			}))
			_, f := execute(t, app)
			waitState(t, f, svc.Running)
			f.commands <- svc.ChangeRequest{Cmd: svc.Shutdown}
			waitExit(t, f, exitStop)
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("cleanup did not honor deadline")
			}
		})
	}
}

func TestSCMUnexpectedWorkerShutdownBeforeAndAfterReady(t *testing.T) {
	for _, duringStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "after-ready", true: "during-start"}[duringStart], func(t *testing.T) {
			var shutdown fx.Shutdowner
			var cleaned atomic.Int32
			app := application(config(), fx.Populate(&shutdown), fx.Invoke(func(lc fx.Lifecycle, s fx.Shutdowner) {
				lc.Append(fx.Hook{OnStart: func(context.Context) error {
					if duringStart {
						return s.Shutdown()
					}
					return nil
				}, OnStop: func(context.Context) error { cleaned.Add(1); return nil }})
			}))
			_, f := execute(t, app)
			if !duringStart {
				waitState(t, f, svc.Running)
				if err := shutdown.Shutdown(); err != nil {
					t.Fatal("worker shutdown failed")
				}
			}
			waitExit(t, f, exitDependency)
			if duringStart {
				noRunning(t, f)
			}
			if cleaned.Load() != 1 {
				t.Fatal("worker failure left required resource active")
			}
		})
	}
}

func TestSCMClosedControllerAndDuplicateExecute(t *testing.T) {
	var cleaned atomic.Int32
	app := application(config(), fx.Invoke(func(lc fx.Lifecycle) {
		lc.Append(fx.Hook{OnStop: func(context.Context) error { cleaned.Add(1); return nil }})
	}))
	h, f := execute(t, app)
	waitState(t, f, svc.Running)
	if specific, code := h.Execute(nil, nil, nil); !specific || code != exitController {
		t.Fatal("duplicate service lifecycle accepted")
	}
	close(f.commands)
	waitExit(t, f, exitController)
	if cleaned.Load() != 1 {
		t.Fatal("closed controller skipped cleanup")
	}
}

func TestSCMUnresponsiveStatusIsBoundedBeforeStartup(t *testing.T) {
	var starts atomic.Int32
	app := application(config(), fx.Invoke(func(lc fx.Lifecycle) {
		lc.Append(fx.Hook{OnStart: func(context.Context) error { starts.Add(1); return nil }})
	}))
	h, err := New(app, config())
	if err != nil {
		t.Fatal(err)
	}
	f := harness{done: make(chan outcome, 1)}
	go func() { specific, code := h.Execute(nil, nil, nil); f.done <- outcome{specific, code} }()
	waitExit(t, f, exitController)
	if starts.Load() != 0 {
		t.Fatal("unreportable service started resources")
	}
}

func TestSCMUnresponsiveControllerAfterReadyStillCleansUp(t *testing.T) {
	var cleaned atomic.Int32
	app := application(config(), fx.Invoke(func(lc fx.Lifecycle) {
		lc.Append(fx.Hook{OnStop: func(context.Context) error { cleaned.Add(1); return nil }})
	}))
	h, err := New(app, config())
	if err != nil {
		t.Fatal(err)
	}
	f := harness{commands: make(chan svc.ChangeRequest, 8), status: make(chan svc.Status, 1), done: make(chan outcome, 1)}
	go func() { specific, code := h.Execute(nil, f.commands, f.status); f.done <- outcome{specific, code} }()
	waitState(t, f, svc.Running)
	// Fill the status buffer with one interrogation, then stop consuming it.
	f.commands <- svc.ChangeRequest{Cmd: svc.Interrogate}
	f.commands <- svc.ChangeRequest{Cmd: svc.Interrogate}
	waitExit(t, f, exitController)
	if cleaned.Load() != 1 {
		t.Fatal("unresponsive controller left resources running")
	}
}

func TestSCMConfigRequiresExplicitMatchingFxBudgets(t *testing.T) {
	app := application(config())
	for _, mutate := range []func(*Config){
		func(c *Config) { c.StartTimeout = 0 }, func(c *Config) { c.StartTimeout += time.Second },
		func(c *Config) { c.StopTimeout = 0 }, func(c *Config) { c.StatusTimeout = 0 },
		func(c *Config) { c.CheckpointInterval = time.Nanosecond },
	} {
		c := config()
		mutate(&c)
		if _, err := New(app, c); !errors.Is(err, ErrConfig) {
			t.Fatal("invalid service budget accepted")
		}
	}
	if _, err := New(nil, config()); !errors.Is(err, ErrConfig) {
		t.Fatal("nil app accepted")
	}
	if _, err := New(application(config(), fx.Error(errors.New("synthetic construction secret"))), config()); !errors.Is(err, ErrConfig) {
		t.Fatal("failed app accepted")
	}
}
