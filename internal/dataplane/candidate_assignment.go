package dataplane

import (
	"reflect"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// candidateAssignment is built only from the verifier's immutable result. A
// profile document or a peer's prepare message cannot construct this authority.
type candidateAssignment struct {
	mapValue connectivity.Map
	lease    *connectivity.Lease
	local    coordinator.Node
	peer     coordinator.Peer
	plan     coordinator.PeerTransportPlan
	offer    coordinator.TransportProfileOffer
}

func newCandidateAssignment(m connectivity.Map, lease *connectivity.Lease, peerID, profileID string, now time.Time) (candidateAssignment, error) {
	if lease == nil || m.Denied() || !lease.MatchesMap(m) || !lease.Permissions().NewFlows {
		return candidateAssignment{}, ErrNoAuthority
	}
	snapshot, err := m.Snapshot()
	if err != nil {
		return candidateAssignment{}, ErrNoAuthority
	}
	for _, peer := range snapshot.Peers {
		if peer.ID != peerID {
			continue
		}
		if peer.Transport == nil {
			return candidateAssignment{}, ErrNoAuthority
		}
		plan := *peer.Transport
		if plan.LocalIntentGeneration != m.IntentGeneration() || coordinator.ValidatePeerTransportPlan(plan, snapshot.Network, snapshot.Node, peer, snapshot.Revision, snapshot.MapSignature.IssuedAt, snapshot.MapSignature.ExpiresAt) != nil || !coordinator.TransportPlanCurrent(plan, now) {
			return candidateAssignment{}, ErrNoAuthority
		}
		// Bidirectional cryptographic transport is needed even for a one-way app
		// grant. The native stateful ACL still controls application initiation.
		if !pairGranted(peer) {
			return candidateAssignment{}, ErrNoAuthority
		}
		for _, offer := range plan.Profiles {
			if offer.Profile.ID == profileID {
				return candidateAssignment{mapValue: m, lease: lease, local: snapshot.Node, peer: peer, plan: plan, offer: offer}, nil
			}
		}
	}
	return candidateAssignment{}, ErrNoAuthority
}

func pairGranted(peer coordinator.Peer) bool {
	for _, grants := range [][]coordinator.ACLGrant{peer.ACLGrants, peer.IngressACLGrants} {
		for _, grant := range grants {
			if len(grant.DestinationCIDRs) > 0 && len(grant.AllowedPorts) > 0 {
				return true
			}
		}
	}
	return false
}

func (a candidateAssignment) permission(now time.Time, probe bool) error {
	if a.lease == nil || !a.lease.MatchesMap(a.mapValue) || now.IsZero() || now.Before(a.plan.IssuedAt) || !now.Before(a.plan.ExpiresAt) {
		return ErrNoAuthority
	}
	p := a.lease.Permissions()
	if p.Revoked || p.ClockUnsafe || !p.ExistingFlows || probe && !p.NewFlows {
		return ErrNoAuthority
	}
	return nil
}

// sameChannel permits retaining the listener, not the old probe or selected
// path. Semantic scope changes must invalidate those separately. A signature
// refresh of the same assignment must not renew a probe's original receipt.
func (a candidateAssignment) sameChannel(b candidateAssignment) bool {
	if a.local.AssignedIP != b.local.AssignedIP || len(a.peer.AllowedIPs) != 1 || len(b.peer.AllowedIPs) != 1 || a.peer.AllowedIPs[0] != b.peer.AllowedIPs[0] {
		return false
	}
	return a.plan.AccountID == b.plan.AccountID && a.plan.NetworkID == b.plan.NetworkID && a.plan.LocalNodeID == b.plan.LocalNodeID && a.plan.LocalPublicKey == b.plan.LocalPublicKey && a.plan.PeerNodeID == b.plan.PeerNodeID && a.plan.PeerPublicKey == b.plan.PeerPublicKey && a.plan.LocalIntentGeneration == b.plan.LocalIntentGeneration && a.plan.PeerAdvertisement.IntentGeneration == b.plan.PeerAdvertisement.IntentGeneration && a.offer.Digest == b.offer.Digest && transport.ValidDigest(a.offer.Digest)
}

// Global revisions do not change this path's authority. The bind revalidates
// the current signed assignment; original nonces and deadlines never move.
func (a candidateAssignment) sameProbe(b candidateAssignment) bool {
	return a.lease == b.lease && a.sameChannel(b) && a.offer.LocalEndpoint == b.offer.LocalEndpoint && a.offer.PeerEndpoint == b.offer.PeerEndpoint && a.local.Status == b.local.Status && a.local.ApprovalState == b.local.ApprovalState && a.peer.Status == b.peer.Status && a.peer.ACLRestricted == b.peer.ACLRestricted && reflect.DeepEqual(a.peer.ACLGrants, b.peer.ACLGrants) && reflect.DeepEqual(a.peer.IngressACLGrants, b.peer.IngressACLGrants)
}
