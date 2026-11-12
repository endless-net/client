//go:build windows

package dataplane

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/endless-net/client/internal/state"
)

type WindowsCleanupConfig struct {
	MaxInventoryRows, MaxInterfaces, MaxProtected int
	TransactionWait, PollInterval                 time.Duration
}

type cleanupStore interface {
	Load(context.Context) (state.State, error)
	Update(context.Context, uint64, func(*state.State) error) error
}

type adapterAbsence interface {
	ConfirmAbsent(context.Context, state.AdapterRecord) error
}

type recoveredGuards interface {
	CloseAfterIPCleanup(context.Context, *WindowsIP) error
	ReleaseForRecovery() error
}

// WindowsCleanup resolves a previous lifetime's journal only. The live owner
// must first revoke its Lease and release engine/TUN/dynamic filter handles.
// This owner never opens/adopts an adapter, restores permits, or reads a map/key
// as authority. A still-present adapter halts cleanup before native deletion.
type WindowsCleanup struct {
	mu      sync.Mutex
	poll    time.Duration
	store   cleanupStore
	ip      *WindowsIP
	adapter adapterAbsence
	guards  func(context.Context, state.ResourceJournal) (recoveredGuards, error)
}

func (*WindowsCleanup) String() string   { return "[Windows resource cleanup]" }
func (*WindowsCleanup) GoString() string { return "[Windows resource cleanup]" }

func NewWindowsCleanup(store *state.Store, config WindowsCleanupConfig) (*WindowsCleanup, error) {
	if config.PollInterval <= 0 || store == nil || config.MaxInventoryRows <= 0 || config.MaxInterfaces <= 0 || config.MaxProtected <= 0 || config.TransactionWait <= 0 || config.TransactionWait%time.Millisecond != 0 || config.TransactionWait/time.Millisecond > time.Duration(1<<32-1) {
		return nil, ErrConfiguration
	}
	ip, err := NewWindowsIP(config.MaxInventoryRows)
	if err != nil {
		return nil, err
	}
	adapter, err := NewWindowsTUNFactory(config.MaxInterfaces)
	if err != nil {
		return nil, err
	}
	return &WindowsCleanup{store: store, ip: ip, adapter: adapter, poll: config.PollInterval, guards: func(ctx context.Context, journal state.ResourceJournal) (recoveredGuards, error) {
		return RecoverWindowsGuardCleanup(ctx, journal, config.TransactionWait, config.MaxProtected)
	}}, nil
}

func copyJournal(j state.ResourceJournal) state.ResourceJournal {
	result := j
	if j.Adapter != nil {
		a := *j.Adapter
		result.Adapter = &a
	}
	result.Addresses = append([]state.AddressRecord(nil), j.Addresses...)
	for i, a := range j.Addresses {
		if a.AutomaticRoute != nil {
			r := *a.AutomaticRoute
			result.Addresses[i].AutomaticRoute = &r
		}
	}
	result.Routes = append([]state.RouteRecord(nil), j.Routes...)
	result.Filters = append([]state.FilterRecord(nil), j.Filters...)
	return result
}

func markJournalRemoving(j *state.ResourceJournal) {
	if j.Adapter != nil {
		j.Adapter.Phase = state.EffectRemoving
	}
	for i := range j.Routes {
		j.Routes[i].Phase = state.EffectRemoving
	}
	for i := range j.Addresses {
		j.Addresses[i].Phase = state.EffectRemoving
		if j.Addresses[i].AutomaticRoute != nil {
			j.Addresses[i].AutomaticRoute.Phase = state.EffectRemoving
		}
	}
	for i := range j.Filters {
		j.Filters[i].Phase = state.EffectRemoving
	}
}

func selectedJournal(value state.State, profileID, sessionID string, generation uint64) (*state.ResourceJournal, error) {
	p, ok := value.Profiles[profileID]
	if !ok || p.Connection == nil {
		return nil, state.ErrStale
	}
	j := p.Connection.Journal
	if j == nil {
		return nil, nil
	}
	if j.ID != sessionID || j.Generation != generation {
		return nil, state.ErrStale
	}
	return j, nil
}

// change fences the entire observed journal plus the store revision. A newer
// intent may coexist with old cleanup, but a changed resource set cannot be
// silently acknowledged using old OS evidence. No mutation or CAS retry occurs.
func (c *WindowsCleanup) change(ctx context.Context, profileID string, expected state.ResourceJournal, fn func(*state.ResourceJournal)) (*state.ResourceJournal, error) {
	value, err := c.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	next := copyJournal(expected)
	fn(&next)
	err = c.store.Update(ctx, value.Revision, func(value *state.State) error {
		current, err := selectedJournal(*value, profileID, expected.ID, expected.Generation)
		if err != nil {
			return err
		}
		if current == nil || !reflect.DeepEqual(*current, expected) {
			return state.ErrStale
		}
		p := value.Profiles[profileID]
		p.Connection.Journal = &next
		value.Profiles[profileID] = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &next, nil
}

func (c *WindowsCleanup) retire(ctx context.Context, profileID string, expected state.ResourceJournal) error {
	value, err := c.store.Load(ctx)
	if err != nil {
		return err
	}
	return c.store.Update(ctx, value.Revision, func(value *state.State) error {
		current, err := selectedJournal(*value, profileID, expected.ID, expected.Generation)
		if err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		if current.Adapter != nil || len(current.Addresses)+len(current.Routes)+len(current.Filters) != 0 || !reflect.DeepEqual(*current, expected) {
			return state.ErrStale
		}
		p := value.Profiles[profileID]
		p.Connection.Journal = nil
		value.Profiles[profileID] = p
		return nil
	})
}

// Run requires the caller's bounded cleanup context, independent of a cancelled
// Connect context. A lost ACK keeps the remaining journal for explicit recovery;
// it never proves rollback or triggers a hidden repeat of native deletions.
func (c *WindowsCleanup) Run(ctx context.Context, profileID, sessionID string, generation uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx == nil || c.store == nil || c.ip == nil || c.adapter == nil || c.guards == nil || generation == 0 {
		return ErrConfiguration
	}
	if _, err := canonicalGUID(sessionID); err != nil {
		return err
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	value, err := c.store.Load(ctx)
	if err != nil {
		return err
	}
	j, err := selectedJournal(value, profileID, sessionID, generation)
	if err != nil || j == nil {
		return err
	}
	if j.Adapter == nil {
		return c.retire(ctx, profileID, *j)
	}
	if err := c.waitAdapterAbsent(ctx, *j.Adapter); err != nil {
		return err
	}
	j, err = c.change(ctx, profileID, *j, markJournalRemoving)
	if err != nil {
		return err
	}
	for len(j.Routes) > 0 {
		r := j.Routes[0]
		p, err := PrepareIPCleanup(j.ID, j.Adapter.LUID, r.Destination)
		if err != nil {
			return err
		}
		if err := c.ip.RemoveRoute(ctx, p, r); err != nil {
			return err
		}
		j, err = c.change(ctx, profileID, *j, func(j *state.ResourceJournal) { j.Routes = j.Routes[1:] })
		if err != nil {
			return err
		}
	}
	for len(j.Addresses) > 0 {
		a := j.Addresses[0]
		p, err := PrepareIPCleanup(j.ID, j.Adapter.LUID, a.Prefix)
		if err != nil {
			return err
		}
		if err := c.ip.RemoveAddress(ctx, p, a); err != nil {
			return err
		}
		j, err = c.change(ctx, profileID, *j, func(j *state.ResourceJournal) { j.Addresses = j.Addresses[1:] })
		if err != nil {
			return err
		}
	}
	// Recheck after row reconciliation: a recycled identity never becomes
	// permission to remove the old persistent guards.
	if err := c.waitAdapterAbsent(ctx, *j.Adapter); err != nil {
		return err
	}
	if j.Adapter.LUID != 0 {
		key, _ := canonicalGUID(j.ID)
		if err := c.ip.confirmFilterCleanup(ctx, FilterPlan{session: key, luid: j.Adapter.LUID}); err != nil {
			return err
		}
	}
	if len(j.Filters) > 0 {
		guards, err := c.guards(ctx, *j)
		if err != nil {
			return err
		}
		if err := guards.CloseAfterIPCleanup(ctx, c.ip); err != nil {
			// Close only this recovery handle/unfinished transaction. Persistent
			// denies survive; this is not an acknowledgment of native cleanup.
			return errors.Join(err, guards.ReleaseForRecovery())
		}
		j, err = c.change(ctx, profileID, *j, func(j *state.ResourceJournal) { j.Filters = nil })
		if err != nil {
			return err
		}
	}
	j, err = c.change(ctx, profileID, *j, func(j *state.ResourceJournal) { j.Adapter = nil })
	if err != nil {
		return err
	}
	return c.retire(ctx, profileID, *j)
}

// Close releases the original creation handle; Windows may finish removing the
// interface asynchronously. Observe only exact-owned presence until absence,
// within the original context. Never delete/adopt an existing adapter here.
func (c *WindowsCleanup) waitAdapterAbsent(ctx context.Context, record state.AdapterRecord) error {
	for {
		err := c.adapter.ConfirmAbsent(ctx, record)
		if !errors.Is(err, ErrAdapterCleanupRequired) {
			return err
		}
		if c.poll <= 0 {
			return ErrConfiguration
		}
		timer := time.NewTimer(c.poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}
