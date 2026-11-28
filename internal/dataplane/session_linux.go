package dataplane

import (
	"context"
	"encoding/binary"
	"errors"
	clientv0 "github.com/endless-net/client/api/client/v0"
	"google.golang.org/protobuf/proto"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/endless-net/client/internal/underlay"

	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/google/uuid"
	"github.com/tailscale/wireguard-go/conn"
)

type LinuxSessionConfig struct {
	MTU, MaxPeers, MaxGrants, MaxProtected int
	ListenPort                             uint16
	Relay                                  RelayConfig
	ProbeInterval, RetryInterval           time.Duration
}

type LinuxSession struct {
	dnsObservation
	dns               linuxSessionDNS
	stopMu            sync.Mutex
	mu                sync.Mutex
	ctx               context.Context
	cancel            context.CancelFunc
	store             *state.Store
	ledger            state.LinuxNetworkLedger
	clock             connectivity.Clock
	lease             *connectivity.Lease
	config            LinuxSessionConfig
	resources         *TransportResources
	profile           string
	generation        uint64
	identity          linuxFilterIdentity
	receipt           state.LinuxNetworkReceipt
	advertisements    chan struct{}
	link              *linuxLink
	packets           *packetSwitch
	engine            *Engine
	supervisor        *transportSupervisor
	operation         *connectivity.OperationBudget
	current           connectivity.Map
	policy            linuxFilterPlan
	retained          []netip.Addr
	prepared, stopped bool
}

func NewLinuxSession(parent context.Context, store *state.Store, ledger state.LinuxNetworkLedger, clock connectivity.Clock, profile string, generation uint64, config LinuxSessionConfig, resources *TransportResources) (*LinuxSession, error) {
	if parent == nil || store == nil || resources == nil || resources.budget == nil ||
		config.MTU < 576 || config.MTU > 1500 || config.MaxPeers <= 0 || config.MaxGrants <= 0 ||
		config.MaxProtected <= 0 || config.ListenPort == 0 || config.ProbeInterval <= 0 ||
		config.RetryInterval <= 0 || config.Relay.Validate() != nil {
		return nil, ErrConfiguration
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, ErrConfiguration
	}
	name := "en" + strings.ReplaceAll(id.String(), "-", "")[:12]
	zone := binary.BigEndian.Uint16(id[6:8]) | 0x8000
	mark := binary.BigEndian.Uint32(id[12:16]) | 1
	ctx, cancel := context.WithCancel(parent)
	lease, err := connectivity.NewLease(ctx, clock, profile, generation)
	if err != nil {
		cancel()
		return nil, err
	}
	return &LinuxSession{
		ctx: ctx, cancel: cancel, store: store, ledger: ledger, clock: clock, lease: lease,
		config: config, resources: resources, profile: profile, generation: generation,
		identity:       linuxFilterIdentity{table: name, tunnel: name, owner: id.String(), zone: zone, mark: mark},
		receipt:        state.LinuxNetworkReceipt{Table: name, Owner: id.String()},
		advertisements: make(chan struct{}, 1),
	}, nil
}

func (*LinuxSession) String() string   { return "[Linux connectivity session]" }
func (*LinuxSession) GoString() string { return "[Linux connectivity session]" }

func (s *LinuxSession) Start(ctx context.Context, value connectivity.Map) error {
	return s.apply(ctx, value, true)
}

func (s *LinuxSession) Restore(ctx context.Context, value connectivity.Map) error {
	// A retained cursor is replay protection, not permission to recreate a link.
	return s.Start(ctx, value)
}

func (s *LinuxSession) Refresh(ctx context.Context, value connectivity.Map) error {
	return s.apply(ctx, value, false)
}

func (s *LinuxSession) apply(ctx context.Context, value connectivity.Map, first bool) (retErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || first != (s.engine == nil) {
		return ErrClosed
	}
	if first {
		operation, err := connectivity.OperationFromContext(ctx)
		if err != nil {
			return err
		}
		if err := s.lease.GuardOperation(operation); err != nil {
			return err
		}
		s.operation = operation
	}
	if !first && s.current.Cursor() == value.Cursor() {
		return connectivity.ErrReplay
	}
	s.lease.PausePackets(true)
	defer func() {
		if retErr != nil {
			s.lease.Revoke()
		}
	}()
	if value.Denied() {
		return connectivity.ErrDenied
	}
	profile, err := s.profileForMap(ctx, value, false)
	if err != nil {
		return err
	}
	defer clear(profile.Keys.WireGuardPrivate)
	if err := connectivity.PersistCursor(ctx, s.store, s.generation, value); err != nil {
		return err
	}
	if err := s.lease.Confirm(value); err != nil {
		return err
	}
	snapshot, err := value.Snapshot()
	if err != nil || len(snapshot.Peers) > s.config.MaxPeers {
		return ErrConfiguration
	}
	policy, err := prepareLinuxFilter(snapshot, s.config.MaxGrants, s.config.MaxProtected)
	if err != nil {
		return err
	}
	// Keep old destination guards through route removal. There is no interval
	// during which a protected address can fall through to a foreign route.
	peers := policy.protected[1:]
	s.retained = append(slices.Clone(s.policy.protected), policy.protected...)
	if first {
		if err := s.ledger.Prepare(ctx, s.receipt); err != nil {
			return err
		}
		s.prepared = true
		if err := s.filters(ctx, value, policy, true, linuxDeny); err != nil {
			return err
		}
		s.link, err = openLinuxLink(ctx, s.identity.tunnel, s.config.MTU)
		if err != nil {
			return err
		}
	} else if err := s.filters(ctx, value, policy, false, linuxOnline); err != nil {
		return err
	}
	if err := s.link.configure(ctx, policy.address, peers); err != nil {
		return err
	}
	if first {
		if err := s.startTransport(ctx, value, profile.Keys.WireGuardPrivate); err != nil {
			return err
		}
	} else {
		if err := s.packets.Update(value); err != nil {
			return err
		}
		if err := s.engine.Apply(ctx, value, profile.Keys.WireGuardPrivate); err != nil {
			return err
		}
		s.supervisor.Update(value)
	}
	var pending *clientv0.DnsApplication
	hasDNS := connectivity.MapDNSConfig(snapshot) != nil
	if hasDNS || s.dns.proxy != nil {
		pending = s.beginDNS(snapshot)
	}
	if err := s.applyDNS(ctx, value, snapshot); err != nil {
		s.finishDNS(pending, !hasDNS, err)
		return err
	}
	fenced, err := s.profileForMap(ctx, value, true)
	if err != nil {
		s.finishDNS(pending, !hasDNS, err)
		return err
	}
	clear(fenced.Keys.WireGuardPrivate)
	s.finishDNS(pending, !hasDNS, nil)
	if err := s.filters(ctx, value, policy, false, linuxOnline); err != nil {
		return err
	}
	s.current, s.policy = value, policy
	expected, ok := ctx.Value(sourceEpochKey{}).(uint64)
	if !ok {
		expected, _ = s.lease.SourceSignal()
	}
	if err := s.lease.EndSourceTransition(expected); errors.Is(err, connectivity.ErrSourceChanged) {
		return nil
	} else if err != nil {
		return err
	}
	s.lease.PausePackets(false)
	if !s.lease.Permissions().NewFlows {
		return connectivity.ErrExpired
	}
	return ctx.Err()
}

func (s *LinuxSession) startTransport(ctx context.Context, value connectivity.Map, key []byte) error {
	reservation, err := s.resources.budget.reserve(s.lease, "", enginePrimary)
	if err != nil {
		return err
	}
	s.packets, err = newPacketSwitch(s.ctx, s.link.device, s.lease, value, s.config.MTU, s.config.MaxPeers)
	if err != nil {
		reservation.release()
		return err
	}
	s.engine, err = NewEngine(wireGuardPort{s.packets.primary}, linuxUnderlayBind{conn.NewDefaultBind()}, s.config.MaxPeers, s.config.ListenPort, s.lease)
	if err != nil {
		reservation.release()
		return err
	}
	s.engine.reservation = reservation
	if err := s.engine.Apply(ctx, value, key); err != nil {
		return err
	}
	s.supervisor, err = newTransportSupervisor(s.ctx, value, s.lease, s.clock, s.profile, s.generation, key,
		s.resources.budget, s.packets, s.engine, supervisorConfig{reserveListener: reserveUDPListener,
			hostKernel: s.prepareHostCandidate, selectDirect: s.selectHostDirect, hintStore: s.store, networkContext: linuxTransportContext,
			relay: s.config.Relay, probeInterval: s.config.ProbeInterval, retry: s.config.RetryInterval,
			mtu: s.config.MTU, maxPeers: s.config.MaxPeers, advertise: func() {
				select {
				case s.advertisements <- struct{}{}:
				default:
				}
			},
		}, nil)
	return err
}

func (s *LinuxSession) profileForMap(ctx context.Context, value connectivity.Map, checkCursor bool) (state.Profile, error) {
	view, err := s.store.Load(ctx)
	if err != nil {
		return state.Profile{}, err
	}
	profile, ok := view.Profiles[s.profile]
	for id, p := range view.Profiles {
		clear(p.Keys.IdentityPrivate)
		if id != s.profile {
			clear(p.Keys.WireGuardPrivate)
		}
	}
	if !ok || view.ActiveProfileID != s.profile || profile.Connection == nil {
		clear(profile.Keys.WireGuardPrivate)
		return state.Profile{}, state.ErrStale
	}
	if err := value.CheckBinding(profile, s.generation); err != nil {
		clear(profile.Keys.WireGuardPrivate)
		return state.Profile{}, err
	}
	if checkCursor && (profile.Connection.MapCursor == nil || *profile.Connection.MapCursor != value.Cursor()) {
		clear(profile.Keys.WireGuardPrivate)
		return state.Profile{}, state.ErrStale
	}
	return profile, nil
}

// Stage the socket grant before its routes. Session locking serializes the
// firewall transaction and route changes with signed map and offline updates.
func (s *LinuxSession) installPeerProbe(ctx context.Context, socket *linuxProbeSocket, relay *RelayTransport) error {
	if !s.mu.TryLock() {
		return connectivity.ErrSourceChanged
	}
	defer s.mu.Unlock()
	if s.stopped || s.link == nil || socket == nil || socket.conn == nil {
		return ErrClosed
	}
	b := socket.binding
	if !relay.permitsProbeRoute(b.local.String(), b.peer.String()) {
		return ErrIPConflict
	}
	if _, exists := s.link.probes[b.peer]; exists {
		return ErrIPConflict
	}
	original := b.assignment
	epoch, changed := s.lease.SourceSignal()
	if original == nil || original.lease != s.lease || epoch != b.sourceEpoch {
		return ErrNoAuthority
	}
	sample, err := s.clock.Sample()
	if err != nil {
		return err
	}
	a, err := newCandidateAssignment(s.current, s.lease, original.peer.ID, original.offer.Profile.ID, sample.Wall)
	if err != nil || !a.keepPair(original.localEndpoint, original.peerEndpoint) || !original.sameProbe(a) {
		return ErrNoAuthority
	}
	iface, err := net.InterfaceByIndex(socket.index)
	if err != nil || iface.Name != b.device {
		return ErrAdapterOwnership
	}
	if _, err := ownedLinuxPeer(ctx, iface, s.identity.owner); err != nil {
		return err
	}
	policy := s.policy
	policy.probes = []linuxProbeBinding{b}
	if err = s.filters(ctx, s.current, policy, false, linuxOnline); err == nil {
		err = s.link.addProbeRoute(ctx, b)
	}
	if err == nil && canFinishPreparation(ctx, s.clock, a, changed) && relay.permitsProbeRoute(b.local.String(), b.peer.String()) {
		return nil
	}
	if err == nil {
		err = ErrNoAuthority
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	// Remove any route first while both addresses are still protected. A
	// failure leaves the session fenced for owner-ledger recovery.
	if current, ok := s.link.probes[b.peer]; ok && current == b {
		if removeErr := s.link.removeProbeRoute(cleanup, b); removeErr != nil {
			s.lease.Revoke()
			return errors.Join(err, removeErr, s.filters(cleanup, s.current, s.policy, false, linuxDeny))
		}
	}
	if restoreErr := s.filters(cleanup, s.current, s.policy, false, s.peerFilterMode()); restoreErr != nil {
		s.lease.Revoke()
		return errors.Join(err, restoreErr, s.filters(cleanup, s.current, s.policy, false, linuxDeny))
	}
	return err
}

func (s *LinuxSession) peerFilterMode() linuxFilterMode {
	perm := s.lease.Permissions()
	if !perm.ExistingFlows {
		return linuxDeny
	}
	if !perm.NewFlows {
		return linuxExisting
	}
	return linuxOnline
}

func (s *LinuxSession) filters(ctx context.Context, value connectivity.Map, policy linuxFilterPlan, install bool, mode linuxFilterMode) error {
	handle, err := linuxNFTLookup(ctx, s.receipt)
	if err != nil || (handle == 0) != install {
		return ErrPolicyOwnership
	}
	var authorityErr error
	retained := s.retained
	if s.link != nil && len(s.link.probes) > 0 {
		snapshot, snapshotErr := value.Snapshot()
		sample, clockErr := s.clock.Sample()
		authorityErr = errors.Join(snapshotErr, clockErr)
		if mode == linuxDeny || authorityErr != nil {
			retained = slices.Clone(retained)
			for _, binding := range s.link.probes {
				retained = append(retained, binding.local, binding.peer)
			}
			mode = linuxDeny
		} else {
			retained = s.link.applyPeerBindings(&policy, retained, value, snapshot, s.lease, sample.Wall)
		}
	}
	transaction, err := policy.transaction(s.identity, install, mode, retained)
	if err != nil {
		return err
	}
	return errors.Join(authorityErr, linuxNFTApply(ctx, transaction))
}

// Keep installed addresses protected until their routes are removed. Restore
// per-device permissions only for the same pair in the newly verified map.
func (l *linuxLink) applyPeerBindings(policy *linuxFilterPlan, retained []netip.Addr, value connectivity.Map, snapshot coordinator.NetworkMapSnapshot, lease *connectivity.Lease, now time.Time) []netip.Addr {
	peers := make(map[string]coordinator.Peer, len(snapshot.Peers))
	for _, peer := range snapshot.Peers {
		peers[peer.ID] = peer
	}
	retained = slices.Clone(retained)
	epoch, _ := lease.SourceSignal()
	for _, binding := range l.probes {
		retained = append(retained, binding.local, binding.peer)
		original := binding.assignment
		if original == nil || original.lease != lease || binding.sourceEpoch != epoch {
			continue
		}
		peer, ok := peers[original.peer.ID]
		if !ok {
			continue
		}
		current, err := signedPeerAssignment(value, lease, snapshot, peer, original.offer.Profile.ID, now)
		if err != nil || !current.keepPair(original.localEndpoint, original.peerEndpoint) || !original.sameProbe(current) {
			continue
		}
		policy.probes = append(policy.probes, binding)
		_, ip, err := current.addresses()
		if iface, ok := l.direct[ip]; err == nil && ok && iface.Name == binding.device {
			if policy.direct == nil {
				policy.direct = make(map[netip.Addr]string)
			}
			policy.direct[ip] = iface.Name
		}
	}
	return retained
}

func (s *LinuxSession) Offline(ctx context.Context) error {
	s.lease.Unavailable()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine == nil {
		return ErrClosed
	}
	if err := s.filters(ctx, s.current, s.policy, false, linuxExisting); err != nil {
		s.lease.Revoke()
		return err
	}
	expected, ok := ctx.Value(sourceEpochKey{}).(uint64)
	if ok {
		if err := s.lease.EndSourceTransition(expected); err != nil && !errors.Is(err, connectivity.ErrSourceChanged) {
			return err
		}
	}
	return nil
}

func (s *LinuxSession) Stop(ctx context.Context) error {
	s.lease.Revoke()
	s.cancel()
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	s.mu.Lock()
	s.stopped = true
	supervisor := s.supervisor
	s.supervisor = nil
	s.mu.Unlock()
	if supervisor != nil {
		supervisor.Close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine != nil {
		s.engine.Close()
		s.engine = nil
	}
	var closeErr error
	if s.packets != nil {
		closeErr = s.packets.Close()
		s.packets, s.link = nil, nil
	} else if s.link != nil {
		closeErr = s.link.device.Close()
		s.link = nil
	}
	if prior := s.dnsResult.Load(); prior != nil {
		// Removing the owned TUN also removes its resolved link configuration.
		interfaces, err := net.Interfaces()
		for _, iface := range interfaces {
			if iface.Name == s.identity.tunnel {
				err = ErrAdapterOwnership
				break
			}
		}
		closeErr = errors.Join(closeErr, err)
		if closeErr == nil {
			closeErr = waitResolvedRemoval(ctx, s.receipt)
			if closeErr == nil {
				s.dns.stop()
			}
		}
		s.finishDNS(prior, true, closeErr)
	}
	if s.prepared {
		if err := CleanLinuxNetwork(ctx, s.ledger); err != nil {
			return err
		}
		s.prepared = false
	}
	return closeErr
}

func (s *LinuxSession) CheckReady(ctx context.Context) error {
	operation, err := connectivity.OperationFromContext(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.engine == nil {
		return ErrClosed
	}
	if operation != s.operation || !operation.Active() {
		return ErrNoAuthority
	}
	if expected, ok := ctx.Value(sourceEpochKey{}).(uint64); ok {
		if actual, _ := s.lease.SourceSignal(); actual != expected {
			return connectivity.ErrSourceChanged
		}
	}
	if !s.lease.Permissions().NewFlows {
		return ErrNoAuthority
	}
	if err := s.packets.Err(); err != nil {
		return err
	}
	profile, err := s.profileForMap(ctx, s.current, true)
	clear(profile.Keys.WireGuardPrivate)
	if err != nil {
		return err
	}
	if err := s.link.verify(ctx); err != nil {
		return err
	}
	if s.dns.proxy != nil {
		if err := checkResolvedStub(); err != nil {
			return err
		}
		if err := checkResolvedDomains(ctx, s.link.index, s.dns.domains); err != nil {
			return err
		}
		current, err := readResolvedLink(ctx, s.link.index)
		if err != nil || current != s.dns.installed {
			return errors.Join(ErrIPConflict, err)
		}
	}
	if handle, err := linuxNFTLookup(ctx, s.receipt); err != nil || handle == 0 {
		return ErrPolicyOwnership
	}
	_, err = s.engine.BoundPort()
	return err
}

func (s *LinuxSession) ProbePaths(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine == nil {
		return ErrClosed
	}
	if err := s.link.verify(ctx); err != nil {
		s.lease.Revoke()
		return err
	}
	if handle, err := linuxNFTLookup(ctx, s.receipt); err != nil || handle == 0 {
		s.lease.Revoke()
		return ErrPolicyOwnership
	}
	return s.engine.ProbeDirect(ctx)
}

func (s *LinuxSession) BoundPort() (uint16, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine == nil {
		return 0, ErrClosed
	}
	return s.engine.BoundPort()
}

func (s *LinuxSession) TransportAdvertisement(addr netip.Addr) (coordinator.TransportAdvertisement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.supervisor == nil {
		return coordinator.TransportAdvertisement{}, ErrClosed
	}
	return s.supervisor.advertisement(addr), nil
}

func (s *LinuxSession) AdvertisementSignal() <-chan struct{} { return s.advertisements }
func (s *LinuxSession) BlockSource() uint64                  { return s.lease.BlockSource() }
func (s *LinuxSession) RevokeSource()                        { s.lease.Revoke(); s.cancel() }

func (s *LinuxSession) ConnectionLocal() connectivity.LocalObservation {
	if !s.mu.TryLock() {
		return connectivity.LocalObservation{Permission: s.lease.Permissions(), DNS: proto.CloneOf(s.dnsResult.Load())}
	}
	defer s.mu.Unlock()
	local := transportObservation(s.current, s.lease, s.engine, s.packets, s.supervisor, s.lease.Permissions().NewFlows)
	local.DNS = proto.CloneOf(s.dnsResult.Load())
	return local
}

// WireGuard reopens its UDP sockets when the bind changes. Mark every new
// socket before the engine can send a handshake or expose a receiver.
type linuxUnderlayBind struct{ conn.Bind }

func (b linuxUnderlayBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	receivers, actual, err := b.Bind.Open(port)
	if err != nil {
		return nil, 0, err
	}
	if err := b.SetMark(underlay.Mark); err != nil {
		return nil, 0, errors.Join(err, b.Close())
	}
	return receivers, actual, nil
}
