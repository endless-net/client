package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCForgetInactiveProfileDoesNotAffectActiveTunnelContext(t *testing.T) {
	m, peer, active := enrollmentAdmissionTest(t)
	create := rpcCreateRequest(t, m)
	create.ControlOrigin = "https://other.test"
	created, err := m.createProfileAs(peer, create)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.NodeID, cfg.NodeCredential = "active-node", "synthetic-active-credential"
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected, Reason: "keep-active"}
		profile := cfg.RPCState.Profiles[created.ProfileId]
		profile.Configuration = Config{NodeID: "inactive-node", NodeCredential: "synthetic-inactive-credential", Token: "synthetic-inactive-session", DeviceFingerprint: "other-binding", ControlPlaneURLs: []string{create.ControlOrigin}, EnrollmentRecovery: &EnrollmentRecovery{RequestID: "inactive-correlation"}}
		profile.UIQuit = ipc.LifecycleBehavior_LIFECYCLE_BEHAVIOR_DISCONNECT.Enum()
		cfg.RPCState.Profiles[profile.ID] = profile
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	peer.Administrator = true
	request := &ipc.ForgetLocalEnrollmentRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}, Confirmed: true}
	op, err := m.forgetEnrollmentAs(peer, request)
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.store.Read()
	forgotten := cfg.RPCState.Profiles[created.ProfileId]
	if op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || op.GetCleanup().ControlRequestId != "inactive-correlation" || op.Continuity != ipc.ConnectionContinuity_CONNECTION_CONTINUITY_PRESERVED {
		t.Fatal("inactive cleanup did not finish atomically")
	}
	if cfg.NodeID != "active-node" || cfg.NodeCredential != "synthetic-active-credential" || cfg.ConnectionIntent.Reason != "keep-active" || cfg.RPCState.ActiveProfileID != active.Profile.ProfileId || cfg.RPCState.DisconnectOperationID != "" {
		t.Fatal("inactive cleanup changed active tunnel context")
	}
	if forgotten.Configuration.NodeID != "" || forgotten.Configuration.NodeCredential != "" || forgotten.Configuration.Token != "" || forgotten.Configuration.DeviceFingerprint != "other-binding" || forgotten.UIQuit == nil || forgotten.ControlOrigin != create.ControlOrigin {
		t.Fatal("inactive cleanup violated retention matrix")
	}
	retry, err := m.forgetEnrollmentAs(peer, request)
	if err != nil || !proto.Equal(op, retry) {
		t.Fatal("inactive cleanup retry not idempotent")
	}
}

func TestRPCForgetEnrollmentStopsBeforeCleanupAndPreservesInstallation(t *testing.T) {
	for _, failStop := range []bool{false, true} {
		t.Run(map[bool]string{false: "cleanup", true: "stop failure"}[failStop], func(t *testing.T) {
			m, peer, enroll := enrollmentAdmissionTest(t)
			if err := m.store.Update(func(cfg *Config) error {
				cfg.NodeID, cfg.NodeCredential, cfg.Token = "node", "synthetic-node-credential", "synthetic-session"
				cfg.PrivateKey, cfg.IdentityPrivateKey, cfg.DeviceFingerprint = "synthetic-wg-key", "synthetic-identity-key", "device-binding"
				cfg.EnrollmentRecovery = &EnrollmentRecovery{RequestID: "control-correlation"}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			request := &ipc.ForgetLocalEnrollmentRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: enroll.Profile, Confirmed: true}
			if _, err := m.forgetEnrollmentAs(peer, request); err == nil {
				t.Fatal("non-administrator forgot registration")
			}
			peer.Administrator = true
			request.Confirmed = false
			if _, err := m.forgetEnrollmentAs(peer, request); err == nil {
				t.Fatal("unconfirmed cleanup accepted")
			}
			request.Confirmed = true
			op, err := m.forgetEnrollmentAs(peer, request)
			if err != nil {
				t.Fatal(err)
			}
			if m.store.Read().NodeCredential == "" {
				t.Fatal("credentials removed before Down")
			}
			stopCalls := 0
			err = m.ReconcileDisconnect(t.Context(), ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
				stopCalls++
				if m.store.Read().NodeCredential == "" {
					t.Error("cleanup preceded actual stop")
				}
				if failStop {
					return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, errors.New("synthetic-stop-failure")
				}
				return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := m.operationAs(peer, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
			if err != nil {
				t.Fatal(err)
			}
			cfg := m.store.Read()
			if stopCalls != 1 || cfg.LocalOwnerID != peer.Identity || cfg.PrivateKey != "synthetic-wg-key" || cfg.IdentityPrivateKey != "synthetic-identity-key" || cfg.DeviceFingerprint != "device-binding" {
				t.Fatal("cleanup changed installation identity")
			}
			if failStop {
				if result.State != ipc.OperationState_OPERATION_STATE_FAILED || cfg.NodeCredential == "" || cfg.Token == "" {
					t.Fatal("failed Down removed registration")
				}
			} else if result.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED || result.GetCleanup().Outcome != ipc.CleanupOutcome_CLEANUP_OUTCOME_REMOTE_UNCONFIRMED || result.GetCleanup().ControlRequestId != "control-correlation" || cfg.NodeID != "" || cfg.NodeCredential != "" || cfg.Token != "" || cfg.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
				t.Fatal("local cleanup matrix not applied")
			}
		})
	}
}
