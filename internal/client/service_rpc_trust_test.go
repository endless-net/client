package client

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCTrustAdmissionPreservesAuthorityAndConfirmation(t *testing.T) {
	m, peer, profile := rpcConnectFixture(t)
	peer.Administrator = true
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(public))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error {
		cfg.MapSigningTrust = &trusted
		cfg.NodeCredential = "synthetic-current-authority"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	req := &ipc.TrustServerIdentityRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile,
		ConfirmedControlOrigin: m.store.Read().RPCState.Profiles[profile.ProfileId].ControlOrigin,
		ConfirmedKeyId:         "announced-key", ConfirmedAnnouncementId: strings.Repeat("a", 64)}
	before := m.Metadata().Revision
	ownerOnly := peer
	ownerOnly.Administrator = false
	_, err = m.trustServerIdentityAs(ownerOnly, req)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_ADMINISTRATOR_REQUIRED)
	for _, field := range []string{"origin", "key", "announcement"} {
		bad := proto.Clone(req).(*ipc.TrustServerIdentityRequest)
		switch field {
		case "origin":
			bad.ConfirmedControlOrigin = "https://wrong.test"
		case "key":
			bad.ConfirmedKeyId = ""
		case "announcement":
			bad.ConfirmedAnnouncementId = "invalid"
		}
		_, err := m.trustServerIdentityAs(peer, bad)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	if m.Metadata().Revision != before || m.store.Read().RPCState.Trust != nil {
		t.Fatal("invalid confirmation changed durable state")
	}
	accepted, err := m.trustServerIdentityAs(peer, req)
	if err != nil || accepted.State != ipc.OperationState_OPERATION_STATE_PENDING || accepted.Kind != ipc.OperationKind_OPERATION_KIND_TRUST_SERVER_IDENTITY {
		t.Fatal("trust admission failed", err)
	}
	retry, err := m.trustServerIdentityAs(peer, req)
	if err != nil || !proto.Equal(accepted, retry) {
		t.Fatal("exact confirmation retry not idempotent")
	}
	disk, err := loadConfigFile(m.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if disk.MapSigningTrust.ActiveKeyID != trusted.ActiveKeyID || disk.NodeCredential != "synthetic-current-authority" || disk.EnrollmentRecovery != nil {
		t.Fatal("admission changed authority before execution")
	}
	if disk.RPCState.Trust == nil || disk.RPCState.Trust.AnnouncementID != req.ConfirmedAnnouncementId || disk.RPCState.Trust.OperationID != accepted.Id || len(disk.RPCState.Trust.Authority) != 32 {
		t.Fatal("durable trust plan lost confirmation binding")
	}
	changed := proto.Clone(req).(*ipc.TrustServerIdentityRequest)
	changed.ConfirmedKeyId = "different-key"
	_, err = m.trustServerIdentityAs(peer, changed)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
}
