package control

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/endless-net/client/internal/enrollment"
	"github.com/endless-net/client/internal/state"
)

func saveApprovalProfile(t *testing.T, f *approvalFixture, p state.Profile) error {
	t.Helper()
	s, err := f.store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return f.store.Update(context.Background(), s.Revision, func(s *state.State) error { s.Profiles[p.ID] = p; return nil })
}

func newApprovalStateFixture(t *testing.T, assigned bool) (approvalFixture, ApprovalTarget) {
	t.Helper()
	f := newApprovalFixture(t, assigned)
	p := f.p
	p.RemoteInFlight, p.InFlightAt = false, time.Time{}
	p.Secrets.RegistrationRequest = ""
	if err := saveApprovalProfile(t, &f, p); err != nil {
		t.Fatal(err)
	}
	target := ApprovalTarget{AccountID: f.request.AccountID, NetworkID: f.request.NetworkID, NetworkName: f.request.NetworkName}
	prepared, err := PrepareApprovalState(p, target, approvalBase, f.now)
	if err != nil {
		t.Fatal(err)
	}
	f.p = prepared
	return f, target
}

func TestApprovalStateOriginalInputSurvivesUncertainCommitAndReopen(t *testing.T) {
	for _, assigned := range []bool{false, true} {
		f, target := newApprovalStateFixture(t, assigned)
		f.memory.lose = true
		if err := saveApprovalProfile(t, &f, f.p); !errors.Is(err, state.ErrUncertain) {
			t.Fatal("lost persistence ACK misreported", err)
		}
		block, err := aes.NewCipher(bytes.Repeat([]byte{4}, 32))
		if err != nil {
			t.Fatal(err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := state.New(f.memory, testCodec{aead}, "e0802a1d-0c1c-43cc-86c5-cbe0ec3f25e8", 1<<20, 2<<20)
		if err != nil {
			t.Fatal(err)
		}
		f.store = reopened
		loaded := f.persisted(t)
		if !reflect.DeepEqual(loaded, f.p) || loaded.Approval.Snapshot != nil || loaded.RemoteInFlight || !loaded.Attempt.BrowserApproval || loaded.Attempt.Phase != enrollment.PendingApproval {
			t.Fatal("approval recovery replaced original private input")
		}
		if !assigned && (loaded.Attempt.AccountID != "" || loaded.Attempt.NetworkID != "") {
			t.Fatal("name-only approval invented context")
		}
		again, err := PrepareApprovalState(loaded, target, approvalBase, f.now.Add(time.Minute))
		if err != nil || !reflect.DeepEqual(again, loaded) {
			t.Fatal("approval replay changed deadline/UUID/keys", err)
		}
		if _, err := PrepareRegistration(loaded); !errors.Is(err, ErrRegistrationProtocol) {
			t.Fatal("browser input entered native registration", err)
		}
		snapshot := f.pending()
		loaded.Approval.Snapshot = &snapshot
		loaded.Approval.NextCallAt = f.now.Add(2 * time.Second)
		if err := saveApprovalProfile(t, &f, loaded); err != nil {
			t.Fatal("private snapshot did not persist", err)
		}
		if raw := f.memory.data; bytes.Contains(raw, []byte(snapshot.PollToken)) || bytes.Contains(raw, []byte(loaded.Secrets.RegistrationRequest)) {
			t.Fatal("approval secrets persisted in plaintext")
		}
		if text := fmt.Sprintf("%+v %#v", *loaded.Approval, snapshot); bytes.Contains([]byte(text), []byte(snapshot.PollToken)) {
			t.Fatal("snapshot fmt disclosed poll capability")
		}
	}
}

func TestApprovalStateReceiptCannotChangeOrGrantRegistration(t *testing.T) {
	f, _ := newApprovalStateFixture(t, false)
	if err := saveApprovalProfile(t, &f, f.p); err != nil {
		t.Fatal(err)
	}
	snapshot := f.pending()
	f.p.Approval.Snapshot = &snapshot
	if err := saveApprovalProfile(t, &f, f.p); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"base", "poll", "request-id", "device", "deadline", "class", "drop", "hint", "next-budget"} {
		t.Run(name, func(t *testing.T) {
			p := f.persisted(t)
			switch name {
			case "base":
				p.Approval.Base = "https://untrusted.example/"
			case "poll":
				p.Approval.Snapshot.PollToken = "changed-valid-poll-authorization-000000000"
			case "request-id":
				p.Approval.Snapshot.Request.ID = "another-request"
			case "device":
				p.Approval.Snapshot.Request.Hostname = "other-device"
			case "deadline":
				p.Attempt.ApprovalUntil = p.Attempt.ApprovalUntil.Add(time.Second)
			case "class":
				p.Attempt.BrowserApproval = false
			case "drop":
				p.Approval = nil
			case "hint":
				p.Approval.Snapshot.PollAfterSeconds = 0
			case "next-budget":
				p.Approval.NextCallAt = p.Attempt.Deadline.Add(time.Second)
			}
			if err := saveApprovalProfile(t, &f, p); !errors.Is(err, state.ErrCorrupt) {
				t.Fatal("changed approval receipt accepted", name, err)
			}
		})
	}
	f.now = f.now.Add(2 * time.Second)
	f.approve()
	p := f.persisted(t)
	a, err := p.Attempt.BindBrowserContext(f.view.AccountID, f.view.NetworkID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	a, err = a.Approved(f.now)
	if err != nil {
		t.Fatal(err)
	}
	p.Attempt = &a
	approved := f.pending()
	approved.PollAfterSeconds = 0
	p.Approval.Snapshot = &approved
	if err := saveApprovalProfile(t, &f, p); err != nil {
		t.Fatal("original owner resolution could not persist", err)
	}
	// Binding to resolved context must not rewrite the original name-only proof.
	copyForAdapter := p
	copyForAdapter.RemoteInFlight, copyForAdapter.InFlightAt = true, f.now
	adapter := f.approval(t, nil)
	if _, err := adapter.input(copyForAdapter, []byte(p.Secrets.RegistrationRequest)); err != nil {
		t.Fatal("resolved context rewrote original request", err)
	}
	changed := f.persisted(t)
	changed.Approval.Snapshot.Request.ApprovedByUserID = "different-owner"
	if err := saveApprovalProfile(t, &f, changed); !errors.Is(err, state.ErrCorrupt) {
		t.Fatal("original decision principal changed", err)
	}
	f.now = f.now.Add(time.Second)
	p = f.persisted(t)
	a, err = p.Attempt.Observe(f.now)
	if err != nil {
		t.Fatal(err)
	}
	p.Attempt = &a
	p.Approval.Snapshot.ObservedAt = f.now
	p.Approval.Snapshot.Request.Status = "enrolled"
	p.Approval.Snapshot.Request.NodeID = "remote-observed-node"
	completed := f.now
	p.Approval.Snapshot.Request.CompletedAt = &completed
	if err := saveApprovalProfile(t, &f, p); err != nil {
		t.Fatal("enrolled observation failed", err)
	}
	loaded := f.persisted(t)
	if loaded.Attempt.Phase != enrollment.Completing || loaded.Attempt.NodeID != "" || loaded.Secrets.NodeCredential != "" {
		t.Fatal("enrolled snapshot granted unverified registration")
	}
}

func TestApprovalStateRejectsFrozenNativeRequest(t *testing.T) {
	f := newRegistrationFixture(t)
	p := f.p
	p.RemoteInFlight, p.InFlightAt = false, time.Time{}
	p.Secrets.RegistrationRequest = string(f.original)
	if _, err := PrepareApprovalState(p, ApprovalTarget{AccountID: "account", NetworkID: "network"}, approvalBase, f.now); !errors.Is(err, ErrApprovalProtocol) {
		t.Fatal("frozen native request converted into approval", err)
	}
	// Unrelated native state still round-trips without adding browser metadata.
	before, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var after state.Profile
	if err := json.Unmarshal(before, &after); err != nil || after.Approval != nil || after.Attempt.BrowserApproval || after.Secrets.RegistrationRequest != p.Secrets.RegistrationRequest {
		t.Fatal("native data was not preserved", err)
	}
}
