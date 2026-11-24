package clientapp

import (
	"context"
	"errors"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
)

// Session maintenance has the lifetime of the desired profile/generation
// between tunnel owners. It never obtains packet authority or starts cleanup.
type sessionMaintenance struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func (m *sessionMaintenance) stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.cancel()
	select {
	case <-m.done:
		return nil
	default:
	}
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sessionMaintenanceAllowed(err error) bool {
	if err == nil {
		return false
	}
	switch connectionOperationError(err) {
	case ErrConnectionOperation, context.Canceled, context.DeadlineExceeded, state.ErrStale,
		connectivity.FailurePermission, connectivity.FailureConflict, connectivity.FailureApply, connectivity.FailurePath, connectivity.FailureCleanup,
		connectivity.FailureControl, connectivity.FailureCredential, connectivity.FailureNodeRenewal:
		return true
	default:
		return false
	}
}

func (w *ConnectionWorker) startSessionMaintenance(ctx context.Context, target connectionTarget, previous *sessionMaintenance) *sessionMaintenance {
	if w.config.RefreshSession == nil || previous != nil && previous.ctx.Err() == nil {
		return previous
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &sessionMaintenance{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(m.done)
		// A successor must still join its predecessor if its own generation is
		// superseded before the predecessor finishes its canceled request.
		if previous != nil {
			<-previous.done
		}
		if ctx.Err() == nil && errors.Is(w.maintainSession(ctx, target), control.ErrCredentialsLogin) {
			select {
			case w.loginNeeded <- m:
			case <-ctx.Done():
			}
		}
	}()
	return m
}

func (w *ConnectionWorker) maintainSession(ctx context.Context, target connectionTarget) error {
	var schedule credentialDeadline
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
		delay, err := w.maintainSessionOnce(ctx, target, &schedule)
		if err != nil {
			if !credentialRetry(err) && !errors.Is(err, control.ErrNodeRenewalPending) {
				return err
			}
			delay = w.config.SessionRetryInterval
		}
		timer.Reset(delay)
	}
}

func (w *ConnectionWorker) maintainSessionOnce(ctx context.Context, target connectionTarget, schedule *credentialDeadline) (time.Duration, error) {
	read, cancel := context.WithTimeout(ctx, w.config.ReadTimeout)
	value, err := w.store.Load(read)
	cancel()
	if err != nil {
		return 0, err
	}
	defer clearConnectionStateKeys(value)
	p := value.Profiles[target.profile]
	if value.ActiveProfileID != target.profile || p.Attempt == nil || p.Attempt.Phase != enrollment.Registered || p.Connection == nil || p.Connection.Generation != target.generation || p.Connection.Intent != state.IntentConnected {
		return 0, context.Canceled
	}
	sample, err := w.config.Clock.Sample()
	if err != nil || sample.Epoch != w.config.RuntimeID {
		return 0, connectivity.ErrClock
	}
	at, due, err := schedule.observeSession(p, sample.Wall)
	if err != nil {
		return 0, err
	}
	if !due {
		return min(w.config.PollInterval, at.Sub(sample.Wall)), nil
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return 0, w.config.RefreshSession(call, target.profile, target.generation)
}
