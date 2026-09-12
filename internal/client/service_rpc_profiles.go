package client

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxRPCProfiles = 128

func (m *ClientRPCMutations) listProfilesAs(peer local.Peer, request *ipc.ListProfilesRequest) (*ipc.ListProfilesResponse, error) {
	cfg := m.store.Read()
	const procedure = "/client.v0.ClientService/ListProfiles"
	if err := authorizeRPCPeer(peer, rpcMethod(procedure), cfg); err != nil {
		return nil, err
	}
	profiles := []clientRPCProfile{}
	activeID := ""
	revision := uint64(1)
	if cfg.RPCState != nil {
		for _, profile := range cfg.RPCState.Profiles {
			profiles = append(profiles, profile)
		}
		activeID, revision = cfg.RPCState.ActiveProfileID, cfg.RPCState.Revision
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	start, end, next, err := m.pageRange(peer, procedure, request.GetPage(), cfg, len(profiles))
	if err != nil {
		return nil, err
	}
	result := &ipc.ListProfilesResponse{ActiveProfileId: activeID, Page: &ipc.PageResponse{NextPageToken: next, Metadata: &ipc.SnapshotMetadata{InstanceId: m.instanceID, Revision: revision, GeneratedAt: timestamppb.New(m.now())}}}
	for _, profile := range profiles[start:end] {
		configuration := profile.Configuration
		if profile.ID == activeID {
			configuration = cfg
		}
		state := ipc.ProfileState_PROFILE_STATE_EMPTY
		if rpcConfigHasEnrollment(configuration) {
			state = ipc.ProfileState_PROFILE_STATE_REGISTERED
		}
		if configuration.EnrollmentRequestID != "" {
			state = ipc.ProfileState_PROFILE_STATE_NEEDS_APPROVAL
		}
		result.Profiles = append(result.Profiles, &ipc.Profile{Id: profile.ID, DisplayName: profile.DisplayName, ControlOrigin: profile.ControlOrigin,
			AccountId: configuration.ActiveAccountID, SelectedNetworkId: configuration.NetworkID, State: state, Active: profile.ID == activeID,
			Selection: &ipc.Restriction{Availability: ipc.Availability_AVAILABILITY_UNSUPPORTED, ReasonKey: "profile_selection_not_implemented"}})
	}
	return result, nil
}

// Configuration excludes installation RPC state/ownership. Active-profile
// state lives in the top-level Config until a safe tunnel-context switch saves
// it here. Never expose Configuration through the local contract.
type clientRPCProfile struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	ControlOrigin string `json:"control_origin"`
	Configuration Config `json:"configuration"`
}

func rpcProfileDisplayName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || value == "" || len(value) > 128 || strings.ContainsFunc(value, unicode.IsControl) {
		return "", rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	return value, nil
}

func rpcProfileOrigin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return "", rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if strings.HasSuffix(u.Host, ":") || strings.Contains(value, "#") {
		return "", rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if port := u.Port(); port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return "", rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
	}
	// A profile stores an origin, not a callback, service path or credentials.
	u.Path, u.RawPath = "", ""
	u.Host = strings.ToLower(u.Host)
	if u.Port() == "443" {
		u.Host = strings.TrimSuffix(u.Host, ":443")
	}
	return u.String(), nil
}

func (m *ClientRPCMutations) createProfileAs(peer local.Peer, request *ipc.CreateProfileRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/CreateProfile", request, func(cfg *Config, op *ipc.Operation) error {
		name, err := rpcProfileDisplayName(request.DisplayName)
		if err != nil {
			return err
		}
		origin, err := rpcProfileOrigin(request.ControlOrigin)
		if err != nil {
			return err
		}
		state := cfg.RPCState
		if len(state.Profiles) >= maxRPCProfiles {
			return rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		if state.Profiles == nil {
			state.Profiles = map[string]clientRPCProfile{}
		}
		id, err := newRPCUUID()
		if err != nil {
			return err
		}
		state.Profiles[id] = clientRPCProfile{ID: id, DisplayName: name, ControlOrigin: origin}
		op.ProfileId = id
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE
		op.Outcome = &ipc.Operation_Selection{Selection: &ipc.SelectionResult{SelectedId: id}}
		return nil
	}, true)
	return op, err
}

func rpcFindProfile(cfg *Config, ref *ipc.ProfileRef) (clientRPCProfile, error) {
	if ref.GetProfileId() == "" {
		return clientRPCProfile{}, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	profile, exists := cfg.RPCState.Profiles[ref.ProfileId]
	if !exists {
		return clientRPCProfile{}, rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	}
	return profile, nil
}

func (m *ClientRPCMutations) renameProfileAs(peer local.Peer, request *ipc.RenameProfileRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/RenameProfile", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		name, err := rpcProfileDisplayName(request.DisplayName)
		if err != nil {
			return err
		}
		changed := name != profile.DisplayName
		profile.DisplayName = name
		cfg.RPCState.Profiles[profile.ID] = profile
		op.ProfileId = profile.ID
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE
		op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: changed}}
		return nil
	}, true)
	return op, err
}

func (m *ClientRPCMutations) removeProfileAs(peer local.Peer, request *ipc.RemoveProfileRequest) (*ipc.Operation, error) {
	op, _, err := m.acceptInternal(peer, "/client.v0.ClientService/RemoveProfile", request, func(cfg *Config, op *ipc.Operation) error {
		profile, err := rpcFindProfile(cfg, request.Profile)
		if err != nil {
			return err
		}
		if cfg.RPCState.ActiveProfileID == profile.ID {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_PROFILE_ACTIVE)
		}
		if rpcConfigHasEnrollment(profile.Configuration) {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_REMOTE_CLEANUP_REQUIRED)
		}
		for _, record := range cfg.RPCState.Operations {
			pending := new(ipc.Operation)
			if err := proto.Unmarshal(record.Operation, pending); err != nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if pending.ProfileId == profile.ID && !rpcOperationTerminal(pending.State) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_BUSY)
			}
		}
		delete(cfg.RPCState.Profiles, profile.ID)
		op.ProfileId = profile.ID
		op.Continuity = ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE
		op.Outcome = &ipc.Operation_Change{Change: &ipc.ChangeResult{Changed: true}}
		return nil
	}, true)
	return op, err
}
