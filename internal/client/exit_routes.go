package client

import (
	"errors"
	"net/netip"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// ClientExitSelection is local intent, not backend authorization or evidence of
// applied routes. Pin both the map recipient and the selected WireGuard identity.
type ClientExitSelection struct {
	ID        string             `json:"id"`
	NetworkID string             `json:"network_id"`
	NodeID    string             `json:"node_id"`
	Host      api.ServiceHost    `json:"host"`
	Family    api.ExitFamilyMode `json:"family"`
	LAN       api.ExitLANAccess  `json:"lan"`
}

// Derive only routing input; never mutate the signed source. The caller must
// apply this together with packet enforcement and platform fail-closed rules.
// A rejected selection is an error, never an implicit clear/direct fallback.
func exitRoutePeers(cfg Config, source api.RegisterNodeResponse, selection *ClientExitSelection, now time.Time) ([]api.Peer, error) {
	if selection != nil {
		trust, err := SigningTrustBundle(cfg)
		if err != nil {
			return nil, err
		}
		if api.ValidateNetworkMap(source) != nil || api.VerifyNetworkMapSignatureWithTrustBundle(source, trust) != nil || source.MapSignature == nil || !now.Before(source.MapSignature.ExpiresAt) {
			return nil, errors.New("exit map is not authenticated and current")
		}
		if cfg.NodeID != source.Node.ID || cfg.NetworkID != source.Network.ID || selection.NodeID != cfg.NodeID || selection.NetworkID != cfg.NetworkID {
			return nil, errors.New("exit selection recipient changed")
		}
		if err := api.ValidateClientExitSelection(source.Snapshot(), selection.ID, selection.Family, selection.LAN, now); err != nil {
			return nil, err
		}
		bound := false
		for _, grant := range source.Network.ClientPolicy.ExitNodes {
			if grant.ID == selection.ID && grant.Host == selection.Host {
				bound = true
				break
			}
		}
		if !bound {
			return nil, errors.New("exit peer identity changed")
		}
	}
	peers := cloneRegisterNodeResponse(source).Peers
	for i := range peers {
		peer := &peers[i]
		allowed := make([]string, 0, len(peer.AllowedIPs))
		for _, value := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, errors.New("invalid exit route input")
			}
			if prefix.Bits() == 0 {
				if selection == nil || peer.ID != selection.Host.NodeID || peer.PublicKey != selection.Host.PublicKey {
					continue
				}
				ipv4 := prefix.Addr().Is4()
				if (ipv4 && selection.Family == api.ExitFamilyIPv6Only) || (!ipv4 && selection.Family == api.ExitFamilyIPv4Only) {
					continue
				}
			}
			allowed = append(allowed, value)
		}
		peer.AllowedIPs = allowed
	}
	return peers, nil
}
