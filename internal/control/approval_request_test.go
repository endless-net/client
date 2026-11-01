package control

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func approvalProfile(t *testing.T, assigned bool) state.Profile {
	t.Helper()
	f := newRegistrationFixture(t)
	p := f.p
	p.RemoteInFlight = false
	if !assigned {
		p.Attempt.Phase = enrollment.ChoosingContext
		p.Attempt.AccountID, p.Attempt.NetworkID = "", ""
	}
	return p
}

func TestPrepareApprovalPreservesDeviceAndOriginalOperationWithoutCredentials(t *testing.T) {
	for _, assigned := range []bool{false, true} {
		p := approvalProfile(t, assigned)
		target := ApprovalTarget{NetworkName: "Рабочая сеть"}
		if assigned {
			target.AccountID, target.NetworkID = p.Attempt.AccountID, p.Attempt.NetworkID
		}
		p.Secrets.PollProof = strings.Repeat("poll-secret", 5)
		p.Secrets.NodeCredential = strings.Repeat("node-secret", 5)
		before := *p.Attempt
		raw, err := PrepareApproval(p, target)
		if err != nil {
			t.Fatal(err)
		}
		q, err := coordinator.DecodeCreateNodeEnrollmentRequest(raw, len(raw))
		if err != nil || coordinator.VerifyRegisterNodeIdentityProof(q) != nil {
			t.Fatal("published browser request/proof invalid", err)
		}
		wg, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
		if err != nil {
			t.Fatal(err)
		}
		if q.IdempotencyID != p.Secrets.RegistrationID || q.PublicKey != base64.StdEncoding.EncodeToString(wg.PublicKey().Bytes()) || q.Hostname != p.Hostname || q.AccountID != target.AccountID || q.NetworkID != target.NetworkID || q.NetworkName != target.NetworkName || *p.Attempt != before {
			t.Fatal("original operation, device or intent changed")
		}
		for _, secret := range []string{p.Secrets.Access, p.Secrets.PollProof, p.Secrets.NodeCredential} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatal("credential escaped device-only request")
			}
		}
		if q.JoinToken != "" || q.NodeCredential != "" || q.RegistrationBinding != "" || q.SessionTokenBinding != "" {
			t.Fatal("mixed credential classes")
		}
		p.Secrets.RegistrationRequest = string(raw)
		p.RemoteInFlight = true
		replay, err := PrepareApproval(p, target)
		if err != nil || !bytes.Equal(raw, replay) {
			t.Fatal("frozen request changed on replay", err)
		}
		// Returned bytes are detached from the private saved replay input.
		replay[0] = 'x'
		again, err := PrepareApproval(p, target)
		if err != nil || !bytes.Equal(raw, again) {
			t.Fatal("caller mutated saved input")
		}
	}
}

func TestPrepareApprovalRejectsRebindingAndInvalidDeviceBeforeAnySend(t *testing.T) {
	for _, name := range []string{"direct", "inflight", "foreign-target", "changed-target", "changed-host", "changed-key", "bad-wg", "invalid-utf8", "control", "missing-target", "terminal", "operation"} {
		t.Run(name, func(t *testing.T) {
			p := approvalProfile(t, true)
			target := ApprovalTarget{AccountID: p.Attempt.AccountID, NetworkID: p.Attempt.NetworkID}
			raw, err := PrepareApproval(p, target)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "direct":
				f := newRegistrationFixture(t)
				p.Secrets.RegistrationRequest = string(f.original)
			case "inflight":
				p.RemoteInFlight = true
			case "foreign-target":
				target.AccountID = "foreign"
			case "changed-target":
				p.Secrets.RegistrationRequest = string(raw)
				target.NetworkName = "changed"
			case "changed-host":
				p.Secrets.RegistrationRequest = string(raw)
				p.Hostname = "changed"
			case "changed-key":
				p.Keys.IdentityPrivate = bytes.Repeat([]byte{1}, 64)
			case "bad-wg":
				p.Keys.WireGuardPrivate = []byte{1}
			case "invalid-utf8":
				target.NetworkName = string([]byte{0xff})
			case "control":
				target.NetworkName = "network\nforeign"
			case "missing-target":
				target = ApprovalTarget{}
			case "terminal":
				p.Attempt.Phase = enrollment.Cancelled
			case "operation":
				p.Secrets.RegistrationID = "not-an-original-operation"
			}
			if raw, err := PrepareApproval(p, target); err != ErrRegistrationProtocol || raw != nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}
