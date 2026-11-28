package connectivity

import (
	"reflect"
	"slices"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// Compare the signed configuration, ignoring publication metadata. During a
// renewal each established path still observes the deadlines of its own map.
func (m Map) continuesWith(next Map) bool {
	a, b := m.snapshot, next.snapshot
	if m.denied || next.denied || m.profileID != next.profileID || m.generation != next.generation || m.anchorID != next.anchorID ||
		a.MapSignature == nil || b.MapSignature == nil || a.MapSignature.KeyID != b.MapSignature.KeyID ||
		b.MapSignature.ExpiresAt.Before(a.MapSignature.ExpiresAt) || len(a.Peers) != len(b.Peers) {
		return false
	}
	if deadlineShortened(a.Node.EndpointExpiresAt, b.Node.EndpointExpiresAt) {
		return false
	}
	for i, old := range a.Peers {
		fresh := b.Peers[i]
		if old.ID != fresh.ID || deadlineShortened(old.EndpointExpiresAt, fresh.EndpointExpiresAt) {
			return false
		}
		if old.Transport != nil && fresh.Transport != nil && fresh.Transport.ExpiresAt.Before(old.Transport.ExpiresAt) {
			return false
		}
	}
	if a.RelayCredential != nil && b.RelayCredential != nil && b.RelayCredential.ExpiresAt.Before(a.RelayCredential.ExpiresAt) {
		return false
	}
	return reflect.DeepEqual(transportConfiguration(a), transportConfiguration(b))
}

func deadlineShortened(old, next *time.Time) bool {
	if old == nil || next == nil {
		return old != next
	}
	return next.Before(*old)
}

func transportConfiguration(s coordinator.NetworkMapSnapshot) coordinator.NetworkMapSnapshot {
	s.MapSignature = nil
	s.Revision = coordinator.MapRevision{}
	s.Network.Revision = 0
	s.Node.EndpointGeneration, s.Node.EndpointExpiresAt = 0, nil
	s.Node.UpdatedAt, s.Node.LastSeen = time.Time{}, time.Time{}
	if s.RelayCredential != nil {
		c := *s.RelayCredential
		c.ExpiresAt, c.Signature = time.Time{}, ""
		s.RelayCredential = &c
	}
	s.Peers = slices.Clone(s.Peers)
	for i := range s.Peers {
		p := &s.Peers[i]
		p.EndpointGeneration, p.EndpointExpiresAt = 0, nil
		if p.Transport != nil {
			t := *p.Transport
			t.IssuedAt, t.ExpiresAt, t.Revision = time.Time{}, time.Time{}, coordinator.MapRevision{}
			p.Transport = &t
		}
	}
	return s
}
