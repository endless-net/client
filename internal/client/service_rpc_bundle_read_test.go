package client

import (
	"context"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCBundleReadRequiresPublishedOwnerResult(t *testing.T) {
	for _, mode := range []string{"valid", "observer", "owner-changed", "orphan", "pending", "wrong-kind", "foreign-result", "metadata-mismatch", "removed", "logout", "forget", "other-profile-logout", "expired", "cancelled", "unconfigured"} {
		t.Run(mode, func(t *testing.T) {
			m, peer, profile := rpcConnectFixture(t)
			s := NewClientRPCService(m, nil)
			now := time.Now()
			s.bundleStore = &clientRPCBundleStore{now: func() time.Time { return now }}
			metadata, err := s.bundleStore.put(peer.Identity, profile.ProfileId, []byte("archive"))
			if err != nil {
				t.Fatal(err)
			}
			if err := m.store.Update(func(cfg *Config) error {
				op := &ipc.Operation{Id: "bundle-operation", ProfileId: profile.ProfileId, Kind: ipc.OperationKind_OPERATION_KIND_CREATE_DIAGNOSTICS_BUNDLE,
					State: ipc.OperationState_OPERATION_STATE_SUCCEEDED, Metadata: &ipc.SnapshotMetadata{Revision: cfg.RPCState.Revision}, Outcome: &ipc.Operation_Bundle{Bundle: metadata}}
				owner := peer.Identity
				switch mode {
				case "owner-changed":
					cfg.LocalOwnerID = "new-owner"
				case "pending":
					op.State = ipc.OperationState_OPERATION_STATE_PENDING
				case "wrong-kind":
					op.Kind = ipc.OperationKind_OPERATION_KIND_CONNECT
				case "foreign-result":
					owner = "foreign-owner"
				case "metadata-mismatch":
					metadata.SizeBytes++
				case "removed":
					delete(cfg.RPCState.Profiles, profile.ProfileId)
				}
				encoded, err := proto.Marshal(op)
				if err != nil {
					return err
				}
				if mode != "orphan" {
					cfg.RPCState.Operations["bundle-request"] = clientRPCOperationRecord{Owner: owner, Operation: encoded}
				}
				if mode == "logout" || mode == "forget" || mode == "other-profile-logout" {
					kind := ipc.OperationKind_OPERATION_KIND_LOGOUT
					if mode == "forget" {
						kind = ipc.OperationKind_OPERATION_KIND_FORGET_LOCAL_ENROLLMENT
					}
					profileID := profile.ProfileId
					if mode == "other-profile-logout" {
						profileID = "other"
					}
					encoded, err = proto.Marshal(&ipc.Operation{Kind: kind, ProfileId: profileID, State: ipc.OperationState_OPERATION_STATE_PENDING, Metadata: &ipc.SnapshotMetadata{Revision: cfg.RPCState.Revision + 1}})
					if err != nil {
						return err
					}
					cfg.RPCState.Operations["cleanup-request"] = clientRPCOperationRecord{Owner: owner, Operation: encoded}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "observer" {
				peer = local.Peer{Identity: "observer"}
			}
			if mode == "expired" {
				now = now.Add(15 * time.Minute)
			}
			if mode == "cancelled" {
				cancel()
			}
			if mode == "unconfigured" {
				s.bundleStore = nil
			}
			result, err := s.readDiagnosticsBundleAs(ctx, peer, &ipc.ReadDiagnosticsBundleRequest{BundleId: metadata.BundleId, Offset: 2, MaxBytes: 3})
			switch mode {
			case "valid", "other-profile-logout":
				if err != nil || string(result.GetData()) != "chi" || result.NextOffset != 5 || result.Eof {
					t.Fatal("invalid published chunk", err)
				}
			case "cancelled":
				if err != context.Canceled || result != nil {
					t.Fatal("cancelled read returned bytes", err)
				}
			default:
				code := ipc.ErrorCode_ERROR_CODE_NOT_FOUND
				if mode == "observer" || mode == "owner-changed" {
					code = ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED
				}
				if mode == "unconfigured" {
					code = ipc.ErrorCode_ERROR_CODE_UNSUPPORTED
				}
				assertRPCFailure(t, err, code)
				if result != nil {
					t.Fatal("denied read returned bytes")
				}
			}
		})
	}
}
