package main

import (
	"context"
	"errors"

	"github.com/endless-net/client/internal/client"
)

func agentRPCPeers(opts agentIPCOptions) client.ClientRPCPeersProvider {
	return func(ctx context.Context) (client.ClientRPCPeerObservation, error) {
		result := client.ClientRPCPeerObservation{}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		cfg := opts.ConfigStore.Read()
		if cfg.RPCState == nil || cfg.RPCState.ActiveProfileID == "" {
			return result, errors.New("no active peer profile")
		}
		networkMap, err := verifiedCachedNetworkMap(&cfg)
		if err != nil {
			return result, err
		}
		paths, available := opts.WireGuard.TryPathStatus(networkMap.Network.ID, networkMap.Node.ID, networkMap.Network.Revision)
		if !available {
			return result, errors.New("current peer paths unavailable")
		}
		peers, _, failures := nativeDiagnosticPeers(networkMap, client.WireGuardInspection{})
		if len(failures) != 0 {
			return result, errors.New("invalid peer map")
		}
		if failures := nativeDiagnosticPaths(peers, paths); len(failures) != 0 {
			return result, errors.New("invalid peer observations")
		}
		result = client.ClientRPCPeerObservation{ProfileID: cfg.RPCState.ActiveProfileID, MapRevision: networkMap.Network.Revision, MapGlobalRevision: networkMap.Revision.Global, Peers: peers}
		return result, ctx.Err()
	}
}
