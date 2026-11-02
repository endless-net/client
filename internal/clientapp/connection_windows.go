//go:build windows

package clientapp

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type WindowsConnectionConfig struct {
	Verifier                                                                connectivity.VerifierConfig
	Native                                                                  dataplane.WindowsSessionConfig
	Endpoint                                                                netip.AddrPort
	EndpointTTL, RenewalInterval, RetryInterval, ProbeInterval, WriteBudget time.Duration
	MaxQueuedMaps                                                           int
}

// validateSettings performs no I/O and needs no runtime clock or trust provider.
func (c WindowsConnectionConfig) validateSettings() error {
	if c.Native.Validate() != nil || c.Verifier.MaxBytes <= 0 || c.Verifier.MaxPeers <= 0 || c.Verifier.MaxRules <= 0 || c.Verifier.MaxCredentialBytes <= 0 || c.Verifier.CallTimeout < time.Millisecond || c.Verifier.CallTimeout > 30*time.Second || !c.Endpoint.IsValid() || !c.Endpoint.Addr().Is4() || !c.Endpoint.Addr().IsGlobalUnicast() || c.Endpoint.Addr().IsLoopback() || c.Endpoint.Port() == 0 || c.EndpointTTL < time.Millisecond || c.EndpointTTL > 300*time.Second || c.RenewalInterval < time.Millisecond || c.RenewalInterval >= c.EndpointTTL || c.RetryInterval < time.Millisecond || c.RetryInterval > 30*time.Second || c.ProbeInterval < time.Millisecond || c.ProbeInterval > 30*time.Second || c.WriteBudget < time.Millisecond || c.WriteBudget > 10*time.Second || c.MaxQueuedMaps <= 0 {
		return ErrConnectionWorker
	}
	return nil
}

type windowsConnectionNative interface {
	Start(context.Context, connectivity.Map) error
	Restore(context.Context, connectivity.Map) error
	Refresh(context.Context, connectivity.Map) error
	Offline(context.Context) error
	Stop(context.Context) error
	WaitDirect(context.Context, time.Duration) error
	ProbeDirect(context.Context) error
	BoundPort() (uint16, error)
	BlockSource() uint64
	RevokeSource()
	ConnectionLocal() connectivity.LocalObservation
}

type windowsConnections struct {
	store      *state.Store
	transport  *connectivity.Transport
	config     WindowsConnectionConfig
	makeNative func(context.Context, string, uint64) (windowsConnectionNative, error)
	cleanup    func(context.Context, string) error
	stream     func(context.Context, *connectivity.Verifier, *state.MapCursor, func(context.Context, connectivity.Map) error) error
}

// NewWindowsConnectionFactory allocates no adapter or background worker. The
// returned factory allocates a per-generation owner only; Connect owns bounded
// cleanup/fresh authorization/native apply/announcement/path confirmation.
func NewWindowsConnectionFactory(store *state.Store, transport *connectivity.Transport, c WindowsConnectionConfig) (ConnectionFactory, error) {
	if store == nil || transport == nil || c.Verifier.Clock == nil || c.Verifier.Announcements == nil || c.Verifier.Now == nil || c.validateSettings() != nil {
		return nil, ErrConnectionWorker
	}
	cleanup, err := dataplane.NewWindowsCleanup(store, dataplane.WindowsCleanupConfig{MaxInventoryRows: c.Native.MaxInventoryRows, MaxInterfaces: c.Native.MaxInterfaces, MaxProtected: c.Native.MaxProtected, TransactionWait: c.Native.TransactionWait})
	if err != nil {
		return nil, err
	}
	f := &windowsConnections{store: store, transport: transport, config: c, stream: transport.StreamSnapshot}
	f.makeNative = func(ctx context.Context, profile string, generation uint64) (windowsConnectionNative, error) {
		return dataplane.NewWindowsSession(ctx, store, c.Verifier.Clock, profile, generation, c.Native)
	}
	f.cleanup = func(ctx context.Context, profile string) error {
		value, err := store.Load(ctx)
		if err != nil {
			return err
		}
		defer clearConnectionKeys(value)
		p, ok := value.Profiles[profile]
		if !ok || p.Connection == nil {
			return state.ErrStale
		}
		j := p.Connection.Journal
		if j == nil {
			return nil
		}
		return cleanup.Run(ctx, profile, j.ID, j.Generation)
	}
	return f.newOwner, nil
}

func clearConnectionKeys(value state.State) {
	for _, p := range value.Profiles {
		clear(p.Keys.IdentityPrivate)
		clear(p.Keys.WireGuardPrivate)
	}
}

type connectionFrame struct {
	ctx   context.Context
	value connectivity.Map
	epoch uint64
	err   error
}

type windowsConnection struct {
	factory    *windowsConnections
	profile    string
	generation uint64
	lifetime   context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	native     windowsConnectionNative
	used       bool
	done       chan struct{}
	loopErr    error
	frames     chan connectionFrame
	fatal      chan error
	applied    chan struct{}
}

func (*windowsConnection) String() string   { return "[private Windows connection]" }
func (*windowsConnection) GoString() string { return "[private Windows connection]" }

func (d *windowsConnection) ConnectionLocal() connectivity.LocalObservation {
	d.mu.Lock()
	native := d.native
	d.mu.Unlock()
	if native == nil {
		return connectivity.LocalObservation{}
	}
	return native.ConnectionLocal()
}

func (f *windowsConnections) newOwner(lifetime context.Context, profile string, generation uint64) (ConnectionSession, error) {
	if lifetime == nil || lifetime.Err() != nil || !validConfigID(profile) || generation == 0 {
		return nil, ErrConnectionWorker
	}
	ctx, cancel := context.WithCancel(lifetime)
	return &windowsConnection{factory: f, profile: profile, generation: generation, lifetime: ctx, cancel: cancel, frames: make(chan connectionFrame, f.config.MaxQueuedMaps), fatal: make(chan error, 1), applied: make(chan struct{}, 1)}, nil
}

func (d *windowsConnection) profileAt(ctx context.Context) (state.State, state.Profile, error) {
	value, err := d.factory.store.Load(ctx)
	if err != nil {
		return state.State{}, state.Profile{}, err
	}
	p, ok := value.Profiles[d.profile]
	if !ok || value.ActiveProfileID != d.profile || p.Connection == nil || p.Connection.Generation != d.generation {
		clearConnectionKeys(value)
		return state.State{}, state.Profile{}, state.ErrStale
	}
	return value, p, nil
}

func (d *windowsConnection) Connect(operation context.Context, restore bool) error {
	if err := connectivity.CheckOperation(operation); err != nil {
		return err
	}
	d.mu.Lock()
	if d.used {
		d.mu.Unlock()
		return ErrConnectionWorker
	}
	d.used = true
	d.mu.Unlock()
	value, p, err := d.profileAt(operation)
	if err != nil {
		return err
	}
	connected := p.Connection.Intent == state.IntentConnected
	clearConnectionKeys(value)
	if !connected {
		return state.ErrStale
	}
	if err := d.factory.cleanup(operation, d.profile); err != nil {
		return nativeConnectionFailure(err, connectivity.FailureCleanup)
	}
	native, err := d.factory.makeNative(d.lifetime, d.profile, d.generation)
	if err != nil {
		return nativeConnectionFailure(err, connectivity.FailureApply)
	}
	d.mu.Lock()
	d.native = native
	d.done = make(chan struct{})
	done := d.done
	d.mu.Unlock()
	readerDone := make(chan struct{})
	go func() { defer close(readerDone); d.readMaps(native) }()
	go func() {
		err := d.applyMaps(operation, restore, native)
		d.cancel()
		<-readerDone
		d.mu.Lock()
		d.loopErr = err
		d.mu.Unlock()
		close(done)
	}()
	select {
	case <-operation.Done():
		return operation.Err()
	case <-d.applied:
		if err := native.WaitDirect(operation, d.factory.config.ProbeInterval); err != nil {
			return nativeConnectionFailure(err, connectivity.FailurePath)
		}
		return connectivity.CheckOperation(operation)
	case <-done:
		return d.result()
	}
}

func (d *windowsConnection) result() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.loopErr == nil {
		return ErrConnectionOperation
	}
	return d.loopErr
}

func (d *windowsConnection) Serve(ctx context.Context) error {
	d.mu.Lock()
	done := d.done
	d.mu.Unlock()
	if done == nil {
		return ErrConnectionWorker
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return d.result()
	}
}

func transientMapError(err error) bool {
	return errors.Is(err, connectivity.ErrUnavailable) || errors.Is(err, connectivity.ErrResync) || errors.Is(err, context.DeadlineExceeded)
}

func (d *windowsConnection) failSource(native windowsConnectionNative, err error) {
	native.RevokeSource()
	select {
	case d.fatal <- err:
	default:
	}
}

func (d *windowsConnection) enqueue(native windowsConnectionNative, frame connectionFrame) error {
	select {
	case d.frames <- frame:
		return nil
	default:
		d.failSource(native, ErrConnectionWorker)
		return ErrConnectionWorker
	}
}

func (d *windowsConnection) readMaps(native windowsConnectionNative) {
	for {
		if d.lifetime.Err() != nil {
			return
		}
		read, cancel := context.WithTimeout(d.lifetime, d.factory.config.WriteBudget)
		value, p, err := d.profileAt(read)
		var verifier *connectivity.Verifier
		var cursor *state.MapCursor
		if err == nil {
			if p.Connection.Intent != state.IntentConnected {
				err = state.ErrStale
			} else {
				verifier, err = connectivity.NewVerifier(p, d.factory.config.Verifier)
				cursor = p.Connection.MapCursor
			}
			clearConnectionKeys(value)
		}
		cancel()
		if err == nil {
			err = d.factory.stream(d.lifetime, verifier, cursor, func(ctx context.Context, m connectivity.Map) error {
				epoch := native.BlockSource()
				if m.Denied() {
					d.failSource(native, connectivity.ErrRevoked)
					return connectivity.ErrRevoked
				}
				return d.enqueue(native, connectionFrame{ctx: ctx, value: m, epoch: epoch})
			})
		}
		if d.lifetime.Err() != nil {
			return
		}
		epoch := native.BlockSource()
		if !transientMapError(err) {
			d.failSource(native, err)
			return
		}
		if err := d.enqueue(native, connectionFrame{epoch: epoch, err: connectivity.ErrUnavailable}); err != nil {
			return
		}
		timer := time.NewTimer(d.factory.config.RetryInterval)
		select {
		case <-d.lifetime.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (d *windowsConnection) applyMaps(initial context.Context, restore bool, native windowsConnectionNative) error {
	renew := time.NewTicker(d.factory.config.RenewalInterval)
	defer renew.Stop()
	probe := time.NewTicker(d.factory.config.ProbeInterval)
	defer probe.Stop()
	started := false
	for {
		select {
		case <-d.lifetime.Done():
			return d.lifetime.Err()
		case err := <-d.fatal:
			return err
		case frame := <-d.frames:
			ctx, cancel := context.WithTimeout(d.lifetime, d.factory.config.Native.ApplyTimeout)
			ctx = dataplane.WithSourceEpoch(ctx, frame.epoch)
			if frame.err != nil {
				if started {
					err := native.Offline(ctx)
					cancel()
					if err != nil {
						return nativeConnectionFailure(err, connectivity.FailureApply)
					}
				} else {
					cancel()
				}
				continue
			}
			if frame.ctx.Err() != nil {
				cancel()
				continue
			}
			var err error
			if !started {
				cancel()
				ctx, cancel = context.WithCancel(initial)
				ctx = dataplane.WithSourceEpoch(ctx, frame.epoch)
			}
			stop := context.AfterFunc(frame.ctx, cancel)
			if !started {
				if restore {
					err = native.Restore(ctx, frame.value)
				} else {
					err = native.Start(ctx, frame.value)
				}
			} else {
				err = native.Refresh(ctx, frame.value)
			}
			stop()
			cancel()
			if err != nil && !errors.Is(err, connectivity.ErrReplay) {
				return nativeConnectionFailure(err, connectivity.FailureApply)
			}
			if !started {
				started = true
				if err := d.announce(initial, true, native); err != nil {
					return err
				}
				select {
				case d.applied <- struct{}{}:
				default:
				}
			}
		case <-renew.C:
			if started {
				ctx, cancel := context.WithTimeout(d.lifetime, d.factory.config.WriteBudget)
				err := d.announce(ctx, true, native)
				cancel()
				if err != nil && !errors.Is(err, connectivity.ErrEndpointUncertain) && !errors.Is(err, connectivity.ErrUnavailable) {
					return err
				}
			}
		case <-probe.C:
			if started {
				ctx, cancel := context.WithTimeout(d.lifetime, d.factory.config.WriteBudget)
				err := native.ProbeDirect(ctx)
				cancel()
				if err != nil && !errors.Is(err, dataplane.ErrNoAuthority) {
					return nativeConnectionFailure(err, connectivity.FailurePath)
				}
			}
		}
	}
}

func (d *windowsConnection) announce(ctx context.Context, online bool, native windowsConnectionNative) error {
	want := state.IntentDisconnected
	status := "offline"
	if online {
		want = state.IntentConnected
		status = "online"
	}
	// At most one frozen recovery and one explicit current announcement. An
	// uncertain response is never retried here, nor does recovery renew a lease.
	for range 2 {
		if err := announcementContext(ctx); err != nil {
			return err
		}
		if online {
			port, err := native.BoundPort()
			if err != nil {
				return nativeConnectionFailure(err, connectivity.FailureApply)
			}
			if port != d.factory.config.Native.ListenPort {
				return connectivity.FailureApply
			}
		}
		value, p, err := d.profileAt(ctx)
		if err != nil {
			return err
		}
		clearConnectionKeys(value)
		if p.Connection.Intent != want {
			return state.ErrStale
		}
		request := p.Connection.EndpointRequest
		endpointGeneration := p.Connection.EndpointGeneration
		if request == nil || request.IntentGeneration != d.generation || request.Receipt != nil {
			if endpointGeneration == ^uint64(0) {
				return state.ErrStale
			}
			endpointGeneration++
			r := coordinator.UpdateNodeEndpointRequest{Endpoint: d.factory.config.Endpoint.String(), Generation: endpointGeneration, TTL: d.factory.config.EndpointTTL.String(), Status: status}
			if err := connectivity.PrepareEndpoint(ctx, d.factory.store, d.profile, d.generation, r); err != nil {
				return err
			}
		}
		if err := announcementContext(ctx); err != nil {
			return err
		}
		receipt, err := connectivity.SendEndpoint(ctx, d.factory.store, d.factory.transport, d.factory.config.Verifier, d.profile, d.generation, endpointGeneration)
		if err != nil {
			return err
		}
		if err := announcementContext(ctx); err != nil {
			return err
		}
		if !online || receipt.Online && receipt.Endpoint == d.factory.config.Endpoint.String() && receipt.ExpiresAt.After(d.factory.config.Verifier.Now()) {
			return nil
		}
	}
	return connectivity.ErrEndpointUncertain
}

func announcementContext(ctx context.Context) error {
	if ctx == nil {
		return ErrConnectionWorker
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrConnectionWorker
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Initial Connect/Restore carries the accepted suspend-inclusive budget.
	// Periodic renewal and offline discovery carry their own bounded context.
	if budget, err := connectivity.OperationFromContext(ctx); err == nil {
		return budget.Check()
	}
	return nil
}

func (d *windowsConnection) Disconnect(ctx context.Context) error {
	if ctx == nil {
		return ErrConnectionWorker
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return ErrConnectionWorker
	}
	d.cancel()
	d.mu.Lock()
	native, done := d.native, d.done
	d.mu.Unlock()
	if native != nil {
		if err := native.Stop(ctx); err != nil {
			return nativeConnectionFailure(err, connectivity.FailureCleanup)
		}
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := d.factory.cleanup(ctx, d.profile); err != nil {
		return nativeConnectionFailure(err, connectivity.FailureCleanup)
	}
	// Cleanup of an old generation never publishes against the newer intent.
	value, p, err := d.profileAt(ctx)
	if err != nil {
		if errors.Is(err, state.ErrStale) {
			return nil
		}
		return err
	}
	disconnected := p.Connection.Intent == state.IntentDisconnected
	clearConnectionKeys(value)
	if disconnected {
		call, cancel := context.WithTimeout(ctx, d.factory.config.WriteBudget)
		defer cancel()
		// This ACK is discovery only. Local teardown success does not depend on
		// control-plane availability; the frozen pending request remains visible.
		_ = d.announce(call, false, native)
	}
	return ctx.Err()
}
