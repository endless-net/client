//go:build windows && integration

package state

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsFirstWritePrivateOwnership(t *testing.T) {
	directory, sid := protectedDirectory(t)
	b, err := OpenWindows(directory, sid, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	for range 2 {
		if err = b.Exclusive(context.Background(), func(s Slot) error { return s.Replace([]byte("synthetic fixture")) }); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"state.lock", "state.bin"} {
		f, err := b.root.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			f.Close()
			t.Fatal(err)
		}
		owner, _, err := sd.Owner()
		if err != nil || owner.String() != sid {
			f.Close()
			t.Fatal("explicit owner missing", err)
		}
		control, _, err := sd.Control()
		if err != nil || control&windows.SE_DACL_PROTECTED == 0 || b.check(f, false) != nil {
			f.Close()
			t.Fatal("private protected ACL missing", err)
		}
		f.Close()
	}
	if f, err := b.createPrivate("state.bin"); !errors.Is(err, windows.STATUS_OBJECT_NAME_COLLISION) {
		if f != nil {
			f.Close()
		}
		t.Fatal("creation overwrote existing state", err)
	}
}

func TestWindowsExistingLockIsNotRepaired(t *testing.T) {
	directory, sid := protectedDirectory(t)
	b, err := OpenWindows(directory, sid, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	f, err := b.root.OpenFile("state.lock", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;" + sid + ")(A;;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(directory+"\\state.lock", windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err = b.Exclusive(context.Background(), func(Slot) error { called = true; return nil }); !errors.Is(err, ErrCorrupt) || called {
		t.Fatal("unsafe existing lock accepted", err)
	}
	after, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil || before.String() != after.String() {
		t.Fatal("existing permissions repaired", err)
	}
}

func TestWindowsConcurrentFirstLock(t *testing.T) {
	directory, sid := protectedDirectory(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 8)
	for range cap(results) {
		go func() {
			b, err := OpenWindows(directory, sid, time.Millisecond)
			if err != nil {
				results <- err
				return
			}
			defer b.Close()
			<-start
			results <- b.Exclusive(ctx, func(s Slot) error { return s.Replace([]byte("concurrent fixture")) })
		}()
	}
	close(start)
	for range cap(results) {
		if err := <-results; err != nil {
			t.Error("concurrent initial lock/write failed", err)
		}
	}
}

func TestWindowsStateWriteFailureReportsReplace(t *testing.T) {
	directory, sid := protectedDirectory(t)
	b, err := OpenWindows(directory, sid, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var logs bytes.Buffer
	b.WriteLogger = slog.New(slog.NewJSONHandler(&logs, nil))
	write := func(slot Slot) error { return slot.Replace([]byte("sensitive-fixture")) }
	if err := b.Exclusive(t.Context(), write); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(filepath.Join(directory, "state.bin"))
	if err != nil {
		t.Fatal(err)
	}
	// Keep the old file open without FILE_SHARE_DELETE.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := b.Exclusive(t.Context(), write); !errors.Is(err, ErrUncertain) {
		t.Fatalf("replace result: %v", err)
	}
	var event struct {
		Msg         string
		Operation   string
		WindowsCode uint32 `json:"win32_code"`
	}
	if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Msg != "client.state.write_failed" || event.Operation != "replace_state" || event.WindowsCode == 0 {
		t.Fatalf("unexpected diagnostic: %+v", event)
	}
	if strings.Contains(logs.String(), "sensitive-fixture") || strings.Contains(logs.String(), "state.bin") {
		t.Fatal("raw state or filename leaked into diagnostic")
	}
}

func TestWindowsExpiredBeforeRenameKeepsPreviousState(t *testing.T) {
	directory, sid := protectedDirectory(t)
	backend, err := OpenWindows(directory, sid, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	store, err := New(backend, codec(t), installation, 1<<20, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	before, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	expired, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	attempted := before
	attempted.Revision++
	err = store.save(t.Context(), windowsSlot{backend: backend, ctx: expired}, attempted)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUncertain) {
		t.Fatalf("expected a definite deadline before rename, got %v", err)
	}
	after, err := store.Load(t.Context())
	if err != nil || after.Revision != before.Revision {
		t.Fatal("old state changed", err)
	}
}
