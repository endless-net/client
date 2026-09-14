package client

import (
	"context"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
)

// PrepareNetworkSelectionTarget reads fresh account authorization and constructs
// an isolated registration candidate. It does not save, enroll, stop or activate
// anything. A coordinator must CAS the source context and persist this candidate
// before registration, and verify old-route teardown before target activation.
func PrepareNetworkSelectionTarget(ctx context.Context, source Config, networkID string, provider ClientRPCNetworksProvider) (Config, error) {
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}
	if networkID == "" || len(networkID) > 256 || !utf8.ValidString(networkID) {
		return Config{}, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	source = clonePersistentConfig(source)
	if source.LocalOwnerID == "" || source.RPCState == nil || source.RPCState.ActiveProfileID == "" {
		return Config{}, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	profile, exists := source.RPCState.Profiles[source.RPCState.ActiveProfileID]
	if !exists || profile.ID != source.RPCState.ActiveProfileID || networkID == source.NetworkID {
		return Config{}, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	if source.ActiveAccountID == "" || source.Token == "" {
		return Config{}, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_LOGIN)
	}
	if len(source.ControlPlaneURLs) == 0 {
		return Config{}, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	for _, raw := range source.ControlPlaneURLs {
		origin, err := rpcProfileOrigin(raw)
		if err != nil || origin != profile.ControlOrigin {
			return Config{}, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
	}
	if provider == nil {
		return Config{}, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	catalog, err := provider(ctx, ClientRPCNetworksInput{ControlOrigin: profile.ControlOrigin, AccountID: source.ActiveAccountID, SessionToken: source.Token})
	if ctx.Err() != nil {
		return Config{}, ctx.Err()
	}
	if err != nil {
		return Config{}, rpcNetworkCatalogFailure(err)
	}
	if len(catalog) > 1000 {
		return Config{}, rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	}
	found := false
	seen := make(map[string]bool, len(catalog))
	for _, item := range catalog {
		if item == nil || item.Id == "" || len(item.Id) > 256 || !utf8.ValidString(item.Id) ||
			len(item.Name) > 1024 || !utf8.ValidString(item.Name) || item.AccountId != source.ActiveAccountID || seen[item.Id] {
			return Config{}, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
		}
		seen[item.Id] = true
		found = found || item.Id == networkID
	}
	if !found {
		return Config{}, rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_PERMISSION_REQUIRED)
	}
	// Allowlist installation/account authority. Do not copy old registration,
	// map revisions, recovery, exit choices, resource intents or routing policy.
	return Config{
		StateFormat: source.StateFormat, StateVersion: source.StateVersion,
		LocalOwnerID: source.LocalOwnerID, ControlPlaneURLs: source.ControlPlaneURLs,
		ManagementURL: source.ManagementURL, Token: source.Token, UserSession: source.UserSession,
		ActiveAccountID: source.ActiveAccountID, IdentityPrivateKey: source.IdentityPrivateKey, PrivateKey: source.PrivateKey,
		DeviceFingerprint: source.DeviceFingerprint, NetworkID: networkID,
		MapSigningTrust: source.MapSigningTrust, NodeCredentialSigningTrust: source.NodeCredentialSigningTrust,
		WireGuardMTU: source.WireGuardMTU, WireGuardRouteTable: source.WireGuardRouteTable,
		ConnectionIntent: &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "network_selection_preparing"},
	}, nil
}
