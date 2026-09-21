package client

import (
	"encoding/base64"
	"errors"
	"maps"
	"net/netip"
	"strconv"
	"strings"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

const resourcePreferenceLimit = 4096

// ReconcileResourcePreferencesForMap moves choices between the current catalog
// and a durable retired catalog. Retired choices are not packet rules. When an
// identity returns its saved choice returns too, subject to the current policy.
// The caller must persist these changes atomically with next and its revisions.
// This does not change a live filter: Configure retains old denials until apply
// commits. Pending operation journals remain bound to their original map.
func ReconcileResourcePreferencesForMap(cfg *Config, next api.RegisterNodeResponse, now time.Time) error {
	if cfg == nil {
		return errors.New("resource configuration is required")
	}
	if len(cfg.ResourcePreferences)+len(cfg.ResourcePreferencesRetired) == 0 {
		return nil
	}
	if len(cfg.ResourcePreferences)+len(cfg.ResourcePreferencesRetired) > resourcePreferenceLimit {
		return errors.New("resource preference limit exceeded")
	}
	if next.MapSignature == nil || cfg.MapSigningTrust == nil {
		return errors.New("resource map signature is required")
	}
	if _, err := resolveNetworkAcceptance(*cfg, next, now); err != nil {
		return err
	}
	previous := cfg.CachedMap
	// Only next supplies authority. The old cache may have expired or been
	// signed by the key replaced during explicit trust recovery. Its metadata
	// checks local context/rollback, not historical resource authorization.
	previousHash := cfg.MapHash
	if previous != nil {
		if previous.Node.ID != cfg.NodeID || previous.Network.ID != cfg.NetworkID || previous.Network.Revision != cfg.MapRevision || previous.Revision.Global != cfg.MapGlobalRevision {
			return errors.New("resource choice cached map context changed")
		}
		if previous.MapSignature != nil {
			if previousHash != "" && previousHash != previous.MapSignature.PayloadHash {
				return errors.New("resource choice cached map hash changed")
			}
			previousHash = previous.MapSignature.PayloadHash
		}
	}
	// Pending enrollment may clear the old cache without changing the enrolled
	// node. Preserve its intent and reconcile only against authenticated next.
	if next.Network.Revision < cfg.MapRevision || next.Revision.Global < cfg.MapGlobalRevision ||
		(next.Network.Revision == cfg.MapRevision && next.Revision.Global == cfg.MapGlobalRevision && previousHash != "" && next.MapSignature.PayloadHash != previousHash) {
		return errors.New("resource choice map context changed or rolled back")
	}
	active, retired := maps.Clone(cfg.ResourcePreferences), maps.Clone(cfg.ResourcePreferencesRetired)
	if active == nil {
		active = make(map[string]bool)
	}
	if retired == nil {
		retired = make(map[string]bool)
	}
	for id, enabled := range cfg.ResourcePreferences {
		if _, duplicate := retired[id]; duplicate {
			return errors.New("resource choice is both active and retired")
		}
		if !canonicalResourcePreferenceID(id) {
			return errors.New("invalid active resource identity")
		}
		// Saved intent need not still exist in the old catalog. After canonical
		// validation this resolver only tests the authenticated next catalog;
		// explicit retirement also repairs an already-orphaned local choice.
		if _, err := resolveResourceInAuthenticatedMap(&next, id); err != nil {
			delete(active, id)
			retired[id] = enabled
		}
	}
	for id, enabled := range cfg.ResourcePreferencesRetired {
		if !canonicalResourcePreferenceID(id) {
			return errors.New("invalid retired resource identity")
		}
		if _, err := resolveResourceInAuthenticatedMap(&next, id); err == nil {
			delete(retired, id)
			active[id] = enabled
		}
	}
	if len(active) == 0 {
		active = nil
	}
	if len(retired) == 0 {
		retired = nil
	}
	cfg.ResourcePreferences, cfg.ResourcePreferencesRetired = active, retired
	return nil
}

func canonicalResourcePreferenceID(id string) bool {
	if len(id) == 0 || len(id) > 2048 {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(id)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != id {
		return false
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) < 2 || parts[1] == "" {
		return false
	}
	switch parts[0] {
	case "1", "4":
		return len(parts) == 2
	case "2":
		if len(parts) != 3 {
			return false
		}
		prefix, err := netip.ParsePrefix(parts[2])
		return err == nil && prefix.Bits() != 0 && prefix.Masked().String() == parts[2]
	case "3":
		if len(parts) != 4 || parts[2] != "tcp" && parts[2] != "udp" {
			return false
		}
		port, err := strconv.ParseUint(parts[3], 10, 16)
		return err == nil && port != 0 && strconv.FormatUint(port, 10) == parts[3]
	}
	return false
}
