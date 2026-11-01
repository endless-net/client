//go:build windows && integration

package state

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	approvalmodel "github.com/endless-net/client/internal/approval"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func TestWindowsProtectedApprovalState(t *testing.T) {
	if testing.Short() {
		t.Skip("explicit isolated Windows DPAPI browser-state fixture")
	}
	directory, sid := protectedDirectory(t)
	b, err := OpenWindows(directory, sid, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	protector, err := NewDPAPI(installation)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(b, protector, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	token := "poll_" + strings.Repeat("S", 43)
	err = s.Update(ctx, 1, func(s *State) error {
		if err := addAttempt(s); err != nil {
			return err
		}
		p := s.Profiles[profileID]
		s.OwnerSID = sid
		p.Attempt.Owner = sid
		now := p.Attempt.CreatedAt
		a, err := p.Attempt.LoginSucceeded(now)
		if err != nil {
			return err
		}
		a, err = a.BeginBrowserApproval("", "", now)
		if err != nil {
			return err
		}
		p.Attempt = &a
		p.Hostname = "browser-state-fixture"
		p.Secrets.RegistrationID = "cc32812c-9386-4847-80d9-62a195fe5b8c"
		wg, err := ecdh.X25519().NewPrivateKey(p.Keys.WireGuardPrivate)
		if err != nil {
			return err
		}
		hash, err := p.Keys.Fingerprint()
		if err != nil {
			return err
		}
		q := coordinator.CreateNodeEnrollmentRequest{SchemaVersion: 2, IdempotencyID: p.Secrets.RegistrationID, NetworkName: "Requested network", Hostname: p.Hostname, PublicKey: base64.StdEncoding.EncodeToString(wg.PublicKey().Bytes()), DeviceFingerprint: hex.EncodeToString(hash[:])}
		if err := coordinator.SetRegisterNodeIdentityProof(&q, ed25519.PrivateKey(p.Keys.IdentityPrivate)); err != nil {
			return err
		}
		raw, err := json.Marshal(q)
		if err != nil {
			return err
		}
		p.Secrets.RegistrationRequest = string(raw)
		p.Approval = &ApprovalProgress{Base: "https://approval.example/", Snapshot: &approvalmodel.Snapshot{PollToken: token, PollAfterSeconds: 2, ObservedAt: now, Request: coordinator.NodeEnrollmentRequest{ID: "original-request", Status: "pending", NetworkName: q.NetworkName, Hostname: q.Hostname, PublicKey: q.PublicKey, IdentityPublicKey: q.IdentityPublicKey, DeviceFingerprint: q.DeviceFingerprint, CreatedAt: now, ExpiresAt: now.Add(600 * time.Second), ApprovalURL: "https://approval.example/?enrollment_request=original-request"}}}
		s.Profiles[profileID] = p
		return nil
	})
	if err != nil {
		t.Fatal("protected approval state not saved", err)
	}
	before, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, "state.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(token)) || bytes.Contains(raw, []byte(before.Profiles[profileID].Secrets.RegistrationRequest)) {
		t.Fatal("DPAPI file exposed browser secrets")
	}
	b2, err := OpenWindows(directory, sid, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b2.Close() }()
	reopened, err := New(b2, protector, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.Load(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("Windows reopen lost browser proof/token/deadline/identity", err)
	}
}
