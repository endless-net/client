package client

import (
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func enrollmentCheckpointTest(t *testing.T) (*ClientRPCMutations, *ipc.Operation, Config) {
	t.Helper()
	m := newRPCStoreTest(t)
	req := &ipc.EnrollRequest{Mutation: rpcCreateRequest(t, m).Mutation}
	op, _, err := m.acceptAs(local.Peer{Identity: "uid:1000"}, "/client.v0.ClientService/Enroll", req, func(cfg *Config, op *ipc.Operation) error {
		op.ProfileId = "profile-a"
		cfg.RPCState.ActiveProfileID = op.ProfileId
		cfg.RPCState.Profiles = map[string]clientRPCProfile{op.ProfileId: {ID: op.ProfileId, ControlOrigin: "https://control.test"}}
		cfg.ControlPlaneURLs = []string{"https://control.test"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	op, err = m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_RUNNING
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m, op, m.store.Read()
}

func TestRPCEnrollmentCheckpointPreservesConcurrentIntentAndJournal(t *testing.T) {
	m, op, initial := enrollmentCheckpointTest(t)
	save := m.EnrollmentSaveCallback(op.Id, initial)
	if err := m.store.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "user_disconnect"}
		cfg.WireGuardMTU = 1400
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	initial.NodeID = "node-1"
	if err := save(initial); err != nil {
		t.Fatal(err)
	}
	initial.NodeApprovalState = "pending"
	if err := save(initial); err != nil {
		t.Fatal(err)
	}
	saved := m.store.Read()
	if saved.NodeID != "node-1" || saved.NodeApprovalState != "pending" || saved.WireGuardMTU != 1400 || saved.ConnectionIntent.Reason != "user_disconnect" ||
		saved.RPCState.Revision != op.Metadata.Revision+2 || len(saved.RPCState.Operations) != 1 {
		t.Fatal("checkpoint lost concurrent state or did not advance journal")
	}
}

func TestRPCEnrollmentCheckpointRejectsStaleContext(t *testing.T) {
	cases := map[string]func(*Config){
		"account":    func(cfg *Config) { cfg.ActiveAccountID = "other-account" },
		"management": func(cfg *Config) { cfg.ManagementURL = "https://other.test" },
		"identity":   func(cfg *Config) { cfg.IdentityPrivateKey = "synthetic-changed-identity" },
		"profile origin": func(cfg *Config) {
			profile := cfg.RPCState.Profiles["profile-a"]
			profile.ControlOrigin = "https://other.test"
			cfg.RPCState.Profiles["profile-a"] = profile
		},
		"profile":     func(cfg *Config) { cfg.RPCState.ActiveProfileID = "profile-b" },
		"origin":      func(cfg *Config) { cfg.ControlPlaneURLs = []string{"https://other.test"} },
		"owner":       func(cfg *Config) { cfg.LocalOwnerID = "uid:1001" },
		"credentials": func(cfg *Config) { cfg.NodeCredential = "synthetic-renewed-credential" },
		"switch":      func(cfg *Config) { cfg.RPCState.ProfileSwitch = &clientRPCProfileSwitch{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m, op, initial := enrollmentCheckpointTest(t)
			save := m.EnrollmentSaveCallback(op.Id, initial)
			if err := m.store.Update(func(cfg *Config) error { mutate(cfg); return nil }); err != nil {
				t.Fatal(err)
			}
			initial.NodeID = "stale-node"
			assertRPCFailure(t, save(initial), ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			if m.store.Read().NodeID != "" || m.Metadata().Revision != op.Metadata.Revision {
				t.Fatal("stale checkpoint changed durable state")
			}
		})
	}
}

func TestRPCEnrollmentCheckpointCannotWriteAfterTerminalOperation(t *testing.T) {
	m, op, initial := enrollmentCheckpointTest(t)
	save := m.EnrollmentSaveCallback(op.Id, initial)
	_, err := m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_CANCELLED
		op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_CANCELLED}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	initial.NodeID = "late-node"
	assertRPCFailure(t, save(initial), ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if m.store.Read().NodeID != "" {
		t.Fatal("late response saved")
	}
}
