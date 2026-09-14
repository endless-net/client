package main

import (
	"context"
	"errors"
	"reflect"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/client"
)

// Fetch policy before startup can reconnect. This path never applies network
// configuration and does not refresh trust across an unapproved key change.
func refreshAgentStartupPolicy(ctx context.Context, store *client.ConfigStore, timeout time.Duration) error {
	before := store.Read()
	wanted := before.ConnectionIntent != nil && (before.ConnectionIntent.DesiredState == client.ConnectionIntentDesiredConnected || before.ConnectionIntent.Reason == "runtime_start_policy_unavailable")
	if before.RPCState != nil {
		profile := before.RPCState.Profiles[before.RPCState.ActiveProfileID]
		wanted = wanted || profile.RuntimeStart != nil && *profile.RuntimeStart == ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_CONNECT
	}
	if !wanted {
		return nil
	}
	if before.CachedMap != nil && before.CachedMap.MapSignature != nil && time.Now().Before(before.CachedMap.MapSignature.ExpiresAt) {
		if _, err := verifiedCachedNetworkMap(&before); err == nil {
			return nil
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
	if !startupPolicyContextEqual(before, candidate) {
		return errors.New("startup policy context changed")
	}
	control := apiFromConfig(candidate)
	control.HTTPClient.Timeout = timeout
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
	return store.Update(func(current *client.Config) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !startupPolicyContextEqual(before, *current) {
			return errors.New("startup policy context changed")
		}
		current.CachedMap, current.CachedMapSavedAt = candidate.CachedMap, candidate.CachedMapSavedAt
		current.MapRevision, current.MapGlobalRevision = candidate.MapRevision, candidate.MapGlobalRevision
		current.MapHash = candidate.MapHash
		current.MapSigningTrust = candidate.MapSigningTrust
		current.NodeApprovalState = candidate.NodeApprovalState
		return nil
	})
}

func startupPolicyContextEqual(a, b client.Config) bool {
	return a.NodeID == b.NodeID && a.NetworkID == b.NetworkID && a.NodeCredential == b.NodeCredential && a.LocalOwnerID == b.LocalOwnerID &&
		a.PrivateKey == b.PrivateKey && a.IdentityPrivateKey == b.IdentityPrivateKey && a.DeviceFingerprint == b.DeviceFingerprint && a.Token == b.Token &&
		a.MapRevision == b.MapRevision && a.MapGlobalRevision == b.MapGlobalRevision && a.MapHash == b.MapHash && reflect.DeepEqual(a.ControlURLs(), b.ControlURLs()) &&
		reflect.DeepEqual(a.ConnectionIntent, b.ConnectionIntent) && reflect.DeepEqual(a.RPCState, b.RPCState) &&
		reflect.DeepEqual(a.MapSigningTrust, b.MapSigningTrust) && reflect.DeepEqual(a.CachedMap, b.CachedMap)
}
