package client

import (
	"encoding/base64"
	"errors"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

type clientResourceIdentity struct {
	Kind     ipc.ResourceKind
	ID       string
	CIDR     string
	Protocol string
	Port     uint32
}

func rpcResourceID(kind ipc.ResourceKind, key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(int(kind)) + "\x00" + key))
}

// Resource mutation identities must come from the current authenticated
// recipient catalog. A caller cannot turn a host into a subnet, invent a port,
// target an application connector, or use a default route as a resource.
func resolveRPCResource(cfg Config, id string, now time.Time) (clientResourceIdentity, error) {
	invalid := func() (clientResourceIdentity, error) {
		return clientResourceIdentity{}, errors.New("resource is not in the authenticated catalog")
	}
	if len(id) == 0 || len(id) > 2048 {
		return invalid()
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(id)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != id {
		return invalid()
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) < 2 || parts[1] == "" {
		return invalid()
	}
	if cfg.CachedMap == nil || cfg.CachedMap.MapSignature == nil || cfg.MapRevision != cfg.CachedMap.Network.Revision || cfg.MapGlobalRevision != cfg.CachedMap.Revision.Global {
		return invalid()
	}
	if _, err := resolveNetworkAcceptance(cfg, *cfg.CachedMap, now); err != nil {
		return invalid()
	}
	source := cfg.CachedMap
	result := clientResourceIdentity{ID: parts[1]}
	switch parts[0] {
	case "1":
		if len(parts) != 2 {
			return invalid()
		}
		result.Kind = ipc.ResourceKind_RESOURCE_KIND_HOST
		for _, peer := range source.Peers {
			if peer.ID != result.ID {
				continue
			}
			for _, value := range peer.AllowedIPs {
				if prefix, err := netip.ParsePrefix(value); err == nil && prefix.IsSingleIP() {
					return result, nil
				}
			}
		}
	case "2":
		if len(parts) != 3 {
			return invalid()
		}
		prefix, err := netip.ParsePrefix(parts[2])
		if err != nil || prefix.Bits() == 0 || prefix.Masked().String() != parts[2] {
			return invalid()
		}
		result.Kind, result.CIDR = ipc.ResourceKind_RESOURCE_KIND_SUBNET, parts[2]
		declared := !prefix.IsSingleIP()
		if policy := source.Network.ClientPolicy; policy != nil {
			for _, managed := range policy.Resources {
				if managed.Kind == api.ManagedResourceSubnet && managed.ID == result.ID && managed.CIDR == result.CIDR {
					declared = true
				}
			}
		}
		if !declared {
			return invalid()
		}
		for _, peer := range source.Peers {
			if peer.ID == result.ID {
				for _, value := range peer.AllowedIPs {
					p, err := netip.ParsePrefix(value)
					if err == nil && p.Masked() == prefix {
						return result, nil
					}
				}
			}
		}
	case "3":
		if len(parts) != 4 {
			return invalid()
		}
		port, err := strconv.ParseUint(parts[3], 10, 16)
		if err != nil || port == 0 || strconv.FormatUint(port, 10) != parts[3] {
			return invalid()
		}
		result.Kind, result.Protocol, result.Port = ipc.ResourceKind_RESOURCE_KIND_SERVICE, parts[2], uint32(port)
		for _, service := range source.Network.Services {
			if service.ID == result.ID {
				for _, allowed := range service.Ports {
					if allowed.Protocol == result.Protocol && allowed.Port == result.Port {
						return result, nil
					}
				}
			}
		}
	case "4":
		if len(parts) != 2 {
			return invalid()
		}
		result.Kind = ipc.ResourceKind_RESOURCE_KIND_APPLICATION
		for _, app := range source.Network.Applications {
			if app.ID == result.ID && slices.Contains(app.Sources, applicationSelf(*source)) {
				return result, nil
			}
		}
	}
	return invalid()
}
