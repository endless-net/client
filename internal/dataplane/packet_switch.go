package dataplane

import (
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wireguard-go/tun"
)

// packetBinding is the immutable plaintext delivery scope of a cipher. A
// signature renewal may retain it; changed identities, addresses or intents may
// not. It is not an application grant: Windows WFP remains the stateful ACL.
type packetBinding struct {
	account, network, localID, localKey, peerID, peerKey string
	local, peer                                          netip.Addr
	localGeneration, peerGeneration                      uint64
}

// packetSwitch is the sole native TUN reader. All engines, including primary
// WireGuard, read virtual ports. Closing an engine cannot close the shared TUN.
// The native session must remove dynamic permits before calling Close.
type packetSwitch struct {
	flows     atomic.Pointer[connectivity.FlowMonitor]
	mu        sync.Mutex
	writeMu   sync.Mutex
	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	native    tun.Device
	primary   *packetPort
	lease     *connectivity.Lease
	maxPeers  int
	mapValue  connectivity.Map
	packetPolicy
	fragments          tcpFragments
	routes             map[string]*peerRoute
	nextEpoch          uint64
	routeEpoch         map[string]uint64
	switchingResources bool
	closed             bool
	err                error
	closeErr           error
	done               chan struct{}
}

func newPacketSwitch(parent context.Context, native tun.Device, lease *connectivity.Lease, value connectivity.Map, mtu, maxPeers int) (*packetSwitch, error) {
	if parent == nil || parent.Err() != nil || native == nil || lease == nil || maxPeers <= 0 {
		return nil, ErrConfiguration
	}
	ctx, cancel := context.WithCancel(parent)
	s := &packetSwitch{ctx: ctx, cancel: cancel, native: native, lease: lease, maxPeers: maxPeers, routes: make(map[string]*peerRoute), done: make(chan struct{})}
	if err := s.Update(value); err != nil {
		cancel()
		return nil, err // native ownership stays with the caller on failure
	}
	port, err := newPacketPort(ctx, mtu, lease.Permissions, s.primaryIngress)
	if err != nil {
		cancel()
		return nil, err
	}
	s.primary = port
	go s.run()
	return s, nil
}

// grantWindows indexes a verified direction by protocol and port. A present
// zero deadline is permanent. Every grant in the direction has the same
// destination (the verifier enforces the exact local or peer /32).
type grantWindows map[coordinator.ACLPort]time.Time

func directionWindows(grants []coordinator.ACLGrant) grantWindows {
	windows := make(grantWindows)
	for _, grant := range grants {
		var end time.Time
		if grant.ValidUntil != nil {
			end = *grant.ValidUntil
		}
		for _, port := range grant.AllowedPorts {
			previous, ok := windows[port]
			if !ok || !previous.IsZero() && (end.IsZero() || end.After(previous)) {
				windows[port] = end
			}
		}
	}
	return windows
}

func (w grantWindows) match(port coordinator.ACLPort) (time.Time, bool) {
	exact, ok := w[port]
	all, anyPort := w[coordinator.ACLPort{Protocol: coordinator.ProtocolAll}]
	if !anyPort || ok && (exact.IsZero() || !all.IsZero() && exact.After(all)) {
		return exact, ok
	}
	return all, true
}

type packetGrants struct {
	egress, ingress grantWindows
	firstEnd        time.Time
}

func windowsForPeer(peer coordinator.Peer) packetGrants {
	g := packetGrants{egress: directionWindows(peer.ACLGrants), ingress: directionWindows(peer.IngressACLGrants)}
	for _, direction := range []grantWindows{g.egress, g.ingress} {
		for port := range direction {
			end, _ := direction.match(port)
			g.firstEnd = earlierDeadline(g.firstEnd, end)
		}
	}
	return g
}

func earlierDeadline(a, b time.Time) time.Time {
	if !b.IsZero() && (a.IsZero() || b.Before(a)) {
		return b
	}
	return a
}

// Native filtering verifies flow state. A TCP tuple also needs a matching
// signed grant at the packet boundary, including after a policy refresh.
func (g packetGrants) peerPacketWindow(raw []byte, outbound bool) (time.Time, bool) {
	if len(raw) < 20 {
		return g.firstEnd, true
	}
	if raw[9] != 6 && g.firstEnd.IsZero() {
		return time.Time{}, true
	}
	header := int(raw[0]&15) * 4
	if raw[0]>>4 != 4 || header < 20 || header > len(raw) || int(binary.BigEndian.Uint16(raw[2:4])) != len(raw) {
		return time.Time{}, false
	}
	protocol, destination, ok := outboundFlowPort(raw)
	if !ok {
		fragment := binary.BigEndian.Uint16(raw[6:8])
		switch raw[9] {
		case 1:
			if fragment & ^uint16(0x4000) == 0 && len(raw)-header >= 8 && raw[header+1] == 0 {
				kind := raw[header]
				if kind == 8 || kind == 0 {
					direction := g.egress
					if outbound != (kind == 8) {
						direction = g.ingress
					}
					if end, ok := direction[coordinator.ACLPort{Protocol: coordinator.ProtocolAll}]; ok {
						return end, true
					}
				}
			}
		case 6:
			if fragment != 0x2000 || (len(raw)-header)%8 != 0 || !completeTCPHeader(raw[header:]) {
				return time.Time{}, false
			}
			protocol, destination = "tcp", binary.BigEndian.Uint16(raw[header+2:header+4])
		}
		if protocol == "" {
			return g.firstEnd, true
		}
	}
	h := raw[header:]
	var flags byte
	if protocol == "tcp" {
		flags = h[13]
	}
	return g.tupleWindow(protocol, binary.BigEndian.Uint16(h), destination, flags, outbound)
}

func (g packetGrants) tupleWindow(protocol string, source, destination uint16, flags byte, outbound bool) (time.Time, bool) {
	forward, reverse := g.egress, g.ingress
	if !outbound {
		forward, reverse = reverse, forward
	}
	origin, canOpen := forward.match(coordinator.ACLPort{Protocol: protocol, Port: int(destination)})
	response, canReply := reverse.match(coordinator.ACLPort{Protocol: protocol, Port: int(source)})
	if protocol == "tcp" && flags&0x02 != 0 {
		if flags&0x10 != 0 {
			return response, canReply
		}
		return origin, canOpen
	}
	return earlierDeadline(origin, response), protocol != "tcp" || canOpen || canReply
}

type resourcePacketRule struct {
	exitOverlay netip.Prefix
	end         time.Time
	peer        string
	ports       map[coordinator.ACLPort]bool
}

type resourcePacketRules map[netip.Prefix]resourcePacketRule

func (rules resourcePacketRules) lookup(ip netip.Addr) resourcePacketRule {
	if len(rules) != 0 && ip.Is4() {
		for bits := 32; bits > 0; bits-- {
			if r, ok := rules[netip.PrefixFrom(ip, bits).Masked()]; ok {
				return r
			}
		}
		return rules[netip.PrefixFrom(netip.IPv4Unspecified(), 0)].internetRule(ip)
	}
	return resourcePacketRule{}
}

// The native filter checks conntrack direction. Here a reply must also come
// from the selected subnet and carry the signed service port as its source.
func (r resourcePacketRule) permits(raw []byte, outbound bool) bool {
	if r.peer == "" {
		return false
	}
	if r.ports[coordinator.ACLPort{Protocol: coordinator.ProtocolAll}] {
		return true
	}
	protocol, port, ok := outboundFlowPort(raw)
	if !ok {
		return false
	}
	if !outbound {
		offset := int(raw[0]&15) * 4
		port = binary.BigEndian.Uint16(raw[offset:])
	}
	return r.ports[coordinator.ACLPort{Protocol: protocol, Port: int(port)}]
}

func recipientPacketRules(m coordinator.NetworkMapSnapshot) (resourcePacketRules, map[string]bool, error) {
	return recipientRulesWithHealth(m, nil)
}

func recipientRulesWithHealth(m coordinator.NetworkMapSnapshot, reachable func(coordinator.ServiceHost) bool) (resourcePacketRules, map[string]bool, error) {
	paths, err := eligibleResourcePaths(m, reachable)
	if err != nil {
		return nil, nil, err
	}
	return recipientRulesForPaths(m, paths)
}

func recipientRulesForPaths(m coordinator.NetworkMapSnapshot, paths []connectivity.ResourcePath) (resourcePacketRules, map[string]bool, error) {
	rules := make(resourcePacketRules)
	routers := make(map[string]bool)
	overlay, _ := netip.ParsePrefix(m.Network.CIDR)
	local := coordinator.ServiceHost{NodeID: m.Node.ID, PublicKey: m.Node.PublicKey}
	apps := make(map[string]coordinator.Application, len(m.Network.Applications))
	inactive := m
	inactive.Network.Applications = nil
	for _, app := range m.Network.Applications {
		// A missing path for a more specific destination must shadow any
		// active path for a covering prefix.
		if len(app.Routes) > 0 {
			prefixes, err := connectivity.AssignedPrefixes(app, overlay)
			if err != nil {
				return nil, nil, ErrConfiguration
			}
			for _, p := range prefixes {
				rules[p] = resourcePacketRule{}
			}
			if !slices.Contains(app.Connectors, local) {
				for _, route := range app.Routes {
					if !route.ExpiresAt.Before(m.MapSignature.ExpiresAt) {
						routers[route.Connector.NodeID] = true
					}
				}
			}
		}
		apps[app.ID] = app
		if len(app.Routes) == 0 {
			inactive.Network.Applications = append(inactive.Network.Applications, app)
		}
	}
	for _, path := range paths {
		id := path.Route.Connector.NodeID
		r := rules[path.Prefix]
		if r.ports == nil {
			r = resourcePacketRule{end: m.MapSignature.ExpiresAt, peer: id, ports: make(map[coordinator.ACLPort]bool)}
			if path.Prefix.Bits() == 0 {
				r.exitOverlay = overlay
			}
		}
		for _, port := range apps[path.ApplicationID].AllowedPorts {
			r.ports[coordinator.ACLPort{Protocol: port.Protocol, Port: int(port.Port)}] = true
		}
		rules[path.Prefix] = r
	}
	if len(inactive.Network.Applications) > 0 {
		prefixes, err := connectivity.UnavailableExitPrefixes(inactive, len(inactive.Network.Applications))
		if err != nil {
			return nil, nil, ErrConfiguration
		}
		for _, prefix := range prefixes {
			rules[prefix] = resourcePacketRule{}
		}
	}
	return rules, routers, nil
}

type packetPolicy struct {
	transit   routerPacketRules
	resources resourcePacketRules
	pairs     map[string]packetBinding
	byIP      map[netip.Addr]string
	grants    map[string]packetGrants
}

func packetBindings(value connectivity.Map, maxPeers int) (packetPolicy, error) {
	m, err := value.Snapshot()
	if err != nil || value.Denied() || len(m.Peers) > maxPeers {
		return packetPolicy{}, ErrNoAuthority
	}
	local, err := netip.ParseAddr(m.Node.AssignedIP)
	if err != nil || !local.Is4() {
		return packetPolicy{}, ErrConfiguration
	}
	pairs := make(map[string]packetBinding, len(m.Peers))
	byIP := make(map[netip.Addr]string, len(m.Peers))
	seenIP := map[netip.Addr]bool{local: true}
	grants := make(map[string]packetGrants, len(m.Peers))
	resources, routers, err := recipientPacketRules(m)
	if err != nil {
		return packetPolicy{}, err
	}
	transit, subjects, err := routerPacketPolicy(m)
	if err != nil {
		return packetPolicy{}, err
	}
	for _, peer := range m.Peers {
		if !subjects[peer.ID] && !routers[peer.ID] && !pairGranted(peer) {
			continue
		}
		if peer.ID == "" || pairs[peer.ID].peerID != "" || len(peer.AllowedIPs) != 1 {
			return packetPolicy{}, ErrConfiguration
		}
		prefix, err := netip.ParsePrefix(peer.AllowedIPs[0])
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 || seenIP[prefix.Addr()] {
			return packetPolicy{}, ErrConfiguration
		}
		seenIP[prefix.Addr()] = true
		b := packetBinding{account: m.Network.AccountID, network: m.Network.ID, localID: m.Node.ID, localKey: m.Node.PublicKey, peerID: peer.ID, peerKey: peer.PublicKey, local: local, peer: prefix.Addr(), localGeneration: value.IntentGeneration()}
		if peer.Transport != nil {
			b.peerGeneration = peer.Transport.PeerAdvertisement.IntentGeneration
		}
		pairs[peer.ID] = b
		if pairGranted(peer) {
			byIP[b.peer] = peer.ID
		}
		grants[peer.ID] = windowsForPeer(peer)
	}
	return packetPolicy{transit: transit, resources: resources, pairs: pairs, byIP: byIP, grants: grants}, nil
}

// Update is called while the native source/lease pause is held. No old accepted
// map may send or deliver during the gap between Lease.Confirm and this update.
func (s *packetSwitch) Update(value connectivity.Map) error {
	return s.updatePolicy(value, nil, false)
}

func (s *packetSwitch) updatePaths(value connectivity.Map, paths []connectivity.ResourcePath) error {
	return s.updatePolicy(value, paths, true)
}

func (s *packetSwitch) updatePolicy(value connectivity.Map, paths []connectivity.ResourcePath, observed bool) error {
	if !s.lease.MatchesMap(value) || value.Denied() {
		return ErrNoAuthority
	}
	policy, err := packetBindings(value, s.maxPeers)
	if err != nil {
		return err
	}
	if observed {
		m, err := value.Snapshot()
		if err != nil {
			return err
		}
		policy.resources, _, err = recipientRulesForPaths(m, paths)
		if err != nil {
			return err
		}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	var removed []*peerRoute
	for id, route := range s.routes {
		if previous, ok := s.pairs[id]; !ok || previous != policy.pairs[id] {
			delete(s.routes, id)
			removed = append(removed, route)
		}
	}
	revised := value.Cursor().Revision != s.mapValue.Cursor().Revision && !s.lease.AllowsPathMap(s.mapValue)
	revised = revised || !reflect.DeepEqual(s.resources, policy.resources) || !reflect.DeepEqual(s.transit, policy.transit)
	epochs := make(map[string]uint64, len(policy.pairs))
	for id, binding := range policy.pairs {
		// Time-dependent grants can change on renewal without a revision bump.
		if revised || !s.lease.PeerScopeCurrent(s.mapValue, id) || binding != s.pairs[id] || !reflect.DeepEqual(s.grants[id], policy.grants[id]) {
			s.nextEpoch++
			epochs[id] = s.nextEpoch
		} else {
			epochs[id] = s.routeEpoch[id]
		}
	}
	for key, fragment := range s.fragments {
		if fragment.firstLength == 0 || fragment.epoch == epochs[key.peer] {
			continue
		}
		end, ok := policy.grants[key.peer].tupleWindow("tcp", fragment.source, fragment.destination, fragment.flags, key.outbound)
		if !ok || !end.Equal(fragment.end) || s.pairs[key.peer] != policy.pairs[key.peer] {
			fragment.firstLength = 0
		}
		fragment.epoch = epochs[key.peer]
		s.fragments[key] = fragment
	}
	s.mapValue, s.packetPolicy, s.routeEpoch = value, policy, epochs
	s.mu.Unlock()
	for _, route := range removed {
		route.Invalidate()
	}
	return nil
}

func (s *packetSwitch) admittedLocked() bool {
	return !s.closed && !s.switchingResources && s.ctx.Err() == nil && s.lease.AllowsPathMap(s.mapValue) && s.lease.Permissions().ExistingFlows
}

// SetRoute transfers only output choice, never resource ownership. The peer
// supervisor retains/tears down ciphers outside the packet switch's mutex.
func (s *packetSwitch) SetRoute(peer string, route *peerRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, exists := s.pairs[peer]
	if !exists || !s.admittedLocked() {
		return ErrNoAuthority
	}
	if route != nil {
		route.mu.Lock()
		r := route.relay
		closed := route.closed
		route.mu.Unlock()
		if closed || r == nil || r.bind.lease != s.lease {
			return ErrNoAuthority
		}
		r.bind.mu.Lock()
		local, remote, account, value := r.bind.local, r.bind.peer, r.bind.account, r.bind.mapValue
		r.bind.mu.Unlock()
		var peerGeneration uint64
		if remote.Transport != nil {
			peerGeneration = remote.Transport.PeerAdvertisement.IntentGeneration
		}
		if !s.lease.MatchesMap(value) || local.ID != b.localID || local.PublicKey != b.localKey || local.AssignedIP != b.local.String() || remote.ID != b.peerID || remote.PublicKey != b.peerKey || len(remote.AllowedIPs) != 1 || remote.AllowedIPs[0] != b.peer.String()+"/32" || account != b.account || r.bind.mux.transport.network != b.network || value.IntentGeneration() != b.localGeneration || peerGeneration != b.peerGeneration {
			return ErrNoAuthority
		}
		if s.routes[peer] == route {
			return nil
		}
		s.routes[peer] = route
	} else {
		if s.routes[peer] == nil {
			return nil
		}
		delete(s.routes, peer)
	}
	// Only this pair changed output path. Preserve queue guards for peers
	// sharing the same switch, including their primary-engine packets.
	s.nextEpoch++
	s.routeEpoch[peer] = s.nextEpoch
	return nil
}

func packetAddress(raw []byte, offset int) (netip.Addr, error) {
	if len(raw) < 20 || raw[0]>>4 != 4 || int(raw[0]&15)*4 < 20 || int(raw[0]&15)*4 > len(raw) || int(binary.BigEndian.Uint16(raw[2:4])) != len(raw) {
		return netip.Addr{}, ErrPacketIO
	}
	return netip.AddrFrom4([4]byte(raw[offset : offset+4])), nil
}

func (s *packetSwitch) routeCurrent(b packetBinding, route *peerRoute, epoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lease.AllowsPeerPath(s.mapValue, b.peerID) && s.admittedLocked() && s.routeEpoch[b.peerID] == epoch && s.pairs[b.peerID] == b && s.routes[b.peerID] == route
}

func (s *packetSwitch) send(raw []byte) error {
	destination, err := packetAddress(raw, 16)
	if err != nil {
		return err
	}
	source, err := packetAddress(raw, 12)
	if err != nil {
		return err
	}
	s.mu.Lock()
	b, exists := s.pairs[s.byIP[destination]]
	rule := s.resources.lookup(destination)
	resource := !exists && rule.permits(raw, true)
	if resource {
		b, exists = s.pairs[rule.peer]
	}
	sourceExpected := b.local
	if transit := s.transit.lookup(source, destination); transit.permits(raw, false) {
		rule, resource = transit, true
		b, exists = s.pairs[transit.peer]
		exists = exists && b.peer == destination
		sourceExpected = source
	}
	route, epoch, observedMap := s.routes[b.peerID], s.routeEpoch[b.peerID], s.mapValue
	allowed := exists && s.lease.AllowsPeerPath(s.mapValue, b.peerID) && s.admittedLocked()
	if !allowed || applicationForPair(raw, sourceExpected, destination) != nil {
		s.mu.Unlock()
		return ErrNoAuthority
	}
	end := rule.end
	if !resource {
		end, allowed = s.fragmentWindow(b.peerID, raw, true)
	}
	s.mu.Unlock()
	if !allowed {
		return ErrNoAuthority
	}
	withinDeadline := func() bool { return end.IsZero() || s.lease.GrantDeadlineCurrent(observedMap, end) }
	if !withinDeadline() {
		return ErrNoAuthority
	}
	current := func() bool { return s.routeCurrent(b, route, epoch) && withinDeadline() }
	// The native interface delivered this outbound application packet after OS
	// filtering. This observation says nothing about remote delivery.
	if monitor := s.flows.Load(); !resource && monitor != nil && monitor.Collecting() {
		protocol, port, _ := outboundFlowPort(raw)
		monitor.ObserveEgress(observedMap, b.local, b.peer, protocol, port, uint64(len(raw)))
	}
	if route != nil {
		// No fallback/retry of this same packet, including when the route has
		// temporarily lost all confirmed choices.
		return route.sendChecked(raw, current)
	}
	return s.primary.enqueueChecked(raw, false, current)
}

func (s *packetSwitch) run() {
	defer close(s.done)
	slab := make([]byte, 65535+2*tun.ReadPacketSpacing)
	defer clear(slab)
	packets := make([]tun.ReadPacket, 1)
	for s.ctx.Err() == nil {
		n, err := s.native.Read(slab, packets)
		if n < 0 || n > len(packets) {
			err = ErrPacketIO
			n = 0
		}
		for i := 0; i < n; i++ {
			p := packets[i]
			if p.Offset < 0 || p.Size <= 0 || p.Offset > len(slab) || p.Size > len(slab)-p.Offset {
				err = ErrPacketIO
				break
			}
			_ = s.send(slab[p.Offset : p.Offset+p.Size])
			clear(slab[p.Offset : p.Offset+p.Size])
		}
		if err != nil || n == 0 {
			s.mu.Lock()
			if !s.closed && s.ctx.Err() == nil {
				s.err = ErrPacketIO
			}
			s.mu.Unlock()
			_ = s.primary.Close()
			return
		}
	}
}

func (s *packetSwitch) ingress(b packetBinding, raw []byte) error {
	source, err := packetAddress(raw, 12)
	if err != nil {
		return ErrNoAuthority
	}
	destination, err := packetAddress(raw, 16)
	if err != nil || applicationForPair(raw, source, destination) != nil {
		return ErrNoAuthority
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	allowed := s.lease.AllowsPeerPath(s.mapValue, b.peerID) && s.admittedLocked() && s.pairs[b.peerID] == b
	var end time.Time
	if destination != b.local {
		r := s.transit.lookup(destination, source)
		allowed = allowed && source == b.peer && r.peer == b.peerID && r.permits(raw, true)
		end = r.end
	} else if source == b.peer {
		allowed = allowed && s.byIP[source] == b.peerID
		var matched bool
		if allowed {
			end, matched = s.fragmentWindow(b.peerID, raw, false)
		}
		allowed = allowed && matched
	} else {
		r := s.resources.lookup(source)
		allowed = allowed && r.peer == b.peerID && r.permits(raw, false)
		end = r.end
	}
	if allowed && !end.IsZero() {
		allowed = s.lease.GrantDeadlineCurrent(s.mapValue, end)
	}
	s.mu.Unlock()
	if !allowed {
		return ErrNoAuthority
	}
	// Every cipher has already applied AEAD/replay/source checks. This common
	// boundary checks current tuple/lease; native WFP classifies app flows.
	count, err := s.native.Write([][]byte{raw}, 0)
	if err != nil || count != 1 {
		return ErrPacketIO
	}
	s.mu.Lock()
	allowed = s.lease.AllowsPeerPath(s.mapValue, b.peerID) && s.admittedLocked() && s.pairs[b.peerID] == b
	s.mu.Unlock()
	if !allowed {
		return ErrNoAuthority
	}
	return nil
}

func (s *packetSwitch) primaryIngress(raw []byte) error {
	source, err := packetAddress(raw, 12)
	if err != nil {
		return nil // consume malformed plaintext; never send it to native TUN
	}
	s.mu.Lock()
	destination, _ := packetAddress(raw, 16)
	b, exists := s.pairs[s.byIP[source]]
	if !exists {
		b, exists = s.pairs[s.transit.lookup(destination, source).peer]
	}
	if !exists {
		b, exists = s.pairs[s.resources.lookup(source).peer]
	}
	s.mu.Unlock()
	if exists {
		_ = s.ingress(b, raw)
	}
	return nil
}

// Sink freezes the identity/IP/intent scope for an isolated peer cipher. It may
// accept qualified receive-only traffic independently of current output choice.
func (s *packetSwitch) Sink(value connectivity.Map, peer string) (func([]byte) error, error) {
	s.mu.Lock()
	b, exists := s.pairs[peer]
	allowed := exists && s.lease.MatchesMap(value) && value.Cursor() == s.mapValue.Cursor() && !s.closed
	s.mu.Unlock()
	if !allowed {
		return nil, ErrNoAuthority
	}
	cursor := value.Cursor()
	return func(raw []byte) error {
		if !s.lease.PeerBindingCurrent(cursor, peer) {
			return ErrNoAuthority
		}
		return s.ingress(b, raw)
	}, nil
}

func (s *packetSwitch) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *packetSwitch) outputChoices() map[string]peerRouteKind {
	s.mu.Lock()
	routes := make(map[string]*peerRoute, len(s.routes))
	for id, route := range s.routes {
		routes[id] = route
	}
	admitted := s.admittedLocked()
	s.mu.Unlock()
	result := make(map[string]peerRouteKind, len(routes))
	for id, route := range routes {
		if admitted {
			result[id] = route.Current()
		} else {
			result[id] = peerRouteNone
		}
	}
	return result
}

func (s *packetSwitch) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.cancel()
		_ = s.primary.Close()
		s.writeMu.Lock()
		s.closeErr = s.native.Close() // unblocks the one reader
		s.writeMu.Unlock()
		<-s.done
	})
	return s.closeErr
}

// outboundFlowPort handles complete IPv4 TCP/UDP headers only. Fragment
// payload and native control messages cannot supply application ports.
func outboundFlowPort(raw []byte) (string, uint16, bool) {
	if len(raw) < 20 || raw[0]>>4 != 4 || binary.BigEndian.Uint16(raw[6:8])&0x3fff != 0 {
		return "", 0, false
	}
	offset := int(raw[0]&15) * 4
	if offset < 20 || offset > len(raw) {
		return "", 0, false
	}
	h := raw[offset:]
	switch raw[9] {
	case 6:
		if !completeTCPHeader(h) {
			return "", 0, false
		}
		return "tcp", binary.BigEndian.Uint16(h[2:4]), true
	case 17:
		if len(h) < 8 || int(binary.BigEndian.Uint16(h[4:6])) != len(h) {
			return "", 0, false
		}
		return "udp", binary.BigEndian.Uint16(h[2:4]), true
	default:
		return "", 0, false
	}
}

func completeTCPHeader(h []byte) bool {
	return len(h) >= 20 && h[12]>>4 >= 5 && int(h[12]>>4)*4 <= len(h)
}

// Fence delivery while WireGuard drains the old peer queues and changes the
// cryptographic prefix owner. Probe traffic on isolated channels stays live.
func (s *packetSwitch) replaceResourceRules(value connectivity.Map, rules resourcePacketRules, apply func() error) error {
	changed, err := s.beginResourceSwitch(value, rules)
	if err != nil || !changed {
		return err
	}
	if err := apply(); err != nil {
		s.lease.Revoke()
		// This is a terminal native failure, including a timed-out UAPI write.
		// ProbePaths callers treat a plain observation timeout as retryable.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrNoAuthority) {
			return ErrEngine
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resources = rules
	s.switchingResources = false
	return nil
}

// Drain the current TUN write and invalidate its route epoch before reconfiguring WireGuard.
func (s *packetSwitch) beginResourceSwitch(value connectivity.Map, rules resourcePacketRules) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.admittedLocked() || s.mapValue.Cursor() != value.Cursor() {
		return false, ErrNoAuthority
	}
	if reflect.DeepEqual(s.resources, rules) {
		return false, nil
	}
	s.switchingResources = true
	for id := range s.routeEpoch {
		s.nextEpoch++
		s.routeEpoch[id] = s.nextEpoch
	}
	return true, nil
}
