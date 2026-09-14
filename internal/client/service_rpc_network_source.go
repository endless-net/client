package client

import (
	"reflect"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// A source may keep receiving verified endpoint/peer observations while its
// isolated target waits for approval. These observations must not cancel the
// selection or be copied into the target. All other config fields remain CAS.
func networkSelectionSourceConfigMatches(current, source Config) bool {
	current = networkSelectionContext(current)
	if reflect.DeepEqual(current, source) {
		return true
	}
	if !reflect.DeepEqual(networkSelectionSourceWithoutMap(current), networkSelectionSourceWithoutMap(source)) ||
		current.CachedMap == nil || source.CachedMap == nil || current.MapSigningTrust == nil {
		return false
	}
	state := current.CachedMap
	if state.MapSignature == nil || !time.Now().Before(state.MapSignature.ExpiresAt) ||
		state.Node.ID != current.NodeID || state.Node.NetworkID != current.NetworkID || state.Network.ID != current.NetworkID || state.Network.AccountID != current.ActiveAccountID ||
		state.Network.Revision != current.MapRevision || state.Revision.Global != current.MapGlobalRevision || state.MapSignature.PayloadHash != current.MapHash ||
		current.MapRevision < source.MapRevision || current.MapGlobalRevision < source.MapGlobalRevision ||
		(current.MapRevision == source.MapRevision && current.MapGlobalRevision == source.MapGlobalRevision && current.MapHash != source.MapHash) ||
		api.ValidateNetworkMap(*state) != nil || api.VerifyNetworkMapSignatureWithTrustBundle(*state, *current.MapSigningTrust) != nil {
		return false
	}
	return reflect.DeepEqual(networkSelectionSourceMapContext(*state), networkSelectionSourceMapContext(*source.CachedMap))
}

func networkSelectionSourceWithoutMap(cfg Config) Config {
	cfg.RPCState = nil
	cfg.CachedMap = nil
	cfg.CachedMapSavedAt = nil
	cfg.MapRevision, cfg.MapGlobalRevision, cfg.MapHash = 0, 0, ""
	return cfg
}

func networkSelectionSourceMapContext(state api.RegisterNodeResponse) api.NetworkMapSnapshot {
	snapshot := state.Snapshot()
	snapshot.Revision = api.MapRevision{}
	snapshot.MapSignature = nil
	snapshot.Network.Revision = 0
	snapshot.Peers, snapshot.STUNEndpoints, snapshot.Relays, snapshot.RelayCredential = nil, nil, nil, nil
	snapshot.Node.Endpoint = ""
	snapshot.Node.EndpointGeneration = 0
	snapshot.Node.EndpointCandidates = nil
	snapshot.Node.EndpointExpiresAt = nil
	snapshot.Node.Status = ""
	snapshot.Node.UpdatedAt, snapshot.Node.LastSeen = time.Time{}, time.Time{}
	return snapshot
}
