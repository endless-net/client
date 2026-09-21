package client

import (
	"errors"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func resourceDenialsForMap(cfg Config, source api.RegisterNodeResponse, now time.Time) ([]resourceDenyRule, time.Time, error) {
	if source.MapSignature == nil && len(cfg.ResourcePreferences) == 0 && (source.Network.ClientPolicy == nil || len(source.Network.ClientPolicy.Resources) == 0) {
		return nil, time.Time{}, nil
	}
	cfg.CachedMap = &source
	cfg.MapRevision, cfg.MapGlobalRevision = source.Network.Revision, source.Revision.Global
	rules, err := compileResourceDenials(cfg, now)
	if err != nil {
		return nil, time.Time{}, err
	}
	return rules, source.MapSignature.ExpiresAt, nil
}

// Compile only denials; positive local values cannot add a route, peer or ACL.
// Invalid/stale local identities fail compilation instead of silently reopening
// traffic. A later reconciliation must explicitly resolve stale local choices.
func compileResourceDenials(cfg Config, now time.Time) ([]resourceDenyRule, error) {
	if len(cfg.ResourcePreferences)+len(cfg.ResourcePreferencesRetired) > resourcePreferenceLimit {
		return nil, errors.New("resource preference limit exceeded")
	}
	source := cfg.CachedMap
	if source == nil || source.MapSignature == nil || cfg.MapRevision != source.Network.Revision || cfg.MapGlobalRevision != source.Revision.Global {
		return nil, errors.New("resource map is unavailable")
	}
	if _, err := resolveNetworkAcceptance(cfg, *source, now); err != nil {
		return nil, err
	}
	for id := range cfg.ResourcePreferencesRetired {
		if _, err := resolveResourceInAuthenticatedMap(source, id); err == nil {
			return nil, errors.New("resource choice retirement has not been reconciled")
		}
	}
	ids := make(map[string]bool)
	for id := range cfg.ResourcePreferences {
		ids[id] = true
	}
	if policy := source.Network.ClientPolicy; policy != nil {
		for _, managed := range policy.Resources {
			switch managed.Kind {
			case api.ManagedResourceHost:
				ids[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, managed.ID)] = true
			case api.ManagedResourceSubnet:
				prefix, err := netip.ParsePrefix(managed.CIDR)
				if err != nil {
					return nil, err
				}
				if prefix.Bits() != 0 {
					ids[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SUBNET, managed.ID+"\x00"+prefix.Masked().String())] = true
				}
			case api.ManagedResourceService:
				for _, service := range source.Network.Services {
					if service.ID == managed.ID {
						for _, port := range service.Ports {
							ids[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, managed.ID+"\x00"+port.Protocol+"\x00"+strconv.Itoa(int(port.Port)))] = true
						}
					}
				}
			case api.ManagedResourceApplication:
				for _, app := range source.Network.Applications {
					if app.ID == managed.ID && slices.Contains(app.Sources, applicationSelf(*source)) {
						ids[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_APPLICATION, managed.ID)] = true
					}
				}
			}
		}
	}
	if len(ids) > 4096 {
		return nil, errors.New("resource preference limit exceeded")
	}
	keys := make([]string, 0, len(ids))
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	rules := []resourceDenyRule{}
	seen := map[resourceDenyRule]bool{}
	for _, id := range keys {
		identity, err := resolveResourceInAuthenticatedMap(source, id)
		if err != nil {
			return nil, err
		}
		if resourcePreferenceForIdentity(cfg, id, identity).Enabled {
			continue
		}
		footprint, err := resourcePacketFootprint(source, identity)
		if err != nil {
			return nil, err
		}
		for _, rule := range footprint {
			if !seen[rule] {
				if len(rules) >= 8192 {
					return nil, errors.New("resource packet rule limit exceeded")
				}
				seen[rule] = true
				rules = append(rules, rule)
			}
		}
	}
	return rules, nil
}

// Shared packet scope for denial compilation and catalog overlap reporting.
// A footprint is not a routing or access grant.
func resourcePacketFootprint(source *api.RegisterNodeResponse, identity clientResourceIdentity) ([]resourceDenyRule, error) {
	rules := []resourceDenyRule{}
	seen := map[resourceDenyRule]bool{}
	add := func(value string, protocol byte, port uint16, hostOnly bool) error {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return err
		}
		if prefix.Bits() == 0 || hostOnly && !prefix.IsSingleIP() {
			return nil
		}
		rule := resourceDenyRule{prefix: prefix.Masked(), protocol: protocol, port: port}
		if !seen[rule] {
			if len(rules) >= 8192 {
				return errors.New("resource packet rule limit exceeded")
			}
			seen[rule] = true
			rules = append(rules, rule)
		}
		return nil
	}
	switch identity.Kind {
	case ipc.ResourceKind_RESOURCE_KIND_HOST:
		for _, peer := range source.Peers {
			if peer.ID == identity.ID {
				for _, value := range peer.AllowedIPs {
					if err := add(value, 0, 0, true); err != nil {
						return nil, err
					}
				}
			}
		}
	case ipc.ResourceKind_RESOURCE_KIND_SUBNET:
		if err := add(identity.CIDR, 0, 0, false); err != nil {
			return nil, err
		}
	case ipc.ResourceKind_RESOURCE_KIND_SERVICE:
		protocol := byte(6)
		if identity.Protocol == "udp" {
			protocol = 17
		}
		for _, service := range source.Network.Services {
			if service.ID == identity.ID {
				for _, host := range service.Hosts {
					for _, peer := range source.Peers {
						if peer.ID == host.NodeID && peer.PublicKey == host.PublicKey {
							for _, value := range peer.AllowedIPs {
								if err := add(value, protocol, uint16(identity.Port), true); err != nil {
									return nil, err
								}
							}
						}
					}
				}
			}
		}
	case ipc.ResourceKind_RESOURCE_KIND_APPLICATION:
		for _, app := range source.Network.Applications {
			if app.ID == identity.ID {
				target, err := api.ParseApplicationTarget(app.TargetType, app.Target)
				if err != nil {
					return nil, err
				}
				protocol := byte(0)
				if target.TCPPort != 0 {
					protocol = 6
				}
				for _, route := range app.Routes {
					for _, value := range route.CIDRs {
						if err := add(value, protocol, target.TCPPort, false); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	return rules, nil
}
