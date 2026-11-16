//go:build windows

package clientapp

import (
	"context"
	"errors"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/state"
)

var errCredentialHandoff = errors.New("map credential handoff")

// parkMaps cancels and joins the old credential's stream before dispatching a
// rotation. The native owner remains alive and its signed lease is not extended.
func (d *windowsConnection) parkMaps(ctx context.Context) error {
	// Let an already admitted native apply finish before cancelling its source.
	// Queued frames see the cancelled context and are skipped after this barrier.
	d.mapApplyMu.Lock()
	d.mapMu.Lock()
	if d.mapResume == nil {
		d.mapResume, d.mapParked = make(chan struct{}), make(chan struct{})
		if d.mapCancel != nil {
			d.mapCancel(errCredentialHandoff)
		}
	}
	parked := d.mapParked
	d.mapMu.Unlock()
	d.mapApplyMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-parked:
		return nil
	}
}

func (d *windowsConnection) resumeMaps() {
	d.mapMu.Lock()
	defer d.mapMu.Unlock()
	if d.mapResume != nil {
		close(d.mapResume)
		d.mapResume, d.mapParked = nil, nil
	}
}

func (d *windowsConnection) beginMapStream() (context.Context, context.CancelCauseFunc) {
	for {
		d.mapMu.Lock()
		if d.mapResume == nil {
			ctx, cancel := context.WithCancelCause(d.lifetime)
			d.mapCancel = cancel
			d.mapMu.Unlock()
			return ctx, cancel
		}
		resume := d.mapResume
		// Only the reader closes this barrier, once per handoff.
		select {
		case <-d.mapParked:
		default:
			close(d.mapParked)
		}
		d.mapMu.Unlock()
		select {
		case <-d.lifetime.Done():
			return context.WithCancelCause(d.lifetime)
		case <-resume:
		}
	}
}

func (d *windowsConnection) credentialPlan(schedule *credentialSchedule) (credentialPlan, error) {
	ctx, cancel := context.WithTimeout(d.lifetime, d.factory.config.WriteBudget)
	defer cancel()
	value, p, err := d.profileAt(ctx)
	if err != nil {
		return credentialPlan{}, err
	}
	defer clearConnectionKeys(value)
	if p.Connection.Intent != state.IntentConnected {
		return credentialPlan{}, state.ErrStale
	}
	return schedule.plan(p, d.factory.config.Verifier.Now())
}

func credentialRetry(err error) bool {
	return errors.Is(err, control.ErrCredentialsUnavailable) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, state.ErrUncertain) || errors.Is(err, state.ErrStale)
}

// One loop owns all credential calls and frozen replay. It does not block the
// map apply/probe loop while waiting for network or durable result adoption.
func (d *windowsConnection) maintainCredentials(native windowsConnectionNative) {
	if d.factory.config.RefreshCredentials == nil {
		return
	}
	select {
	case <-d.lifetime.Done():
		return
	case <-d.credentialsReady:
	}
	locked := false
	defer func() {
		if locked {
			d.credentialMu.Unlock()
		}
	}()
	var schedule credentialSchedule
	started := false
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-d.lifetime.Done():
			return
		case <-timer.C:
		}
		plan, err := d.credentialPlan(&schedule)
		if err != nil {
			if d.lifetime.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
				timer.Reset(d.factory.config.RetryInterval)
				continue
			}
			if d.lifetime.Err() == nil {
				d.failSource(native, nativeConnectionFailure(err, connectivity.FailureControl))
			}
			return
		}
		if !started {
			close(d.credentialsStarted)
			started = true
		}
		if locked && !plan.node && !plan.session {
			// A lost local acknowledgement may already have adopted the receipt.
			d.resumeMaps()
			d.credentialMu.Unlock()
			locked = false
		}
		delay := min(time.Second, d.factory.config.RetryInterval)
		if plan.session || plan.node {
			ctx, cancel := context.WithTimeout(d.lifetime, 30*time.Second)
			// Session adoption also updates the protected profile revision. Fence
			// endpoint and map-apply CAS writes for either kind of rotation.
			if !locked {
				d.credentialMu.Lock()
				locked = true
			}
			err = d.parkMaps(ctx)
			if err == nil {
				err = d.factory.config.RefreshCredentials(ctx, d.profile, d.generation, plan.session, plan.node)
			}
			cancel()
			if d.lifetime.Err() != nil {
				return
			}
			if err != nil && !credentialRetry(err) {
				d.failSource(native, nativeConnectionFailure(err, connectivity.FailureControl))
				return
			}
			if err == nil {
				// Anchor the newly adopted deadlines before admitting their first
				// map. A delayed later poll must not move the renewal window.
				if _, err = d.credentialPlan(&schedule); err != nil {
					d.failSource(native, nativeConnectionFailure(err, connectivity.FailureControl))
					return
				}
				d.resumeMaps()
				if locked {
					d.credentialMu.Unlock()
					locked = false
				}
			} else {
				// Keep a possibly retired predecessor parked until exact replay is
				// resolved. Native leases still enforce their original deadlines.
				delay = d.factory.config.RetryInterval
			}
		} else {
			delay = min(delay, max(time.Nanosecond, plan.next.Sub(d.factory.config.Verifier.Now())))
		}
		timer.Reset(delay)
	}
}
