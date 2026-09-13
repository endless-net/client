package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ClientRPCServerIdentityProvider func(context.Context, Config) (clientapi.SigningTrustBundle, error)

func (s *ClientRPCService) GetServerIdentity(ctx context.Context, request *connect.Request[ipc.GetServerIdentityRequest]) (*connect.Response[ipc.GetServerIdentityResponse], error) {
	peer, _ := local.PeerFromContext(ctx)
	result, err := s.serverIdentityAs(ctx, peer, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (s *ClientRPCService) serverIdentityAs(ctx context.Context, peer local.Peer, request *ipc.GetServerIdentityRequest) (*ipc.GetServerIdentityResponse, error) {
	cfg := s.mutations.store.Read()
	method := rpcMethod("/client.v0.ClientService/GetServerIdentity")
	if err := authorizeRPCPeer(peer, method, cfg); err != nil {
		return nil, err
	}
	profile, err := rpcFindProfile(&cfg, request.Profile)
	if err != nil {
		return nil, err
	}
	if s.ServerIdentityProvider == nil {
		return nil, rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	selected := profile.Configuration
	if profile.ID == cfg.RPCState.ActiveProfileID {
		selected = cfg
	}
	trusted, err := SigningTrustBundle(selected)
	if err != nil {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_NEEDS_ENROLLMENT)
	}
	// Only public trust/origin cross the provider boundary; no local credentials.
	announced, err := s.ServerIdentityProvider(ctx, Config{ControlPlaneURLs: []string{profile.ControlOrigin}, MapSigningTrust: &trusted})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || announced.Validate() != nil {
		return nil, rpc.Error(connect.CodeUnavailable, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
	}
	s.mutations.mu.Lock()
	defer s.mutations.mu.Unlock()
	current := s.mutations.store.Read()
	if err := authorizeRPCPeer(peer, method, current); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(clonePersistentConfig(cfg), clonePersistentConfig(current)) {
		return nil, rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	}
	announcementID, err := rpcIdentityAnnouncementID(profile.ID, profile.ControlOrigin, trusted, announced)
	if err != nil {
		return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
	}
	return &ipc.GetServerIdentityResponse{Identity: &ipc.ServerIdentity{ProfileId: profile.ID, ControlOrigin: profile.ControlOrigin,
		TrustedKeyId: trusted.ActiveKeyID, AnnouncedKeyId: announced.ActiveKeyID, Changed: trusted.ActiveKeyID != announced.ActiveKeyID,
		AnnouncementId: announcementID}, Metadata: &ipc.SnapshotMetadata{InstanceId: s.mutations.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(s.mutations.now())}}, nil
}

func rpcIdentityAnnouncementID(profileID, origin string, trusted, announced clientapi.SigningTrustBundle) (string, error) {
	encoded, err := json.Marshal(struct {
		Profile, Origin    string
		Trusted, Announced clientapi.SigningTrustBundle
	}{profileID, origin, trusted, announced})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
