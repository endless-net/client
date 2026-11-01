//go:build windows && integration

package state

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func protectedDirectory(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	return directory, sid
}

func TestWindowsProtectedState(t *testing.T) {
	if testing.Short() {
		t.Skip("explicit Windows DPAPI/ACL/file fixture")
	}
	directory, sid := protectedDirectory(t)
	b, err := OpenWindows(directory, sid, time.Millisecond)
	if err != nil {
		t.Fatal("protected directory rejected", err)
	}
	defer func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	}()
	p, err := NewDPAPI(installation)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(b, p, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = s.Load(ctx); !errors.Is(err, ErrMissing) {
		t.Fatal("startup created missing state")
	}
	if err = s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.Update(ctx, 1, addAttempt); err != nil {
		t.Fatal(err)
	}
	before, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := os.ReadFile(filepath.Join(directory, "state.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cipher, []byte(before.Profiles[profileID].Secrets.RequestProof)) {
		t.Fatal("DPAPI file contains plaintext proof")
	}
	other, err := NewDPAPI("9bcc14eb-ef8d-4a6b-afef-955691ea4624")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.Open(cipher); !errors.Is(err, ErrCorrupt) {
		t.Fatal("DPAPI accepted wrong installation entropy")
	}
	// Reopen a separate directory handle: no process cache supplies state.
	b2, err := OpenWindows(directory, sid, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := b2.Close(); err != nil {
			t.Error(err)
		}
	}()
	s2, err := New(b2, p, installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s2.Load(ctx)
	if err != nil || *after.Profiles[profileID].Attempt != *before.Profiles[profileID].Attempt || !bytes.Equal(after.Profiles[profileID].Keys.IdentityPrivate, before.Profiles[profileID].Keys.IdentityPrivate) {
		t.Fatal("Windows restart lost identity/deadlines", err)
	}
	if err = s2.Update(ctx, 2, func(*State) error { return nil }); err != nil {
		t.Fatal("atomic Windows replace failed", err)
	}
	// Cross-handle OS lock respects cancellation; it does not rely on a Go mutex.
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- b.Exclusive(ctx, func(Slot) error { close(entered); <-release; return nil }) }()
	<-entered
	bounded, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	_, lockErr := s2.Load(bounded)
	cancel()
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(lockErr, context.DeadlineExceeded) {
		t.Fatal("lock wait ignored deadline", lockErr)
	}
	// Tamper leaves the original ciphertext on disk and never initializes a new key.
	cipher[len(cipher)-1] ^= 1
	if err = os.WriteFile(filepath.Join(directory, "state.bin"), cipher, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(ctx); !errors.Is(err, ErrCorrupt) {
		t.Fatal("tampered DPAPI state accepted")
	}
	if err = s.Initialize(ctx); !errors.Is(err, ErrStale) {
		t.Fatal("tampered state reset")
	}
	// Even an authenticated ciphertext cannot be read through a multiply linked
	// file that could give another path authority over replacements.
	link := filepath.Join(directory, "state.link")
	if err = os.Link(filepath.Join(directory, "state.bin"), link); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(ctx); !errors.Is(err, ErrCorrupt) {
		t.Fatal("hard-linked state accepted")
	}
}

func TestWindowsStateRejectsBroadACL(t *testing.T) {
	if testing.Short() {
		t.Skip("explicit Windows DACL fixture")
	}
	directory, sid := protectedDirectory(t)
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + sid + ")(A;OICI;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	if b, err := OpenWindows(directory, sid, time.Millisecond); err == nil {
		_ = b.Close()
		t.Fatal("world-readable key directory accepted")
	}
}
