package client

import (
	"context"
	"errors"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

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
