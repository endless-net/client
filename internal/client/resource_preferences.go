package client

import (
	"net/netip"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Resolved choice only, never observed reachability or permission beyond the
// signed ACL. The runtime must intersect it with packet/resource restrictions.
type clientResourceSetting struct {
	Identity  clientResourceIdentity
	Requested *bool
	Enabled   bool
	Managed   *api.ManagedResourceSetting
}

func resolveResourcePreference(cfg Config, id string, now time.Time) (clientResourceSetting, error) {
	identity, err := resolveRPCResource(cfg, id, now)
	if err != nil {
		return clientResourceSetting{}, err
	}
	result := clientResourceSetting{Identity: identity, Enabled: true}
	kind := map[ipc.ResourceKind]api.ManagedResourceKind{
		ipc.ResourceKind_RESOURCE_KIND_HOST:        api.ManagedResourceHost,
		ipc.ResourceKind_RESOURCE_KIND_SUBNET:      api.ManagedResourceSubnet,
		ipc.ResourceKind_RESOURCE_KIND_SERVICE:     api.ManagedResourceService,
		ipc.ResourceKind_RESOURCE_KIND_APPLICATION: api.ManagedResourceApplication,
	}[identity.Kind]
	if policy := cfg.CachedMap.Network.ClientPolicy; policy != nil {
		for _, managed := range policy.Resources {
			if managed.Kind != kind || managed.ID != identity.ID {
				continue
			}
			if kind == api.ManagedResourceSubnet {
				prefix, err := netip.ParsePrefix(managed.CIDR)
				if err != nil || prefix.Masked().String() != identity.CIDR {
					continue
				}
			}
			copy := managed
			result.Managed, result.Enabled = &copy, managed.Enabled
			break
		}
	}
	if requested, exists := cfg.ResourcePreferences[id]; exists {
		result.Requested = proto.Bool(requested)
		if result.Managed == nil || !result.Managed.Locked {
			result.Enabled = requested
		}
	}
	return result, nil
}
