package main

import (
	"context"
	"errors"
	"log"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

func initializeAgentStartup(ctx context.Context, engine agentWireGuard, store *client.ConfigStore, timeout time.Duration, offline bool) error {
	if err := engine.RestoreExitProtection(ctx, store.Read()); err != nil {
		return err
	}
	if !offline {
		if err := refreshAgentStartupPolicy(ctx, engine, store, timeout); err != nil {
			log.Printf("startup policy refresh unavailable; local recovery remains available")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return client.NewConnectionIntentStore(store).InitializeRuntimeIntent()
}

// Fetch policy before startup can reconnect. This path never applies network
// configuration and does not refresh trust across an unapproved key change.
func refreshAgentStartupPolicy(ctx context.Context, engine agentWireGuard, store *client.ConfigStore, timeout time.Duration) error {
	return refreshAgentStartupPolicyWith(ctx, engine, store, timeout, func(before, candidate client.Config) error {
		return store.Update(func(current *client.Config) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !client.StartupPolicyContextEqual(before, *current) {
				return errors.New("startup policy context changed")
			}
			client.ApplyStartupPolicyMap(current, candidate)
			return nil
		})
	})
}

func refreshAgentStartupPolicyWith(ctx context.Context, engine agentWireGuard, store *client.ConfigStore, timeout time.Duration, commit func(client.Config, client.Config) error) error {
	before := store.Read()
	wanted := before.ConnectionIntent != nil && (before.ConnectionIntent.DesiredState == client.ConnectionIntentDesiredConnected || before.ConnectionIntent.Reason == "runtime_start_policy_unavailable")
	if before.RPCState != nil {
		profile := before.RPCState.Profiles[before.RPCState.ActiveProfileID]
		wanted = wanted || profile.RuntimeStart != nil && *profile.RuntimeStart == ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT
	}
	if !wanted {
		return nil
	}
	return refreshAgentPolicySnapshot(ctx, engine, store, timeout, commit)
}

// Unlike startup admission, resume needs authenticated policy even when the
// current intent is disconnected. The shared fetch never applies a tunnel.
func refreshAgentPolicySnapshot(ctx context.Context, engine agentWireGuard, store *client.ConfigStore, timeout time.Duration, commit func(client.Config, client.Config) error) error {
	before := store.Read()
	if before.CachedMap != nil && before.CachedMap.MapSignature != nil && time.Now().Before(before.CachedMap.MapSignature.ExpiresAt) {
		if _, err := verifiedCachedNetworkMap(&before); err == nil {
			return commit(before, before)
		}
	}
	if before.NodeID == "" || before.NodeCredential == "" || len(before.ControlURLs()) == 0 {
		return errors.New("startup policy source unavailable")
	}
	if err := client.ValidateConfigCurrentDevice(before); err != nil {
		return errors.New("startup policy device authorization unavailable")
	}
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	candidate := store.Read()
	if !client.StartupPolicyContextEqual(before, candidate) {
		return errors.New("startup policy context changed")
	}
	control := apiFromConfig(candidate)
	if engine != nil {
		var err error
		control.HTTPClient, err = engine.ControlPlaneHTTPClient(candidate)
		if err != nil {
			return err
		}
	}
	control.HTTPClient.Timeout = timeout
	if closer, ok := control.HTTPClient.Transport.(interface{ CloseIdleConnections() }); ok {
		defer closer.CloseIdleConnections()
	}
	control.HTTPClient.Transport = enrollmentContextTransport{lifetime: ctx, base: control.HTTPClient.Transport}
	if err := refreshMapSigningTrust(&candidate, control); err != nil {
		return errors.New("startup policy trust unavailable")
	}
	// A full snapshot is required; an expired local map cannot base a delta.
	event, err := control.ReadMapStreamEvent(candidate.NodeID, api.MapCursor{}, timeout)
	if err != nil {
		return errors.New("startup policy map unavailable")
	}
	if event.Snapshot == nil {
		return errors.New("startup policy snapshot required")
	}
	networkMap, _, err := cacheNetworkMapFromEvent(&candidate, event)
	if err != nil {
		return errors.New("startup policy map rejected")
	}
	if err := cacheNetworkMapChecked(&candidate, networkMap); err != nil {
		return errors.New("startup policy map rejected")
	}
	candidate.MapHash = networkMap.MapSignature.PayloadHash
	if candidate.CachedMap == nil || candidate.CachedMap.MapSignature == nil || !time.Now().Before(candidate.CachedMap.MapSignature.ExpiresAt) {
		return errors.New("startup policy map expired")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return commit(before, candidate)
}

func retryAgentStartupPolicy(ctx context.Context, mutations *client.ClientRPCMutations, opts agentIPCOptions, timeout time.Duration) error {
	if !client.RuntimeStartRecoveryReady(opts.ConfigStore.Read()) {
		return nil
	}
	return refreshAgentStartupPolicyWith(ctx, opts.WireGuard, opts.ConfigStore, timeout, func(before, candidate client.Config) error {
		if mutations != nil {
			return mutations.RecoverStartupPolicy(ctx, before, candidate)
		}
		return client.NewConnectionIntentStore(opts.ConfigStore).RecoverStartupPolicy(ctx, before, candidate)
	})
}
