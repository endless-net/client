package connectivity

import (
	"reflect"
	"time"

	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

type pathDeadline struct {
	uptime time.Duration
	wall   time.Time
}

func (d pathDeadline) current(now ClockSample) bool {
	return now.UpperUptime < d.uptime && now.Wall.Before(d.wall)
}

// since is the first cursor of this uninterrupted grant. Removing and
// regranting a peer creates a new signal and cannot revive an old handler.
type peerPathLease struct {
	config      coordinator.Peer
	since       state.MapCursor
	boundSince  state.MapCursor
	accessSince state.MapCursor
	changed     chan struct{}
}

func (l *Lease) signalSourceChange() {
	close(l.sourceChanged)
	l.sourceChanged = make(chan struct{})
	for id, p := range l.peerPaths {
		close(p.changed)
		p.changed = make(chan struct{})
		l.peerPaths[id] = p
	}
}

func (l *Lease) observePeers(m Map) {
	cursor, last := m.Cursor(), l.observed
	if cursor.PayloadHash == "" || cursor == last || cursor.Revision.Network < last.Revision.Network ||
		cursor.IssuedAt.Before(last.IssuedAt) || cursor.Revision == last.Revision && !cursor.IssuedAt.After(last.IssuedAt) {
		return
	}
	next := make(map[string]peerPathLease, len(m.snapshot.Peers))
	for _, peer := range m.snapshot.Peers {
		path, ok := l.peerPaths[peer.ID]
		boundSince := path.boundSince
		if !ok || path.config.PublicKey != peer.PublicKey || path.config.NetworkID != peer.NetworkID {
			boundSince = cursor
		}
		accessSince := path.accessSince
		if !ok || path.config.Status != peer.Status || path.config.ACLRestricted != peer.ACLRestricted ||
			!reflect.DeepEqual(path.config.ACLGrants, peer.ACLGrants) || !reflect.DeepEqual(path.config.IngressACLGrants, peer.IngressACLGrants) {
			accessSince = cursor
		}
		if !ok || !samePeerPath(path.config, peer) {
			if ok {
				close(path.changed)
			}
			path = peerPathLease{since: cursor, changed: make(chan struct{})}
		}
		path.boundSince, path.accessSince = boundSince, accessSince
		path.config = peer
		next[peer.ID] = path
	}
	for id, path := range l.peerPaths {
		if _, ok := next[id]; !ok {
			close(path.changed)
		}
	}
	l.observed, l.peerPaths = cursor, next
}

func (l *Lease) AllowsPeerPath(m Map, peer string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peerScopeCurrent(m, peer) && l.matchesMap(m)
}

// PeerScopeCurrent checks continuity of the peer configuration for queue
// reconciliation. Delivery separately requires a live lease and applied map.
func (l *Lease) PeerScopeCurrent(m Map, peer string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.peerScopeCurrent(m, peer)
}

func (l *Lease) peerScopeCurrent(m Map, peer string) bool {
	path, ok := l.peerPaths[peer]
	c := m.Cursor()
	return ok && m.profileID == l.profileID && m.generation == l.generation && c.Revision.Network >= path.since.Revision.Network && !c.IssuedAt.Before(path.since.IssuedAt)
}

// PeerBindingCurrent identifies handlers created before removal or key replacement.
func (l *Lease) PeerBindingCurrent(c state.MapCursor, peer string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	path, ok := l.peerPaths[peer]
	return ok && c.Revision.Network >= path.boundSince.Revision.Network && !c.IssuedAt.Before(path.boundSince.IssuedAt)
}

func (l *Lease) PeerAccessCurrent(c state.MapCursor, peer string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	path, ok := l.peerPaths[peer]
	return ok && c.Revision.Network >= path.accessSince.Revision.Network && !c.IssuedAt.Before(path.accessSince.IssuedAt)
}

// PeerSignal closes on a change to this path or any broader source fence.
func (l *Lease) PeerSignal(peer string) <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if p, ok := l.peerPaths[peer]; ok {
		return p.changed
	}
	ch := make(chan struct{})
	close(ch)
	return ch
}
