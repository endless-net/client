package client

import (
	"errors"
	"net/netip"
	"sync"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

type exitPacketPolicy struct {
	ordinary                 []netip.Prefix
	mapExpires, grantExpires time.Time
	ipv4, ipv6               bool
}

// This enforces traffic inside the TUN, not OS routes outside it. It must be
// combined with platform fail-closed protection before exit readiness is exposed.
type exitPacketFilter struct {
	mu               sync.RWMutex
	current, pending *exitPacketPolicy
	applying, closed bool
}

func compileExitPacketPolicy(cfg Config, source api.RegisterNodeResponse, selection *ClientExitSelection, now time.Time) (*exitPacketPolicy, error) {
	trust, err := SigningTrustBundle(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.NodeID != source.Node.ID || cfg.NetworkID != source.Network.ID || api.ValidateNetworkMap(source) != nil ||
		api.VerifyNetworkMapSignatureWithTrustBundle(source, trust) != nil || source.MapSignature == nil || !now.Before(source.MapSignature.ExpiresAt) {
		return nil, errors.New("exit packet policy requires current recipient-bound signed map")
	}
	peers, err := exitRoutePeers(cfg, source, selection, now)
	if err != nil {
		return nil, err
	}
	policy := &exitPacketPolicy{mapExpires: source.MapSignature.ExpiresAt}
	for _, peer := range peers {
		for _, cidr := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil {
				return nil, err
			}
			if prefix.Bits() != 0 {
				policy.ordinary = append(policy.ordinary, prefix)
			}
		}
	}
	if selection != nil {
		policy.ipv4 = selection.Family == api.ExitFamilyIPv4Only || selection.Family == api.ExitFamilyDualStack
		policy.ipv6 = selection.Family == api.ExitFamilyIPv6Only || selection.Family == api.ExitFamilyDualStack
		for _, grant := range source.Network.ClientPolicy.ExitNodes {
			if grant.ID == selection.ID {
				policy.grantExpires = grant.ExpiresAt
				break
			}
		}
	}
	return policy, nil
}

func (f *exitPacketFilter) suspend(cfg Config, source api.RegisterNodeResponse, selection *ClientExitSelection, now time.Time) error {
	policy, err := compileExitPacketPolicy(cfg, source, selection, now)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		f.closed = true
		f.pending = nil
		f.applying = false
		return err
	}
	f.pending, f.applying = policy, true
	return nil
}

func (f *exitPacketFilter) commit() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.applying || f.pending == nil {
		return
	}
	f.current, f.pending, f.applying, f.closed = f.pending, nil, false, false
}

func (f *exitPacketFilter) withdraw() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed, f.pending, f.applying = true, nil, false
}

func (f *exitPacketFilter) allows(raw []byte, inbound bool, now time.Time) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.closed {
		return false
	}
	packet, ok := parseApplicationPacket(raw)
	if !ok {
		return false
	}
	address := packet.destination
	if inbound {
		address = packet.source
	}
	return exitPacketAllowed(f.current, address, now) && (!f.applying || exitPacketAllowed(f.pending, address, now))
}

func exitPacketAllowed(policy *exitPacketPolicy, address netip.Addr, now time.Time) bool {
	if policy == nil || !now.Before(policy.mapExpires) {
		return false
	}
	for _, prefix := range policy.ordinary {
		if prefix.Contains(address) {
			return true
		}
	}
	return now.Before(policy.grantExpires) && ((address.Is4() && policy.ipv4) || (address.Is6() && policy.ipv6))
}
