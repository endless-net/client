package client

import (
	"bytes"
	"testing"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCStateRequiresPrivateConfigPermissionsBeforeEnrollment(t *testing.T) {
	if !configContainsSecrets(Config{RPCState: &ClientRPCState{Enrollment: &clientRPCEnrollment{Token: "synthetic-token"}}}) ||
		!configContainsSecrets(Config{RPCState: &ClientRPCState{}}) ||
		!configContainsSecrets(Config{PendingDirectRegistration: &PendingDirectRegistration{}}) {
		t.Fatal("protected runtime state classified as public config")
	}
}

func enrollmentAdmissionTest(t *testing.T) (*ClientRPCMutations, local.Peer, *ipc.EnrollRequest) {
	t.Helper()
	m := newRPCStoreTest(t)
	peer := local.Peer{Identity: "uid:1000"}
	create := rpcCreateRequest(t, m)
	create.ControlOrigin = "https://control.test"
	created, err := m.createProfileAs(peer, create)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.RPCState.ActiveProfileID = created.ProfileId
		cfg.ControlPlaneURLs = []string{create.ControlOrigin}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m, peer, &ipc.EnrollRequest{Mutation: rpcCreateRequest(t, m).Mutation,
		Profile: &ipc.ProfileRef{ProfileId: created.ProfileId}, Mode: ipc.EnrollmentMode_ENROLLMENT_MODE_WORKSTATION,
		Authentication: &ipc.EnrollRequest_EnrollmentToken{EnrollmentToken: "synthetic-enrollment-secret"}}
}

func TestRPCEnrollmentAdmissionDurableReplayAndSecretCleanup(t *testing.T) {
	m, peer, req := enrollmentAdmissionTest(t)
	op, err := m.enrollAs(peer, req)
	if err != nil {
		t.Fatal(err)
	}
	if plan := m.store.Read().RPCState.Enrollment; plan == nil || plan.OperationID != op.Id || plan.Token != req.GetEnrollmentToken() {
		t.Fatal("accepted enrollment plan not persisted")
	}
	disk, err := loadConfigFile(m.store.path)
	if err != nil || disk.RPCState == nil || disk.RPCState.Enrollment == nil || disk.RPCState.Enrollment.OperationID != op.Id {
		t.Fatal("accepted enrollment plan missing from disk")
	}
	encoded, err := proto.Marshal(op)
	if err != nil || bytes.Contains(encoded, []byte(req.GetEnrollmentToken())) {
		t.Fatal("operation exposed enrollment authorization")
	}
	retry, err := m.enrollAs(peer, req)
	if err != nil || !proto.Equal(op, retry) {
		t.Fatal("retry did not return the original acceptance")
	}
	changed := proto.Clone(req).(*ipc.EnrollRequest)
	changed.Hostname = "different-input"
	if _, err := m.enrollAs(peer, changed); err == nil {
		t.Fatal("accepted changed input under the same request ID")
	}
	_, err = m.ReconcileOperation(op.Id, func(_ *Config, op *ipc.Operation) error {
		op.State = ipc.OperationState_OPERATION_STATE_CANCELLED
		op.Outcome = &ipc.Operation_Failure{Failure: &ipc.Failure{Code: ipc.ErrorCode_ERROR_CODE_CANCELLED}}
		return nil
	})
	if err != nil || m.store.Read().RPCState.Enrollment != nil {
		t.Fatal("terminal operation retained enrollment plan")
	}
}

func TestRPCEnrollmentAdmissionRejectsInvalidInputWithoutMutation(t *testing.T) {
	cases := map[string]func(*ipc.EnrollRequest){
		"mode":          func(req *ipc.EnrollRequest) { req.Mode = ipc.EnrollmentMode_ENROLLMENT_MODE_UNSPECIFIED },
		"hostname":      func(req *ipc.EnrollRequest) { req.Hostname = "bad\nname" },
		"no auth":       func(req *ipc.EnrollRequest) { req.Authentication = nil },
		"false browser": func(req *ipc.EnrollRequest) { req.Authentication = &ipc.EnrollRequest_BrowserLogin{} },
		"blank token": func(req *ipc.EnrollRequest) {
			req.Authentication = &ipc.EnrollRequest_EnrollmentToken{EnrollmentToken: " "}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m, peer, req := enrollmentAdmissionTest(t)
			before := m.Metadata().Revision
			mutate(req)
			_, err := m.enrollAs(peer, req)
			assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
			if m.store.Read().RPCState.Enrollment != nil || m.Metadata().Revision != before {
				t.Fatal("invalid request mutated runtime")
			}
		})
	}
}

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
