package dataplane

import (
	"net"
	"net/netip"
	"slices"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// The supervisor owns each idle socket until the candidate opens it.
// A NAT socket discards idle peer datagrams while processing Binding replies.
type reservedListener struct {
	preparedListener
	socket *net.UDPConn
	nat    *natSocket
}

func reserveUDPListener(port uint16) (*net.UDPConn, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: int(port)})
	if err != nil {
		return nil, err
	}
	if err := conn.SetReadBuffer(2048); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (l *reservedListener) close() {
	if l.socket != nil {
		_ = l.socket.Close()
		l.socket = nil
		l.nat = nil
	}
}

func (p *supervisedRelay) takeListener(offer coordinator.TransportProfileOffer) uint16 {
	l := p.listeners[offer.Profile.ID]
	if l == nil || l.digest != offer.Digest || l.socket == nil {
		return 0
	}
	l.close()
	return l.port
}

func (p *supervisedRelay) hasCandidate(id string) bool {
	return (p.direct != nil && p.direct.profileID == id) ||
		(p.spare != nil && p.spare.profileID == id) ||
		(p.search != nil && p.search.candidate != nil && p.search.candidate.profileID == id)
}

func (g *transportSupervisor) authorizedOffers(peer coordinator.Peer, now time.Time) []coordinator.TransportProfileOffer {
	plan := peer.Transport
	if plan == nil || !pairGranted(peer) || !g.lease.AllowsPeerPath(g.current, peer.ID) || !g.lease.WindowCurrent(g.current, plan.IssuedAt, plan.ExpiresAt, now) {
		return nil
	}
	return plan.Profiles
}

func (g *transportSupervisor) reconcileReservedListeners() {
	if g.config.reserveListener == nil {
		return
	}
	now, err := g.clock.Sample()
	withinMapWindow := err == nil && g.snapshot.MapSignature != nil &&
		g.lease.OriginalExpiryCurrent(g.current, g.snapshot.MapSignature.ExpiresAt, now.Wall)
	plans := make(map[string][]coordinator.TransportProfileOffer, len(g.snapshot.Peers))
	for _, peer := range g.snapshot.Peers {
		// An interrupted source update can temporarily withhold new flows.
		// Retain the port only within the original signed map and plan windows.
		plan := peer.Transport
		if withinMapWindow && plan != nil && pairGranted(peer) &&
			g.lease.OriginalExpiryCurrent(g.current, plan.ExpiresAt, now.Wall) {
			plans[peer.ID] = plan.Profiles
		}
	}
	for peerID, p := range g.peers {
		offers := plans[peerID]
		for id, l := range p.listeners {
			if !slices.ContainsFunc(offers, func(o coordinator.TransportProfileOffer) bool { return o.Profile.ID == id && o.Digest == l.digest }) {
				l.close()
				delete(p.listeners, id)
			}
		}
	}
	if !withinMapWindow || !g.applied || !g.mapAvailable(g.current) {
		return
	}
	// Reserve in signed map order. Include existing active candidates and idle
	// ports in the same node-wide advertisement size limit.
	server := netip.AddrPort{}
	if len(g.snapshot.STUNEndpoints) > 0 {
		server, _ = netip.ParseAddrPort(g.snapshot.STUNEndpoints[0].Addr)
	}
	for _, p := range g.peers {
		for _, l := range p.listeners {
			if l.socket != nil && (l.nat != nil) != server.IsValid() {
				l.close()
			}
		}
	}
	published := g.preparedListeners()
	known := make(map[string]preparedListener, len(published))
	for _, l := range published {
		known[l.id] = l
	}
	for _, peer := range g.snapshot.Peers {
		p := g.peers[peer.ID]
		if p == nil {
			continue
		}
		for _, o := range g.authorizedOffers(peer, now.Wall) {
			id := o.Profile.ID
			l := p.listeners[id]
			if l == nil {
				l = &reservedListener{preparedListener: preparedListener{id: id, digest: o.Digest}}
				if p.listeners == nil {
					p.listeners = make(map[string]*reservedListener)
				}
				p.listeners[id] = l
			}
			if l.nat != nil {
				l.nat.discover(server)
			}
			for _, channel := range p.candidates() {
				if channel != nil && channel.profileID == id {
					channel.bind.mu.Lock()
					ns, _ := channel.bind.socket.(*natSocket)
					channel.bind.mu.Unlock()
					if ns != nil {
						ns.discover(server)
					}
				}
			}
			if active, ok := known[id]; ok {
				if active.digest == l.digest {
					l.port = active.port
				}
				continue
			}
			// Check capacity before opening a socket, using the widest possible port.
			proposed := preparedListener{id: id, digest: o.Digest, port: 65535}
			if server.IsValid() {
				proposed.local = netip.MustParseAddr("255.255.255.254")
				proposed.mapped = netip.MustParseAddrPort("255.255.255.253:65535")
			}
			if !announcementFits(listenerAdvertisement(g.generation, append(published, proposed), netip.MustParseAddr("255.255.255.254"))) {
				return
			}
			if server.IsValid() {
				ns, err := reserveNATSocket(server, l.port)
				if err != nil && l.port != 0 {
					ns, err = reserveNATSocket(server, 0)
				}
				if err == nil {
					l.nat = ns
					l.socket = ns.UDPConn
					l.port = ns.local.Port()
					ns.discover(server)
					published = append(published, proposed)
					known[id] = l.preparedListener
					continue
				}
			}
			conn, err := g.config.reserveListener(l.port)
			if err != nil && l.port != 0 && !p.hasCandidate(id) {
				conn, err = g.config.reserveListener(0)
			}
			if err != nil {
				continue
			}
			l.socket = conn
			l.port = uint16(conn.LocalAddr().(*net.UDPAddr).Port)
			published = append(published, l.preparedListener)
			known[id] = l.preparedListener
		}
	}
}

// Active engines have priority over idle reservations in the announcement.
// The actor reconciles idle ports with the remaining capacity after selection.
func (g *transportSupervisor) admitPreparedListener() bool {
	if g.listenersFit() {
		return true
	}
	for i := len(g.snapshot.Peers) - 1; i >= 0; i-- {
		peer := g.snapshot.Peers[i]
		p := g.peers[peer.ID]
		if p == nil || peer.Transport == nil {
			continue
		}
		offers := peer.Transport.Profiles
		for j := len(offers) - 1; j >= 0; j-- {
			l := p.listeners[offers[j].Profile.ID]
			if l == nil || l.socket == nil {
				continue
			}
			l.close()
			if g.listenersFit() {
				return true
			}
		}
	}
	return false
}

func (p *supervisedRelay) candidates() [3]*assignedCipher {
	channels := [3]*assignedCipher{p.direct, p.spare, nil}
	if p.search != nil {
		channels[2] = p.search.candidate
	}
	return channels
}
