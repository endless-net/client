package clientapp

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/control"
	"github.com/endless-net/client/internal/dataplane"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type connectionConfig struct {
	Verifier                                                                connectivity.VerifierConfig
	Endpoint                                                                netip.AddrPort
	EndpointTTL, RenewalInterval, RetryInterval, ProbeInterval, WriteBudget time.Duration
	MaxQueuedMaps                                                           int
	ApplyTimeout                                                            time.Duration
	ListenPort                                                              uint16
	PrepareCredentials                                                      func(context.Context, string, uint64) error
	RefreshCredentials                                                      func(context.Context, string, uint64, bool, bool) error
	ReportFailure                                                           func(uint64, string, error)
	ClassifyCleanup                                                         func(error) error
}

type nativeConnectionSession interface {
	Start(context.Context, connectivity.Map) error
	Restore(context.Context, connectivity.Map) error
	Refresh(context.Context, connectivity.Map) error
	Offline(context.Context) error
	Stop(context.Context) error
	CheckReady(context.Context) error
	ProbePaths(context.Context) error
	BoundPort() (uint16, error)
	TransportAdvertisement(netip.Addr) (coordinator.TransportAdvertisement, error)
	AdvertisementSignal() <-chan struct{}
	BlockSource() uint64
	RevokeSource()
	ConnectionLocal() connectivity.LocalObservation
}

type connectionFactory struct {
	store      *state.Store
	transport  *connectivity.Transport
	config     connectionConfig
	makeNative func(context.Context, string, uint64) (nativeConnectionSession, error)
	cleanup    func(context.Context, string) error
	stream     func(context.Context, *connectivity.Verifier, *state.MapCursor, func(context.Context, connectivity.Map) error) error
	endpoint   func() (netip.AddrPort, error)
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

type connectionDriver struct {
	factory              *connectionFactory
	verifier             connectivity.VerifierConfig
	profile              string
	generation           uint64
	lifetime             context.Context
	cancel               context.CancelFunc
	mu                   sync.Mutex
	credentialMu         sync.RWMutex
	mapMu                sync.Mutex
	mapApplyMu           sync.Mutex
	mapCancel            context.CancelCauseFunc
	mapResume, mapParked chan struct{}
	credentialsReady     chan struct{}
	credentialsStarted   chan struct{}
	native               nativeConnectionSession
	used                 bool
	done                 chan struct{}
	loopErr              error
	frames               chan connectionFrame
	fatal                chan error
	applied              chan struct{}
	mapOffline           atomic.Bool
	endpointUnconfirmed  atomic.Bool
}

func (*connectionDriver) String() string   { return "[private connection]" }
func (*connectionDriver) GoString() string { return "[private connection]" }

func (d *connectionDriver) ConnectionLocal() connectivity.LocalObservation {
	d.mu.Lock()
	native := d.native
	d.mu.Unlock()
	if native == nil {
		return connectivity.LocalObservation{}
	}
	local := native.ConnectionLocal()
	local.ControlUnavailable = d.mapOffline.Load() && !local.Permission.NewFlows
	local.EndpointRefreshUnconfirmed = d.endpointUnconfirmed.Load()
	return local
}

func (f *connectionFactory) newOwner(lifetime context.Context, profile string, generation uint64) (ConnectionSession, error) {
	if lifetime == nil || lifetime.Err() != nil || !validConfigID(profile) || generation == 0 {
		return nil, ErrConnectionWorker
	}
	ctx, cancel := context.WithCancel(lifetime)
	return &connectionDriver{factory: f, verifier: f.config.Verifier, profile: profile, generation: generation, lifetime: ctx, cancel: cancel, frames: make(chan connectionFrame, f.config.MaxQueuedMaps), fatal: make(chan error, 1), applied: make(chan struct{}, 1), credentialsReady: make(chan struct{}), credentialsStarted: make(chan struct{})}, nil
}

func (d *connectionDriver) profileAt(ctx context.Context) (state.State, state.Profile, error) {
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

func (d *connectionDriver) Connect(operation context.Context, restore bool) error {
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
	if prepare := d.factory.config.PrepareCredentials; prepare != nil {
		if err := prepare(operation, d.profile, d.generation); err != nil {
			return nativeConnectionFailure(err, connectivity.FailureControl)
		}
	}
	native, err := d.factory.makeNative(d.lifetime, d.profile, d.generation)
	if err != nil {
		d.reportNativeFailure("apply_session_create", err)
		return nativeConnectionFailure(err, connectivity.FailureApply)
	}
	if bootstrap, ok := d.verifier.Announcements.(*control.Bootstrap); ok {
		initial, finish := context.WithCancel(operation)
		defer finish()
		d.verifier.Announcements = bootstrap.ForConnection(initial, p.ControlOrigin)
	}
	d.mu.Lock()
	d.native = native
	d.done = make(chan struct{})
	done := d.done
	d.mu.Unlock()
	readerDone := make(chan struct{})
	credentialsDone := make(chan struct{})
	go func() { defer close(readerDone); d.readMaps(native) }()
	go func() { defer close(credentialsDone); d.maintainCredentials(native) }()
	go func() {
		err := d.applyMaps(operation, restore, native)
		d.cancel()
		<-readerDone
		<-credentialsDone
		d.mu.Lock()
		d.loopErr = err
		d.mu.Unlock()
		close(done)
	}()
	select {
	case <-operation.Done():
		return operation.Err()
	case <-d.applied:
		if d.factory.config.RefreshCredentials != nil {
			select {
			case <-operation.Done():
				return operation.Err()
			case <-done:
				return d.result()
			case <-d.credentialsStarted:
			}
		}
		return connectivity.CheckOperation(operation)
	case <-done:
		return d.result()
	}
}

func (d *connectionDriver) result() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.loopErr == nil {
		return ErrConnectionOperation
	}
	return d.loopErr
}

func (d *connectionDriver) Serve(ctx context.Context) error {
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

func (d *connectionDriver) failSource(native nativeConnectionSession, err error) {
	native.RevokeSource()
	select {
	case d.fatal <- err:
	default:
	}
}

// Terminal source failure revokes traffic before this bounded metadata write.
// A later intent cannot lose its hints to this old generation's callback.
func (d *connectionDriver) invalidateTransportHints() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(d.lifetime), d.factory.config.WriteBudget)
	defer cancel()
	for ctx.Err() == nil {
		value, _, err := d.profileAt(ctx)
		if err != nil {
			return
		}
		err = d.factory.store.Update(ctx, value.Revision, func(next *state.State) error {
			p := next.Profiles[d.profile]
			if next.ActiveProfileID != d.profile || p.Connection == nil || p.Connection.Generation != d.generation {
				return state.ErrStale
			}
			kept := next.TransportHints[:0]
			for _, hint := range next.TransportHints {
				if hint.Key.LocalProfileID != d.profile {
					kept = append(kept, hint)
				}
			}
			next.TransportHints = kept
			return nil
		})
		clearConnectionKeys(value)
		if !errors.Is(err, state.ErrStale) && !errors.Is(err, state.ErrUncertain) {
			return
		}
	}
}

func (d *connectionDriver) enqueue(native nativeConnectionSession, frame connectionFrame) error {
	select {
	case d.frames <- frame:
		return nil
	default:
		d.failSource(native, ErrConnectionWorker)
		return ErrConnectionWorker
	}
}

func (d *connectionDriver) readMaps(native nativeConnectionSession) {
	for {
		if d.lifetime.Err() != nil {
			return
		}
		streamContext, streamCancel := d.beginMapStream()
		read, cancel := context.WithTimeout(streamContext, d.factory.config.WriteBudget)
		value, p, err := d.profileAt(read)
		var verifier *connectivity.Verifier
		var cursor *state.MapCursor
		if err == nil {
			if p.Connection.Intent != state.IntentConnected {
				err = state.ErrStale
			} else {
				verifier, err = connectivity.NewVerifier(p, d.verifier)
				cursor = p.Connection.MapCursor
			}
			clearConnectionKeys(value)
		}
		cancel()
		if err == nil {
			err = d.factory.stream(streamContext, verifier, cursor, func(ctx context.Context, m connectivity.Map) error {
				epoch := native.BlockSource()
				if m.Denied() {
					d.failSource(native, connectivity.ErrRevoked)
					return connectivity.ErrRevoked
				}
				return d.enqueue(native, connectionFrame{ctx: ctx, value: m, epoch: epoch})
			})
		}
		d.mapMu.Lock()
		handoff := errors.Is(context.Cause(streamContext), errCredentialHandoff)
		d.mapCancel = nil
		d.mapMu.Unlock()
		streamCancel(nil)
		if d.lifetime.Err() != nil {
			return
		}
		if handoff || errors.Is(err, connectivity.ErrStreamRotation) {
			// Rotation keeps the signed lease as-is. A replacement failure
			// still takes the normal offline or terminal path below.
			continue
		}
		d.reportNativeFailure("map_stream_ended", err)
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

func (d *connectionDriver) applyMaps(initial context.Context, restore bool, native nativeConnectionSession) error {
	advertisements := native.AdvertisementSignal()
	renew := time.NewTicker(d.factory.config.RenewalInterval)
	defer renew.Stop()
	probe := time.NewTicker(d.factory.config.ProbeInterval)
	defer probe.Stop()
	started := false
	waitingForReady := initial.Done()
	for {
		select {
		case <-waitingForReady:
			return initial.Err()
		case <-d.lifetime.Done():
			return d.lifetime.Err()
		case err := <-d.fatal:
			d.invalidateTransportHints()
			return err
		case frame := <-d.frames:
			d.mapApplyMu.Lock()
			if frame.ctx != nil && frame.ctx.Err() != nil {
				d.mapApplyMu.Unlock()
				continue
			}
			parent := d.lifetime
			if waitingForReady != nil {
				parent = initial
			}
			var ctx context.Context
			var cancel context.CancelFunc
			if !started && frame.err == nil {
				ctx, cancel = context.WithCancel(parent)
			} else {
				ctx, cancel = context.WithTimeout(parent, d.factory.config.ApplyTimeout)
			}
			ctx = dataplane.WithSourceEpoch(ctx, frame.epoch)
			if frame.err != nil {
				var err error
				if started {
					err = native.Offline(ctx)
				}
				cancel()
				d.mapApplyMu.Unlock()
				if err != nil {
					d.reportNativeFailure("apply_offline", err)
					return nativeConnectionFailure(err, connectivity.FailureApply)
				}
				d.mapOffline.Store(true)
				continue
			}
			var err error
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
			d.mapApplyMu.Unlock()
			if err != nil && !errors.Is(err, connectivity.ErrReplay) {
				if started {
					d.reportNativeFailure("apply_refresh", err)
				}
				return nativeConnectionFailure(err, connectivity.FailureApply)
			}
			if err == nil {
				d.mapOffline.Store(false)
			}
			if !started {
				started = true
				if err := d.announce(initial, true, native); err != nil {
					return err
				}
			}
			if waitingForReady != nil {
				err := native.CheckReady(dataplane.WithSourceEpoch(initial, frame.epoch))
				if errors.Is(err, connectivity.ErrSourceChanged) {
					continue
				}
				if err != nil {
					return nativeConnectionFailure(err, connectivity.FailureApply)
				}
				waitingForReady = nil
				close(d.credentialsReady)
				select {
				case d.applied <- struct{}{}:
				default:
				}
			}
		case <-renew.C:
			if started && waitingForReady == nil {
				if err := d.refreshEndpoint(native); err != nil {
					return err
				}
			}
		case <-advertisements:
			if started && waitingForReady == nil {
				if err := d.refreshEndpoint(native); err != nil {
					return err
				}
			}
		case <-probe.C:
			if started && waitingForReady == nil {
				ctx, cancel := context.WithTimeout(d.lifetime, d.factory.config.WriteBudget)
				err := native.ProbePaths(ctx)
				cancel()
				if errors.Is(err, context.DeadlineExceeded) && d.lifetime.Err() == nil {
					// Unknown on this tick; signed lease/packet guards remain strict.
					continue
				}
				if err != nil && !errors.Is(err, dataplane.ErrNoAuthority) {
					return nativeConnectionFailure(err, connectivity.FailurePath)
				}
			}
		}
	}
}

// Endpoint requests never race a credential-head transition. A parked renewal
// must not stall native map apply/probe; the next timer uses the latest state.
func (d *connectionDriver) refreshEndpoint(native nativeConnectionSession) error {
	ctx, cancel := context.WithTimeout(d.lifetime, d.factory.config.WriteBudget)
	defer cancel()
	err := d.announceCurrent(ctx, native)
	switch {
	case err == nil:
		d.endpointUnconfirmed.Store(false)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, connectivity.ErrEndpointUncertain), errors.Is(err, connectivity.ErrUnavailable):
		d.endpointUnconfirmed.Store(true)
		d.reportNativeFailure("endpoint_refresh", err)
	default:
		return err
	}
	return nil
}

func (d *connectionDriver) announceCurrent(ctx context.Context, native nativeConnectionSession) error {
	if !d.credentialMu.TryRLock() {
		return nil
	}
	defer d.credentialMu.RUnlock()
	return d.announce(ctx, true, native)
}

func (d *connectionDriver) announce(ctx context.Context, online bool, native nativeConnectionSession) error {
	want := state.IntentDisconnected
	status := "offline"
	if online {
		want = state.IntentConnected
		status = "online"
	}
	endpoint := d.factory.config.Endpoint
	if online && d.factory.endpoint != nil {
		var err error
		endpoint, err = d.factory.endpoint()
		if err != nil {
			return connectivity.FailurePath
		}
	}
	var desired *coordinator.TransportAdvertisement
	if online {
		ad, err := native.TransportAdvertisement(endpoint.Addr())
		if err != nil {
			return nativeConnectionFailure(err, connectivity.FailurePath)
		}
		desired = &ad
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
				d.reportNativeFailure("apply_listen_port", err)
				return nativeConnectionFailure(err, connectivity.FailureApply)
			}
			if port != d.factory.config.ListenPort {
				d.reportNativeFailure("apply_listen_port", connectivity.FailureApply)
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
			r := coordinator.UpdateNodeEndpointRequest{Endpoint: endpoint.String(), Generation: endpointGeneration, TTL: d.factory.config.EndpointTTL.String(), Status: status, Transport: desired}
			if err := connectivity.PrepareEndpoint(ctx, d.factory.store, d.profile, d.generation, r); err != nil {
				return err
			}
		}
		if err := announcementContext(ctx); err != nil {
			return err
		}
		verifier := d.factory.config.Verifier
		if online {
			verifier = d.verifier
		}
		receipt, err := connectivity.SendEndpoint(ctx, d.factory.store, d.factory.transport, verifier, d.profile, d.generation, endpointGeneration)
		if err != nil {
			return err
		}
		if err := announcementContext(ctx); err != nil {
			return err
		}
		current := coordinator.UpdateNodeEndpointRequest{Endpoint: endpoint.String(), Generation: receipt.Generation, TTL: d.factory.config.EndpointTTL.String(), Status: status, Transport: desired}
		if !online || receipt.Online && receipt.Endpoint == endpoint.String() && receipt.ExpiresAt.After(d.factory.config.Verifier.Now()) && coordinator.ValidateEndpointReceipt(current, receipt, p.Attempt.NodeID, p.Attempt.NetworkID) == nil {
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

func (d *connectionDriver) Disconnect(ctx context.Context) error {
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
			return d.classifyCleanup(err)
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
		return d.classifyCleanup(err)
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

func (d *connectionDriver) classifyCleanup(err error) error {
	if classify := d.factory.config.ClassifyCleanup; classify != nil {
		return classify(err)
	}
	return nativeConnectionFailure(err, connectivity.FailureCleanup)
}

func (d *connectionDriver) reportNativeFailure(stage string, err error) {
	if report := d.factory.config.ReportFailure; report != nil {
		report(d.generation, stage, err)
	}
}
