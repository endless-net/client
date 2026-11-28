package connectivity

import (
	"reflect"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// Shared authority includes the local identity, network policy, resources and
// relay scope. Individual peer changes are fenced by the lease at receipt.
func (m Map) continuesWith(next Map) bool {
	a, b := m.snapshot, next.snapshot
	if m.denied || next.denied || m.profileID != next.profileID || m.generation != next.generation || m.anchorID != next.anchorID ||
		a.MapSignature == nil || b.MapSignature == nil || a.MapSignature.KeyID != b.MapSignature.KeyID ||
		b.MapSignature.ExpiresAt.Before(a.MapSignature.ExpiresAt) || deadlineShortened(a.Node.EndpointExpiresAt, b.Node.EndpointExpiresAt) {
		return false
	}
	if a.RelayCredential != nil && b.RelayCredential != nil && b.RelayCredential.ExpiresAt.Before(a.RelayCredential.ExpiresAt) {
		return false
	}
	a.Peers, b.Peers = nil, nil
	return reflect.DeepEqual(transportConfiguration(a), transportConfiguration(b))
}

func samePeerPath(old, next coordinator.Peer) bool {
	if deadlineShortened(old.EndpointExpiresAt, next.EndpointExpiresAt) || old.Transport != nil && next.Transport != nil && next.Transport.ExpiresAt.Before(old.Transport.ExpiresAt) {
		return false
	}
	return reflect.DeepEqual(peerConfiguration(old), peerConfiguration(next))
}

func peerConfiguration(p coordinator.Peer) coordinator.Peer {
	p.EndpointGeneration, p.EndpointExpiresAt = 0, nil
	if p.Transport != nil {
		t := *p.Transport
		t.IssuedAt, t.ExpiresAt, t.Revision = time.Time{}, time.Time{}, coordinator.MapRevision{}
		p.Transport = &t
	}
	return p
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
	// Listener addresses are allocated per offered profile. The signed peer
	// plan carries the endpoints available to each individual pair.
	if s.Node.Transport != nil {
		ad := *s.Node.Transport
		ad.Endpoints = nil
		s.Node.Transport = &ad
	}
	s.Node.UpdatedAt, s.Node.LastSeen = time.Time{}, time.Time{}
	if s.RelayCredential != nil {
		c := *s.RelayCredential
		c.ExpiresAt, c.Signature = time.Time{}, ""
		s.RelayCredential = &c
	}
	return s
}
