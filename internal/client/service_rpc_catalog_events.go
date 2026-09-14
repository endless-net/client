package client

import (
	"crypto/sha256"
	"encoding/json"
	"time"
)

// Catalog reads authenticate their source independently. This fingerprint only
// detects when consumers must refetch, including a changed invalid source. Hash
// the actual data rather than trusting the claimed signature payload hash.
func rpcCatalogFingerprint(cfg Config, now time.Time) ([32]byte, error) {
	profile := ""
	if cfg.RPCState != nil {
		profile = cfg.RPCState.ActiveProfileID
	}
	var expired []bool
	if state := cfg.CachedMap; state != nil {
		if state.MapSignature != nil {
			expired = append(expired, !now.Before(state.MapSignature.ExpiresAt))
		}
		if policy := state.Network.ClientPolicy; policy != nil {
			for _, exit := range policy.ExitNodes {
				expired = append(expired, !now.Before(exit.ExpiresAt))
			}
		}
		for _, app := range state.Network.Applications {
			for _, route := range app.Routes {
				expired = append(expired, !now.Before(route.ExpiresAt))
			}
		}
	}
	encoded, err := json.Marshal([]any{profile, cfg.LocalOwnerID, cfg.ActiveAccountID,
		cfg.NodeID, cfg.NetworkID, cfg.MapRevision, cfg.MapGlobalRevision,
		cfg.CachedMap, cfg.MapSigningTrust, expired})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
