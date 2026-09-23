package client

import (
	"context"
	"net/netip"
	"slices"
	"strconv"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/tailscale/wireguard-go/device"
)

// Flow evidence is intentionally existential: it confirms one exact address
// and application tuple, not every address in a SUBNET or every service host.
// The catalog never derives it from a signed setting, route installation or
// WireGuard handshake alone.
func (e *WireGuardEngine) observeResourceFlows(ctx context.Context, cfg Config, inspection WireGuardInspection, paths []PeerPathStatus, own underlayDNSInterface, runner CommandRunner, now time.Time, proof *ResourceHostObservation, count *int) error {
	samples := e.resourceFlows.samples(now)
	if len(samples) > 64 || cfg.CachedMap == nil {
		return nil
	}
	records := peerDNSRecords(*cfg.CachedMap)
	for _, sample := range samples {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !slices.ContainsFunc(e.routerCfg.Addresses, func(p netip.Prefix) bool { return p.Addr() == sample.key.local }) {
			continue
		}
		peerIndex := resourceFlowPeerIndex(cfg.CachedMap, sample.key.remote)
		if peerIndex < 0 {
			continue
		}
		peer := cfg.CachedMap.Peers[peerIndex]
		ids := resourceFlowIDs(cfg.CachedMap, peer, sample, records)
		e.peerACLFilter.mu.RLock()
		unrestricted := resourceHostACLUnrestricted(e.peerACLFilter.current, sample.key.remote)
		e.peerACLFilter.mu.RUnlock()
		unrestricted = unrestricted && resourceSubnetFlowUnrestricted(e, cfg, sample.key.remote)
		if !unrestricted {
			ids = slices.DeleteFunc(ids, func(id string) bool {
				identity, err := resolveResourceInAuthenticatedMap(cfg.CachedMap, id)
				return err != nil || identity.Kind == ipc.ResourceKind_RESOURCE_KIND_SUBNET
			})
		}
		if len(ids) == 0 {
			continue
		}
		live, ok := wireGuardPeerForMapPeer(inspection, peer)
		if !ok {
			continue
		}
		relay, relayConfirmed := wireGuardRelayPeerObservation{}, false
		if !resourceHostPathObserved(paths, peer.ID, live, now) {
			if e.relayBridge == nil {
				continue
			}
			relay, ok = e.relayBridge.ObservePeer(*cfg.CachedMap, peer.ID)
			if !ok || !resourceHostRelayPathObserved(paths, peer, live, relay, now) {
				continue
			}
			relayConfirmed = true
		}
		*count++
		if *count > 4096 {
			return errResourceHostObservation
		}
		if observeResourceHostRoute(ctx, sample.key.remote, own, e.routerCfg.Addresses, runner) != nil {
			continue
		}
		for _, id := range ids {
			if !slices.ContainsFunc(proof.resources[id], func(existing resourceFlowSample) bool { return existing.key.remote == sample.key.remote }) {
				proof.resources[id] = append(proof.resources[id], sample)
			}
		}
		if relayConfirmed {
			proof.relayBridge = e.relayBridge
			proof.relays = append(proof.relays, relay)
		}
		if expiry := sample.observed.Add(resourceFlowLifetime); expiry.Before(proof.expires) {
			proof.expires = expiry
		}
		if handshake, complete := live.authenticatedHandshakeTime(); complete {
			if expiry := handshake.Add(device.RejectAfterTime); expiry.Before(proof.expires) {
				proof.expires = expiry
			}
		}
		for _, path := range paths {
			if path.PeerID != peer.ID || path.SelectedPath != "direct" {
				continue
			}
			for _, candidate := range append([]PathCandidateStatus{path.Direct}, path.Candidates...) {
				if candidate.Endpoint != live.Endpoint || candidate.State != "reachable" {
					continue
				}
				checked, err := time.Parse(time.RFC3339Nano, candidate.CheckedAt)
				if err == nil {
					if expiry := checked.Add(device.RejectAfterTime); expiry.Before(proof.expires) {
						proof.expires = expiry
					}
				}
			}
		}
	}
	// A service DNS name may select any signed host/address family. Positive
	// availability requires a fresh confirmed tuple for every advertised target.
	for _, service := range cfg.CachedMap.Network.Services {
		for _, port := range service.Ports {
			id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, service.ID+"\x00"+port.Protocol+"\x00"+strconv.Itoa(int(port.Port)))
			selected := proof.resources[id]
			if len(selected) == 0 {
				continue
			}
			if !resourceServiceTargetsConfirmed(service, records, selected) {
				delete(proof.resources, id)
			}
		}
	}
	return nil
}

// A permitted application or sharing flow does not establish unrestricted
// access to a containing subnet. Check both retained runtime reservations and
// the current signed reservations before assigning a SUBNET proof.
func resourceSubnetFlowUnrestricted(e *WireGuardEngine, cfg Config, address netip.Addr) bool {
	if e == nil || e.applicationFilter == nil || e.sharingFilter == nil || cfg.CachedMap == nil {
		return false
	}
	wantApp := newApplicationPacketFilter()
	wantApp.update(*cfg.CachedMap)
	wantSharing := newSharingPacketFilter()
	wantSharing.suspend(*cfg.CachedMap)
	for _, app := range []*applicationPacketFilter{e.applicationFilter, wantApp} {
		app.mu.RLock()
		protected := false
		for prefix := range app.protected {
			if prefix.Contains(address) {
				protected = true
				break
			}
		}
		app.mu.RUnlock()
		if protected {
			return false
		}
	}
	for _, sharing := range []*sharingPacketFilter{e.sharingFilter, wantSharing} {
		sharing.mu.Lock()
		protected := sharing.protected[address]
		sharing.mu.Unlock()
		if protected {
			return false
		}
	}
	return true
}

func resourceServiceTargetsConfirmed(service api.AdvertisedService, records []peerDNSRecord, selected []resourceFlowSample) bool {
	targets := 0
	for _, host := range service.Hosts {
		found := false
		for _, record := range records {
			if record.nodeID != host.NodeID {
				continue
			}
			found = true
			for _, raw := range []string{record.ipv4, record.ipv6} {
				if raw == "" {
					continue
				}
				targets++
				address, err := netip.ParseAddr(raw)
				if err != nil || !slices.ContainsFunc(selected, func(sample resourceFlowSample) bool { return sample.key.remote == address }) {
					return false
				}
			}
		}
		if !found {
			return false
		}
	}
	return targets > 0
}

// Reject ambiguous overlapping AllowedIPs. The selected WireGuard peer must
// be the same signed identity that owns the resource being reported.
func resourceFlowPeerIndex(source *api.RegisterNodeResponse, address netip.Addr) int {
	best, bits := -1, -1
	scanned := 0
	for i, peer := range source.Peers {
		for _, raw := range peer.AllowedIPs {
			scanned++
			if scanned > 8192 {
				return -1
			}
			prefix, err := netip.ParsePrefix(raw)
			if err != nil || prefix.Bits() == 0 || !prefix.Contains(address) {
				continue
			}
			if prefix.Bits() > bits {
				best, bits = i, prefix.Bits()
			} else if prefix.Bits() == bits && best != i {
				best = -1
			}
		}
	}
	return best
}

func resourceFlowIDs(source *api.RegisterNodeResponse, peer api.Peer, sample resourceFlowSample, records []peerDNSRecord) []string {
	ids := []string{}
	managedSingle := map[string]bool{}
	if policy := source.Network.ClientPolicy; policy != nil {
		for _, setting := range policy.Resources {
			if setting.Kind == api.ManagedResourceSubnet && setting.ID == peer.ID {
				managedSingle[setting.CIDR] = true
			}
		}
	}
	for _, raw := range peer.AllowedIPs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || prefix.Bits() == 0 || !prefix.Contains(sample.key.remote) || prefix.IsSingleIP() && !managedSingle[prefix.Masked().String()] {
			continue
		}
		ids = append(ids, rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SUBNET, peer.ID+"\x00"+prefix.Masked().String()))
	}
	addressIsServiceTarget := false
	for _, record := range records {
		if record.nodeID == peer.ID && (record.ipv4 == sample.key.remote.String() || record.ipv6 == sample.key.remote.String()) {
			addressIsServiceTarget = true
			break
		}
	}
	if !addressIsServiceTarget {
		return ids
	}
	protocol := "tcp"
	if sample.key.protocol == 17 {
		protocol = "udp"
	}
	for _, service := range source.Network.Services {
		if service.ApprovalStatus != "approved" || !slices.Contains(service.Hosts, api.ServiceHost{NodeID: peer.ID, PublicKey: peer.PublicKey}) {
			continue
		}
		for _, port := range service.Ports {
			if port.Protocol == protocol && port.Port == uint32(sample.key.remotePort) {
				ids = append(ids, rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, service.ID+"\x00"+protocol+"\x00"+strconv.Itoa(int(port.Port))))
			}
		}
	}
	return ids
}
