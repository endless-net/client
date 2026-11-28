package dataplane

import (
	"net/netip"
	"reflect"
	"slices"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

// candidateAssignment is built only from the verifier's immutable result. A
// profile document or a peer's prepare message cannot construct this authority.
type candidateAssignment struct {
	resources                   resourcePacketRules
	transit                     routerPacketRules
	prefixes                    []netip.Prefix
	routerSource                bool
	index                       uint32
	localEndpoint, peerEndpoint string
	mapValue                    connectivity.Map
	lease                       *connectivity.Lease
	local                       coordinator.Node
	peer                        coordinator.Peer
	plan                        coordinator.PeerTransportPlan
	offer                       coordinator.TransportProfileOffer
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
		return signedPeerAssignment(m, lease, snapshot, peer, profileID, now)
	}
	return candidateAssignment{}, ErrNoAuthority
}

// Can also be used while packet delivery is paused for a signed map refresh.
// Admission of new flows remains the responsibility of the caller.
func signedPeerAssignment(m connectivity.Map, lease *connectivity.Lease, snapshot coordinator.NetworkMapSnapshot, peer coordinator.Peer, profileID string, now time.Time) (candidateAssignment, error) {
	if peer.Transport == nil {
		return candidateAssignment{}, ErrNoAuthority
	}
	plan := *peer.Transport
	if plan.LocalIntentGeneration != m.IntentGeneration() || coordinator.ValidatePeerTransportPlan(plan, snapshot.Network, snapshot.Node, peer, snapshot.Revision, snapshot.MapSignature.IssuedAt, snapshot.MapSignature.ExpiresAt) != nil || !lease.WindowCurrent(m, plan.IssuedAt, plan.ExpiresAt, now) {
		return candidateAssignment{}, ErrNoAuthority
	}
	// Bidirectional cryptographic transport is needed even for a one-way app
	// grant. The native stateful ACL still controls application initiation.
	resources, _, err := recipientRulesWithHealth(snapshot, func(h coordinator.ServiceHost) bool { return h.NodeID == peer.ID && h.PublicKey == peer.PublicKey })
	if err != nil {
		return candidateAssignment{}, err
	}
	transit, subjects, err := routerPacketPolicy(snapshot)
	if err != nil {
		return candidateAssignment{}, err
	}
	prefixes := resources.forPeer(peer.ID)
	if !pairGranted(peer) && len(prefixes) == 0 && !subjects[peer.ID] {
		return candidateAssignment{}, ErrNoAuthority
	}
	a, err := offeredAssignment(m, lease, snapshot.Node, peer, profileID)
	if err != nil {
		return candidateAssignment{}, err
	}
	a.resources, a.transit, a.prefixes, a.routerSource = resources, transit, prefixes, subjects[peer.ID]
	return a, nil
}

// Read the profile from an already verified snapshot. This lookup alone does
// not establish current permission to open or retain the channel.
func offeredAssignment(m connectivity.Map, lease *connectivity.Lease, node coordinator.Node, peer coordinator.Peer, profileID string) (candidateAssignment, error) {
	if peer.Transport == nil {
		return candidateAssignment{}, ErrNoAuthority
	}
	plan := *peer.Transport
	for _, offer := range plan.Profiles {
		if offer.Profile.ID == profileID {
			a := candidateAssignment{mapValue: m, lease: lease, local: node, peer: peer, plan: plan, offer: offer}
			a.selectPair(0)
			return a, nil
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
	if a.lease == nil || !a.lease.WindowCurrent(a.mapValue, a.plan.IssuedAt, a.plan.ExpiresAt, now) {
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
	if !slices.Equal(a.prefixes, b.prefixes) || a.routerSource != b.routerSource || a.plan.LocalProbeIPv4 != b.plan.LocalProbeIPv4 || a.plan.PeerProbeIPv4 != b.plan.PeerProbeIPv4 {
		return false
	}
	if a.local.AssignedIP != b.local.AssignedIP || len(a.peer.AllowedIPs) != 1 || len(b.peer.AllowedIPs) != 1 || a.peer.AllowedIPs[0] != b.peer.AllowedIPs[0] {
		return false
	}
	return a.plan.AccountID == b.plan.AccountID && a.plan.NetworkID == b.plan.NetworkID && a.plan.LocalNodeID == b.plan.LocalNodeID && a.plan.LocalPublicKey == b.plan.LocalPublicKey && a.plan.PeerNodeID == b.plan.PeerNodeID && a.plan.PeerPublicKey == b.plan.PeerPublicKey && a.plan.LocalIntentGeneration == b.plan.LocalIntentGeneration && a.plan.PeerAdvertisement.IntentGeneration == b.plan.PeerAdvertisement.IntentGeneration && a.offer.Digest == b.offer.Digest && transport.ValidDigest(a.offer.Digest)
}

// Global revisions do not change this path's authority. The bind revalidates
// the current signed assignment; original nonces and deadlines never move.
func (a candidateAssignment) sameProbe(b candidateAssignment) bool {
	return a.lease == b.lease && a.sameChannel(b) && a.localEndpoint == b.localEndpoint && a.peerEndpoint == b.peerEndpoint && a.local.Status == b.local.Status && a.local.ApprovalState == b.local.ApprovalState && a.peer.Status == b.peer.Status && a.peer.ACLRestricted == b.peer.ACLRestricted && reflect.DeepEqual(a.peer.ACLGrants, b.peer.ACLGrants) && reflect.DeepEqual(a.peer.IngressACLGrants, b.peer.IngressACLGrants)
}

// selectPair uses the same initiator-major product on both peers, without allocating it.
func (a *candidateAssignment) selectPair(index uint32) {
	a.index = index
	a.localEndpoint, a.peerEndpoint = "", ""
	local, remote := a.offer.LocalEndpoints, a.offer.PeerEndpoints
	if len(local) == 0 || len(remote) == 0 {
		return
	}
	i := uint64(index)
	if a.plan.LocalNodeID < a.plan.PeerNodeID {
		a.localEndpoint = local[(i/uint64(len(remote)))%uint64(len(local))]
		a.peerEndpoint = remote[i%uint64(len(remote))]
	} else {
		a.localEndpoint = local[i%uint64(len(local))]
		a.peerEndpoint = remote[(i/uint64(len(local)))%uint64(len(remote))]
	}
}

// keepPair retains a qualified path across unrelated additions or withdrawals.
func (a *candidateAssignment) keepPair(local, peer string) bool {
	if !slices.Contains(a.offer.LocalEndpoints, local) || !slices.Contains(a.offer.PeerEndpoints, peer) {
		return false
	}
	a.localEndpoint, a.peerEndpoint = local, peer
	return true
}

func (a candidateAssignment) resourceCurrent(end time.Time) bool {
	return end.IsZero() || a.lease != nil && a.lease.MatchesMap(a.mapValue) && a.lease.GrantDeadlineCurrent(a.mapValue, end)
}
