package client

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/endless-net/client/clientipc/local"
	"github.com/endless-net/client/clientipc/rpc"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const rpcOperationRetention = 24 * time.Hour
const rpcMaxOperationRecords = 4096
const rpcMaxNonterminalOperations = 32

var errRPCNoChange = errors.New("RPC durable state unchanged")

// ClientRPCState is part of the protected atomic ConfigStore, never UI-readable.
// Request payloads are not retained: a keyed digest prevents token disclosure
// and offline guessing from an unkeyed enrollment-request hash.
type ClientRPCState struct {
	Revision              uint64                              `json:"revision"`
	DigestKey             []byte                              `json:"digest_key"`
	Operations            map[string]clientRPCOperationRecord `json:"operations"`
	Profiles              map[string]clientRPCProfile         `json:"profiles,omitempty"`
	ActiveProfileID       string                              `json:"active_profile_id,omitempty"`
	ProfileSwitch         *clientRPCProfileSwitch             `json:"profile_switch,omitempty"`
	DisconnectOperationID string                              `json:"disconnect_operation_id,omitempty"`
}

type clientRPCOperationRecord struct {
	Owner       string     `json:"owner"`
	Digest      []byte     `json:"digest"`
	Operation   []byte     `json:"operation"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// ClientRPCMutations coordinates v0 durable acceptance. Domain preparation must
// mutate only the supplied config: no network, device or other external effects
// may run until acceptance commits. Runtime reconciliation executes those effects.
type ClientRPCMutations struct {
	mu               sync.Mutex
	profileWorker    sync.Mutex
	disconnectWorker sync.Mutex
	store            *ConfigStore
	instanceID       string
	now              func() time.Time
	observedStatus   *ipc.Status
	subscribers      map[*rpcSubscriber]struct{}
}

func NewClientRPCMutations(store *ConfigStore) (*ClientRPCMutations, error) {
	if store == nil {
		return nil, errors.New("RPC config store is required")
	}
	instance, err := newRPCUUID()
	if err != nil {
		return nil, err
	}
	return &ClientRPCMutations{store: store, instanceID: instance, now: time.Now}, nil
}

func newRPCUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 15) | 64
	value[8] = (value[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

func validRPCUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil || len(raw) != 16 {
		return false
	}
	return strings.Trim(strings.ReplaceAll(value, "-", ""), "0") != ""
}

func rpcMethod(procedure string) protoreflect.MethodDescriptor {
	const prefix = "/client.v0.ClientService/"
	if !strings.HasPrefix(procedure, prefix) {
		return nil
	}
	return ipc.File_client_v0_service_proto.Services().ByName("ClientService").Methods().ByName(protoreflect.Name(strings.TrimPrefix(procedure, prefix)))
}

func rpcConfigHasEnrollment(cfg Config) bool {
	return cfg.Token != "" || cfg.ActiveAccountID != "" || cfg.NodeID != "" || cfg.NetworkID != "" ||
		cfg.NodeCredential != "" || cfg.NodeApprovalState != "" || cfg.EnrollmentRequestID != "" ||
		cfg.EnrollmentPollToken != "" || cfg.EnrollmentRequest != nil || cfg.PendingDirectRegistration != nil || cfg.CachedMap != nil
}

func authorizeRPCPeer(peer local.Peer, method protoreflect.MethodDescriptor, cfg Config) error {
	if peer.Identity == "" {
		return rpc.Error(connect.CodeUnauthenticated, ipc.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	}
	if method == nil {
		return rpc.Error(connect.CodeUnimplemented, ipc.ErrorCode_ERROR_CODE_UNSUPPORTED)
	}
	access := proto.GetExtension(method.Options(), ipc.E_Access).(ipc.Access)
	if access == ipc.Access_ACCESS_OBSERVER || peer.Administrator {
		return nil
	}
	if access == ipc.Access_ACCESS_ADMINISTRATOR {
		return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_ADMINISTRATOR_REQUIRED)
	}
	if access != ipc.Access_ACCESS_OWNER {
		return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	}
	if cfg.LocalOwnerID != "" && strings.EqualFold(cfg.LocalOwnerID, peer.Identity) {
		return nil
	}
	claim := proto.GetExtension(method.Options(), ipc.E_AllowsInitialOwnershipClaim).(bool)
	if claim && cfg.LocalOwnerID == "" {
		if rpcConfigHasEnrollment(cfg) {
			return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_ADMINISTRATOR_REQUIRED)
		}
		return nil
	}
	return rpc.Error(connect.CodePermissionDenied, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
}

// Authorize is installed in rpc.Guard. Acceptance repeats this check under the
// durable store lock so two unowned callers cannot both claim the installation.
func (m *ClientRPCMutations) Authorize(ctx context.Context, _ ipc.Access, procedure string) error {
	peer, _ := local.PeerFromContext(ctx)
	return authorizeRPCPeer(peer, rpcMethod(procedure), m.store.Read())
}

func (m *ClientRPCMutations) Metadata() *ipc.SnapshotMetadata {
	m.mu.Lock()
	defer m.mu.Unlock()
	revision := uint64(1)
	if state := m.store.Read().RPCState; state != nil {
		revision = state.Revision
	}
	return &ipc.SnapshotMetadata{InstanceId: m.instanceID, Revision: revision, GeneratedAt: timestamppb.New(m.now())}
}

func (m *ClientRPCMutations) Accept(ctx context.Context, procedure string, request proto.Message, prepare func(*Config, *ipc.Operation) error) (*ipc.Operation, bool, error) {
	peer, _ := local.PeerFromContext(ctx)
	return m.acceptAs(peer, procedure, request, prepare)
}

func (m *ClientRPCMutations) acceptAs(peer local.Peer, procedure string, request proto.Message, prepare func(*Config, *ipc.Operation) error) (*ipc.Operation, bool, error) {
	return m.acceptInternal(peer, procedure, request, prepare, false)
}

// Immediate effects are limited to this same durable config transaction. This
// must never be used for network/device effects that can outlive a failed save.
func (m *ClientRPCMutations) acceptInternal(peer local.Peer, procedure string, request proto.Message, prepare func(*Config, *ipc.Operation) error, immediate bool) (*ipc.Operation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	method := rpcMethod(procedure)
	var accepted *ipc.Operation
	var reused bool
	err := m.store.Update(func(cfg *Config) error {
		if err := authorizeRPCPeer(peer, method, *cfg); err != nil {
			return err
		}
		if request == nil || !request.ProtoReflect().IsValid() || request.ProtoReflect().Descriptor().FullName() != method.Input().FullName() || prepare == nil {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		field := method.Input().Fields().ByName("mutation")
		kind := proto.GetExtension(method.Options(), ipc.E_OperationKind).(ipc.OperationKind)
		if field == nil || kind == ipc.OperationKind_OPERATION_KIND_UNSPECIFIED {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		mutation := request.ProtoReflect().Get(field).Message().Interface().(*ipc.MutationContext)
		if !validRPCUUID(mutation.GetRequestId()) || mutation.GetExpectedInstanceId() == "" || mutation.GetExpectedRevision() == 0 {
			return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
		}
		if cfg.RPCState == nil {
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return err
			}
			cfg.RPCState = &ClientRPCState{Revision: 1, DigestKey: key, Operations: map[string]clientRPCOperationRecord{}}
		}
		state := cfg.RPCState
		if state.Revision == 0 || len(state.DigestKey) != 32 || state.Operations == nil {
			return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		}
		payload := proto.Clone(request)
		payload.ProtoReflect().Clear(field) // CAS metadata is not semantic payload.
		wire, err := (proto.MarshalOptions{Deterministic: true}).Marshal(payload)
		if err != nil {
			return err
		}
		hash := hmac.New(sha256.New, state.DigestKey)
		_, _ = hash.Write([]byte(procedure + "\x00"))
		_, _ = hash.Write(wire)
		digest := hash.Sum(nil)
		requestID := strings.ToLower(mutation.RequestId)
		now := m.now()
		for id, record := range state.Operations {
			if record.CompletedAt != nil && !now.Before(record.CompletedAt.Add(rpcOperationRetention)) {
				delete(state.Operations, id)
			}
		}
		if record, exists := state.Operations[requestID]; exists {
			if !strings.EqualFold(record.Owner, peer.Identity) {
				return rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
			}
			if !hmac.Equal(digest, record.Digest) {
				return rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
			}
			accepted = new(ipc.Operation)
			if err := proto.Unmarshal(record.Operation, accepted); err != nil {
				return err
			}
			reused = true
			return errRPCNoChange // A retry must not depend on another disk write.
		}
		if mutation.ExpectedInstanceId != m.instanceID || mutation.ExpectedRevision != state.Revision {
			return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		}
		if len(state.Operations) >= rpcMaxOperationRecords && kind != ipc.OperationKind_OPERATION_KIND_DISCONNECT {
			return rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		// Check before domain preparation and ownership changes. Retries were
		// resolved above and must remain readable even at capacity.
		nonterminal := 0
		ordinary := 0
		for _, record := range state.Operations {
			op := new(ipc.Operation)
			if err := proto.Unmarshal(record.Operation, op); err != nil {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if !rpcOperationTerminal(op.State) {
				nonterminal++
				if op.Kind != ipc.OperationKind_OPERATION_KIND_DISCONNECT {
					ordinary++
				}
			}
		}
		// Reserve one slot without double-reserving it when Disconnect is active.
		if nonterminal >= rpcMaxNonterminalOperations || (kind != ipc.OperationKind_OPERATION_KIND_DISCONNECT && ordinary >= rpcMaxNonterminalOperations-1) {
			return rpc.Error(connect.CodeResourceExhausted, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
		}
		if cfg.LocalOwnerID == "" && proto.GetExtension(method.Options(), ipc.E_AllowsInitialOwnershipClaim).(bool) {
			cfg.LocalOwnerID = peer.Identity
		}
		id, err := newRPCUUID()
		if err != nil {
			return err
		}
		accepted = &ipc.Operation{Id: id, RequestId: requestID, Kind: kind, State: ipc.OperationState_OPERATION_STATE_PENDING,
			Continuity: ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN}
		owner := cfg.LocalOwnerID
		if err := prepare(cfg, accepted); err != nil {
			return err
		}
		// Preparation cannot change identity or the lifecycle managed here.
		if cfg.RPCState != state || cfg.LocalOwnerID != owner || accepted.Id != id || accepted.RequestId != requestID || accepted.Kind != kind || accepted.State != ipc.OperationState_OPERATION_STATE_PENDING || accepted.UserAction != nil || (!immediate && accepted.Outcome != nil) {
			return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
		}
		var completedAt *time.Time
		if immediate {
			previous := proto.Clone(accepted).(*ipc.Operation)
			previous.State = ipc.OperationState_OPERATION_STATE_RUNNING
			previous.Outcome = nil
			accepted.State = ipc.OperationState_OPERATION_STATE_SUCCEEDED
			if !validRPCOperationTransition(previous, accepted) {
				return rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			completedAt = &now
		}
		state.Revision++
		accepted.Metadata = &ipc.SnapshotMetadata{InstanceId: m.instanceID, Revision: state.Revision, GeneratedAt: timestamppb.New(now)}
		encoded, err := proto.Marshal(accepted)
		if err != nil {
			return err
		}
		state.Operations[requestID] = clientRPCOperationRecord{Owner: peer.Identity, Digest: digest, Operation: encoded, CompletedAt: completedAt}
		return nil
	})
	if err != nil && !errors.Is(err, errRPCNoChange) {
		return nil, false, err
	}
	if !reused {
		m.publishMutationLocked(accepted)
	}
	return accepted, reused, nil
}

func (m *ClientRPCMutations) GetOperation(ctx context.Context, request *ipc.GetOperationRequest) (*ipc.Operation, error) {
	peer, _ := local.PeerFromContext(ctx)
	return m.operationAs(peer, request)
}

func (m *ClientRPCMutations) operationAs(peer local.Peer, request *ipc.GetOperationRequest) (*ipc.Operation, error) {
	cfg := m.store.Read()
	if err := authorizeRPCPeer(peer, rpcMethod("/client.v0.ClientService/GetOperation"), cfg); err != nil {
		return nil, err
	}
	if request == nil || (request.GetOperationId() == "" && request.GetRequestId() == "") {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if cfg.RPCState != nil {
		for id, record := range cfg.RPCState.Operations {
			if !strings.EqualFold(record.Owner, peer.Identity) || (record.CompletedAt != nil && !m.now().Before(record.CompletedAt.Add(rpcOperationRetention))) {
				continue
			}
			operation := new(ipc.Operation)
			if err := proto.Unmarshal(record.Operation, operation); err != nil {
				return nil, rpc.Error(connect.CodeInternal, ipc.ErrorCode_ERROR_CODE_INTERNAL)
			}
			if (request.GetRequestId() != "" && strings.EqualFold(request.GetRequestId(), id)) || request.GetOperationId() == operation.Id {
				return operation, nil
			}
		}
	}
	return nil, rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
}

func rpcOperationTerminal(state ipc.OperationState) bool {
	return state == ipc.OperationState_OPERATION_STATE_SUCCEEDED || state == ipc.OperationState_OPERATION_STATE_FAILED || state == ipc.OperationState_OPERATION_STATE_CANCELLED
}

func validRPCOperationTransition(previous, next *ipc.Operation) bool {
	if rpcOperationTerminal(previous.State) || previous.Id != next.Id || previous.Kind != next.Kind || previous.RequestId != next.RequestId || previous.ProfileId != next.ProfileId {
		return false
	}
	allowed := false
	switch previous.State {
	case ipc.OperationState_OPERATION_STATE_PENDING:
		allowed = next.State == ipc.OperationState_OPERATION_STATE_RUNNING || next.State == ipc.OperationState_OPERATION_STATE_FAILED || next.State == ipc.OperationState_OPERATION_STATE_CANCELLED
	case ipc.OperationState_OPERATION_STATE_RUNNING:
		allowed = next.State == ipc.OperationState_OPERATION_STATE_RUNNING || next.State == ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER || rpcOperationTerminal(next.State)
	case ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER:
		allowed = next.State == ipc.OperationState_OPERATION_STATE_RUNNING || next.State == ipc.OperationState_OPERATION_STATE_FAILED || next.State == ipc.OperationState_OPERATION_STATE_CANCELLED
	}
	if !allowed {
		return false
	}
	if next.State == ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER && (next.UserAction == nil || next.UserAction.Kind == ipc.UserAction_KIND_UNSPECIFIED) {
		return false
	}
	if next.State != ipc.OperationState_OPERATION_STATE_WAITING_FOR_USER && next.UserAction != nil {
		return false
	}
	if !rpcOperationTerminal(next.State) {
		return next.Outcome == nil
	}
	if next.State == ipc.OperationState_OPERATION_STATE_FAILED || next.State == ipc.OperationState_OPERATION_STATE_CANCELLED {
		return next.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_UNSPECIFIED
	}
	if next.Continuity == ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNSPECIFIED {
		return false
	}
	switch next.Kind {
	case ipc.OperationKind_OPERATION_KIND_ENROLL:
		return next.GetEnrollment() != nil
	case ipc.OperationKind_OPERATION_KIND_CREATE_PROFILE, ipc.OperationKind_OPERATION_KIND_SELECT_PROFILE, ipc.OperationKind_OPERATION_KIND_SELECT_NETWORK, ipc.OperationKind_OPERATION_KIND_SELECT_EXIT_NODE, ipc.OperationKind_OPERATION_KIND_CLEAR_EXIT_NODE:
		return next.GetSelection() != nil
	case ipc.OperationKind_OPERATION_KIND_LOGOUT, ipc.OperationKind_OPERATION_KIND_FORGET_LOCAL_ENROLLMENT:
		return next.GetCleanup() != nil
	case ipc.OperationKind_OPERATION_KIND_RENEW_SESSION:
		return next.GetRenewal() != nil
	case ipc.OperationKind_OPERATION_KIND_CREATE_DIAGNOSTICS_BUNDLE:
		return next.GetBundle() != nil
	default:
		return next.GetChange() != nil
	}
}

// ReconcileOperation is called only by runtime providers, never directly by an
// RPC caller. Persist result/state together; external effects precede terminal
// success. Failed persistence leaves the prior durable operation recoverable.
func (m *ClientRPCMutations) ReconcileOperation(id string, apply func(*Config, *ipc.Operation) error) (*ipc.Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	previousActive := ""
	if state := m.store.Read().RPCState; state != nil {
		previousActive = state.ActiveProfileID
	}
	if id == "" || apply == nil {
		return nil, rpc.Error(connect.CodeInvalidArgument, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	var updated *ipc.Operation
	err := m.store.Update(func(cfg *Config) error {
		if cfg.RPCState == nil {
			return rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
		}
		for requestID, record := range cfg.RPCState.Operations {
			previous := new(ipc.Operation)
			if err := proto.Unmarshal(record.Operation, previous); err != nil {
				return err
			}
			if previous.Id != id {
				continue
			}
			updated = proto.Clone(previous).(*ipc.Operation)
			if err := apply(cfg, updated); err != nil {
				return err
			}
			if !validRPCOperationTransition(previous, updated) {
				return rpc.Error(connect.CodeFailedPrecondition, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			}
			cfg.RPCState.Revision++
			now := m.now()
			updated.Metadata = &ipc.SnapshotMetadata{InstanceId: m.instanceID, Revision: cfg.RPCState.Revision, GeneratedAt: timestamppb.New(now)}
			if rpcOperationTerminal(updated.State) {
				record.CompletedAt = &now
			}
			encoded, err := proto.Marshal(updated)
			if err != nil {
				return err
			}
			record.Operation = encoded
			cfg.RPCState.Operations[requestID] = record
			return nil
		}
		return rpc.Error(connect.CodeNotFound, ipc.ErrorCode_ERROR_CODE_NOT_FOUND)
	})
	if err != nil {
		return nil, err
	}
	if state := m.store.Read().RPCState; state != nil && state.ActiveProfileID != previousActive {
		m.observedStatus = nil
	}
	if updated.Kind == ipc.OperationKind_OPERATION_KIND_DISCONNECT {
		if m.observedStatus == nil {
			m.observedStatus = &ipc.Status{}
		}
		m.observedStatus.ConnectionPhase = ipc.ConnectionPhase_CONNECTION_PHASE_UNSPECIFIED
		if updated.State == ipc.OperationState_OPERATION_STATE_RUNNING {
			m.observedStatus.ConnectionPhase = ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTING
		}
		if updated.State == ipc.OperationState_OPERATION_STATE_SUCCEEDED {
			m.observedStatus.ConnectionPhase = ipc.ConnectionPhase_CONNECTION_PHASE_DISCONNECTED
		}
	}
	m.publishMutationLocked(updated)
	return updated, nil
}
