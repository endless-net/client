//go:build windows

package dataplane

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	"github.com/google/uuid"
	"github.com/tailscale/wireguard-go/conn"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

type WindowsSessionConfig struct {
	MaxPeers, MaxGrants, MaxProtected, MaxInventoryRows, MaxInterfaces int
	MTU                                                                int
	ListenPort                                                         uint16
	RouteMetric                                                        uint32
	DADPollInterval, TransactionWait, ApplyTimeout                     time.Duration
}

func (c WindowsSessionConfig) Validate() error {
	if c.MaxPeers <= 0 || c.MaxGrants <= 0 || c.MaxProtected <= 0 || c.MaxInventoryRows <= 0 || c.MaxInterfaces <= 0 || c.MTU < 576 || c.MTU > 65535 || c.ListenPort == 0 || c.DADPollInterval <= 0 || c.ApplyTimeout <= 0 || c.ApplyTimeout > 30*time.Second || c.TransactionWait <= 0 || c.TransactionWait%time.Millisecond != 0 || c.TransactionWait/time.Millisecond > time.Duration(1<<32-1) {
		return ErrConfiguration
	}
	return nil
}

// WindowsSession is one intent's resource owner. A successful Start means local
// application and an acknowledged listener, never a peer handshake/Connected UI.
// Refresh/offline delivery and the daemon worker are separate lifecycle steps.
type WindowsSession struct {
	mu               sync.Mutex
	store            *state.Store
	profileID, id    string
	generation       uint64
	config           WindowsSessionConfig
	lease            *connectivity.Lease
	lifetime         context.Context
	created          connectivity.ClockSample
	clock            connectivity.Clock
	operation        *connectivity.OperationBudget
	cancel           context.CancelFunc
	ip               *WindowsIP
	cleanup          *WindowsCleanup
	tunnel           *WindowsTUN
	filters          *WindowsFilters
	engine           *Engine
	started, stopped bool
	current          connectivity.Map
	policy           FilterPlan
	createTUN        func(context.Context, state.AdapterRecord, int, AdapterJournalWriter) (*WindowsTUN, error)
	makeFilters      func(string, time.Duration, GuardJournalWriter) (*WindowsFilters, error)
	planFilters      func(context.Context, connectivity.Map, string, uint64, uint32, int, int, bool) (FilterPlan, error)
	identity         func(uint64) (adapterIdentity, error)
	setMTU           func(context.Context, state.AdapterRecord, int) error
	makeBind         func() conn.Bind
}

func (*WindowsSession) String() string   { return "[Windows connectivity session]" }
func (*WindowsSession) GoString() string { return "[Windows connectivity session]" }

// NewWindowsSession binds authority to the daemon worker's lifetime, not an IPC
// request or UI window. Start receives its own finite operation context.
func NewWindowsSession(parent context.Context, store *state.Store, clock connectivity.Clock, profileID string, generation uint64, config WindowsSessionConfig) (*WindowsSession, error) {
	if parent == nil || store == nil || clock == nil || generation == 0 || config.Validate() != nil {
		return nil, ErrConfiguration
	}
	cleanup, err := NewWindowsCleanup(store, WindowsCleanupConfig{MaxInventoryRows: config.MaxInventoryRows, MaxInterfaces: config.MaxInterfaces, MaxProtected: config.MaxProtected, TransactionWait: config.TransactionWait})
	if err != nil {
		return nil, err
	}
	factory, err := NewWindowsTUNFactory(config.MaxInterfaces)
	if err != nil {
		return nil, err
	}
	ip, err := NewWindowsIP(config.MaxInventoryRows)
	if err != nil {
		return nil, err
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, ErrConfiguration
	}
	lifetime, cancel := context.WithCancel(parent)
	created, err := clock.Sample()
	if err != nil {
		cancel()
		return nil, err
	}
	lease, err := connectivity.NewLease(lifetime, clock, profileID, generation)
	if err != nil {
		cancel()
		return nil, err
	}
	return &WindowsSession{store: store, profileID: profileID, id: id.String(), generation: generation, config: config, lease: lease, lifetime: lifetime, created: created, clock: clock, cancel: cancel, ip: ip, cleanup: cleanup, createTUN: factory.Create, makeFilters: NewWindowsFilters, planFilters: PrepareFilterPlan, identity: nativeTUNAPI{}.identity, setMTU: setOwnedMTU, makeBind: func() conn.Bind { return conn.NewStdNetBind() }}, nil
}

func clearSessionKeys(value state.State) {
	for _, p := range value.Profiles {
		clear(p.Keys.WireGuardPrivate)
		clear(p.Keys.IdentityPrivate)
	}
}

func (s *WindowsSession) profile(ctx context.Context, m connectivity.Map, cursor bool) (state.Profile, error) {
	value, err := s.store.Load(ctx)
	if err != nil {
		return state.Profile{}, err
	}
	p, ok := value.Profiles[s.profileID]
	if !ok || value.ActiveProfileID != s.profileID {
		clearSessionKeys(value)
		return state.Profile{}, state.ErrStale
	}
	if err := m.CheckBinding(p, s.generation); err != nil {
		clearSessionKeys(value)
		return state.Profile{}, err
	}
	if cursor && (p.Connection.MapCursor == nil || *p.Connection.MapCursor != m.Cursor()) {
		clearSessionKeys(value)
		return state.Profile{}, state.ErrStale
	}
	// Keep only the selected WG key until synchronous engine application.
	clear(p.Keys.IdentityPrivate)
	for id, other := range value.Profiles {
		if id != s.profileID {
			clear(other.Keys.IdentityPrivate)
			clear(other.Keys.WireGuardPrivate)
		}
	}
	return p, nil
}

func (s *WindowsSession) change(ctx context.Context, m connectivity.Map, fn func(*state.ResourceJournal) error) (*state.ResourceJournal, error) {
	value, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	defer clearSessionKeys(value)
	var journal state.ResourceJournal
	err = s.store.Update(ctx, value.Revision, func(value *state.State) error {
		p, ok := value.Profiles[s.profileID]
		if !ok || value.ActiveProfileID != s.profileID {
			return state.ErrStale
		}
		if err := m.CheckBinding(p, s.generation); err != nil {
			return err
		}
		j := p.Connection.Journal
		if p.Connection.MapCursor == nil || *p.Connection.MapCursor != m.Cursor() || j == nil || j.ID != s.id || j.Generation != s.generation || j.Adapter == nil || j.Adapter.Phase != state.EffectApplied {
			return state.ErrStale
		}
		if err := fn(j); err != nil {
			return err
		}
		journal = copyJournal(*j)
		value.Profiles[s.profileID] = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &journal, nil
}

func (s *WindowsSession) replaceFilters(ctx context.Context, m connectivity.Map, plan FilterPlan) error {
	_, err := s.change(ctx, m, func(j *state.ResourceJournal) error {
		known := make(map[string]bool, len(j.Filters))
		desired := make(map[string]bool)
		for _, r := range plan.Records() {
			desired[r.Key] = true
		}
		for i, r := range j.Filters {
			known[r.Key] = true
			if !desired[r.Key] {
				j.Filters[i].Phase = state.EffectRemoving
			}
		}
		for _, r := range plan.Records() {
			if !known[r.Key] {
				j.Filters = append(j.Filters, r)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	receipts, err := s.filters.Replace(ctx, plan)
	if err != nil {
		return err
	}
	_, err = s.change(ctx, m, func(j *state.ResourceJournal) error {
		wanted := make(map[string]bool, len(receipts))
		for _, receipt := range receipts {
			wanted[receipt.Key] = true
			found := false
			for i, old := range j.Filters {
				if old.Key == receipt.Key {
					j.Filters[i] = receipt
					found = true
					break
				}
			}
			if !found {
				return state.ErrStale
			}
		}
		kept := make([]state.FilterRecord, 0, len(receipts))
		for _, r := range j.Filters {
			if wanted[r.Key] {
				kept = append(kept, r)
			} else if r.Phase != state.EffectRemoving {
				return state.ErrStale
			}
		}
		j.Filters = kept
		return nil
	})
	return err
}

// Start applies the initial verified map once. SDK errors are terminal for the
// session; no mutation retries occur. Stop uses a separate bounded context to
// reconcile the retained journal, including when this method's context expired.
func (s *WindowsSession) Start(ctx context.Context, m connectivity.Map) error {
	return s.start(ctx, m, false)
}

// Restore requires a fresh verifier receipt after this session's creation and
// an explicit <=30s deadline accepted by the daemon's restart reconciliation.
// It preserves the old durable intent/generation and never renews an old grant.
func (s *WindowsSession) Restore(ctx context.Context, m connectivity.Map) error {
	return s.start(ctx, m, true)
}

func (s *WindowsSession) start(ctx context.Context, m connectivity.Map, restore bool) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx == nil || s.started || s.stopped {
		return ErrConfiguration
	}
	s.started = true
	s.lease.PausePackets(true)
	operation, cancelOperation := context.WithCancel(ctx)
	defer cancelOperation()
	stopCancellation := context.AfterFunc(s.lifetime, cancelOperation)
	defer stopCancellation()
	defer func() {
		if err != nil {
			s.lease.Revoke()
			s.cancel()
			s.stopped = true
			err = errors.Join(err, s.releaseHandles())
		}
	}()
	if err := s.lifetime.Err(); err != nil {
		return err
	}
	sourceEpoch := s.sourceEpoch(ctx)
	p, err := s.profile(operation, m, false)
	if err != nil {
		return err
	}
	defer clear(p.Keys.WireGuardPrivate)
	if p.Connection.Journal != nil {
		return ErrAdapterCleanupRequired
	}
	budget, budgetErr := connectivity.OperationFromContext(ctx)
	if budgetErr != nil {
		return budgetErr
	}
	if err := budget.CheckBinding(s.profileID, s.generation); err != nil {
		return err
	}
	if !budget.Active() {
		return connectivity.ErrConfig
	}
	if !restore {
		r := p.Connection.Request
		if r == nil || r.ClockEpoch == "" || !budget.MatchesReceipt(connectivity.ClockSample{Wall: p.Connection.AcceptedAt, Epoch: r.ClockEpoch, Uptime: r.Uptime, UpperUptime: r.UpperUptime}, 30*time.Second) {
			return connectivity.ErrConfig
		}
	}
	if err := s.lease.GuardOperation(budget); err != nil {
		return err
	}
	s.operation = budget
	deadline := p.Connection.Deadline
	if restore {
		requested, bounded := ctx.Deadline()
		if !bounded || time.Until(requested) > 30*time.Second || !m.FreshSince(s.created) {
			return ErrConfiguration
		}
		deadline = requested
	}
	apply, cancel := context.WithDeadline(operation, deadline)
	defer cancel()
	if err := connectivity.CheckOperation(apply); err != nil {
		return err
	}
	if err := connectivity.PersistCursor(apply, s.store, s.generation, m); err != nil {
		return err
	}
	if err := s.lease.Confirm(m); err != nil {
		return err
	}
	prepared, err := PreparedWindowsAdapter(s.id)
	if err != nil {
		return err
	}
	s.tunnel, err = s.createTUN(apply, prepared, s.config.MTU, func(ctx context.Context, record state.AdapterRecord) error {
		return s.store.RecordAdapterCreation(ctx, s.profileID, s.generation, record)
	})
	if err != nil {
		return err
	}
	record := s.tunnel.Record()
	row, err := s.identity(record.LUID)
	if err != nil {
		return adapterError(err)
	}
	if row.Index == 0 || adapterProof(row) != record.Fingerprint {
		return ErrAdapterOwnership
	}
	if err := s.setMTU(apply, record, s.config.MTU); err != nil {
		return err
	}
	ipPlan, err := PrepareIPPlan(m, s.id, record.LUID, s.config.RouteMetric, s.config.MaxPeers)
	if err != nil {
		return err
	}
	plan, err := s.planFilters(apply, m, s.id, record.LUID, row.Index, s.config.MaxGrants, s.config.MaxProtected, false)
	if err != nil {
		return err
	}
	j, err := s.change(apply, m, func(*state.ResourceJournal) error { return nil })
	if err != nil {
		return err
	}
	if err := s.ip.Preflight(apply, ipPlan, *j); err != nil {
		return err
	}
	s.filters, err = s.makeFilters(s.id, s.config.TransactionWait, func(ctx context.Context, records []state.FilterRecord) error {
		return s.store.RecordPreparedGuardIDs(ctx, s.profileID, s.id, s.generation, records)
	})
	if err != nil {
		return err
	}
	// Denies exist before any protected address or /32 route reaches Windows.
	if err := s.replaceFilters(apply, m, plan.DenyAllTraffic()); err != nil {
		return err
	}
	a, routes := ipPlan.Prepared()
	j, err = s.change(apply, m, func(j *state.ResourceJournal) error {
		j.Addresses = []state.AddressRecord{a}
		j.Routes = routes
		return nil
	})
	if err != nil {
		return err
	}
	effect, err := s.ip.CreateAddress(apply, ipPlan, *j)
	if err != nil {
		return err
	}
	for {
		j, err = s.change(apply, m, func(j *state.ResourceJournal) error { j.Addresses[0] = effect.Record; return nil })
		if err != nil {
			return err
		}
		if effect.Ready {
			break
		}
		timer := time.NewTimer(s.config.DADPollInterval)
		select {
		case <-apply.Done():
			timer.Stop()
			return apply.Err()
		case <-timer.C:
		}
		effect, err = s.ip.InspectAddress(apply, ipPlan, effect.Record)
		if err != nil {
			return err
		}
	}
	for i, route := range routes {
		receipt, err := s.ip.CreateRoute(apply, ipPlan, route, *j)
		if err != nil {
			return err
		}
		j, err = s.change(apply, m, func(j *state.ResourceJournal) error { j.Routes[i] = receipt; return nil })
		if err != nil {
			return err
		}
	}
	if err := s.replaceFilters(apply, m, plan); err != nil {
		return err
	}
	current, err := s.profile(apply, m, true)
	if err != nil {
		return err
	}
	defer clear(current.Keys.WireGuardPrivate)
	// Pause keeps the original receipt deadline running during native work.
	// No second Confirm can extend it after expensive native operations.
	if s.lease.Permissions().ClockUnsafe {
		return connectivity.ErrClock
	}
	s.engine, err = NewEngine(s.tunnel, s.makeBind(), s.config.MaxPeers, s.config.ListenPort, s.lease)
	if err != nil {
		return err
	}
	if err := s.engine.Apply(apply, m, current.Keys.WireGuardPrivate); err != nil {
		return err
	}
	if _, err := s.engine.BoundPort(); err != nil {
		return err
	}
	if err := s.activationFence(apply, m); err != nil {
		return err
	}
	if err := connectivity.CheckOperation(apply); err != nil {
		return err
	}
	s.current, s.policy = m, plan
	resumed, err := s.resumeSource(sourceEpoch)
	if err != nil {
		return err
	}
	if !resumed {
		return nil
	}
	if !s.lease.Permissions().NewFlows {
		return connectivity.ErrExpired
	}
	return nil
}

func (s *WindowsSession) activationFence(ctx context.Context, m connectivity.Map) error {
	p, err := s.profile(ctx, m, true)
	if err != nil {
		return err
	}
	clear(p.Keys.WireGuardPrivate)
	return ctx.Err()
}

func (s *WindowsSession) releaseHandles() error {
	var err error
	// Remove dynamic exceptions before closing the adapter, preventing recycled
	// interface identities from inheriting this process's old permits.
	if s.filters != nil {
		if err := s.filters.ReleaseForRecovery(); err != nil {
			return err
		}
	}
	if s.engine != nil {
		s.engine.Close()
	}
	if s.tunnel != nil {
		err = errors.Join(err, s.tunnel.Close())
	}
	return err
}

func (s *WindowsSession) Stop(ctx context.Context) error {
	s.lease.Revoke() // before waiting for an in-flight Start's native call
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	var markErr error
	if ctx == nil {
		markErr = ErrConfiguration
	} else {
		value, err := s.store.Load(ctx)
		if err != nil {
			markErr = err
		} else {
			j, err := selectedJournal(value, s.profileID, s.id, s.generation)
			if err != nil {
				markErr = err
			} else if j != nil {
				_, markErr = s.cleanup.change(ctx, s.profileID, *j, markJournalRemoving)
			}
			clearSessionKeys(value)
		}
	}
	if err := errors.Join(markErr, s.releaseHandles()); err != nil {
		return err
	}
	return s.cleanup.Run(ctx, s.profileID, s.id, s.generation)
}

func (s *WindowsSession) BoundPort() (uint16, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine == nil {
		return 0, ErrClosed
	}
	return s.engine.BoundPort()
}

func setOwnedMTU(ctx context.Context, record state.AdapterRecord, mtu int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	row, err := (nativeTUNAPI{}).identity(record.LUID)
	if err != nil {
		return adapterError(err)
	}
	if adapterProof(row) != record.Fingerprint {
		return ErrAdapterOwnership
	}
	ip, err := winipcfg.LUID(record.LUID).IPInterface(windows.AF_INET)
	if err != nil {
		return ipError(err)
	}
	if ip == nil {
		return ErrIPUncertain
	}
	ip.NLMTU, ip.SitePrefixLength = uint32(mtu), 0
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := ip.Set(); err != nil {
		return errors.Join(ErrIPUncertain, ipError(err))
	}
	actual, err := winipcfg.LUID(record.LUID).IPInterface(windows.AF_INET)
	if err != nil || actual == nil || actual.InterfaceLUID != winipcfg.LUID(record.LUID) || actual.Family != windows.AF_INET || actual.NLMTU != uint32(mtu) || ctx.Err() != nil {
		return ErrIPUncertain
	}
	return nil
}
