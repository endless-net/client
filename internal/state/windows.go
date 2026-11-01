//go:build windows

package state

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type DPAPI struct{ entropy [32]byte }

func NewDPAPI(installation string) (*DPAPI, error) {
	if !validID(installation) {
		return nil, ErrCorrupt
	}
	return &DPAPI{entropy: sha256.Sum256([]byte("endlessnet-client-state-v1\x00" + installation))}, nil
}

func (d *DPAPI) crypt(data []byte, protect bool) ([]byte, error) {
	if len(data) == 0 || uint64(len(data)) > uint64(^uint32(0)) {
		return nil, ErrCorrupt
	}
	// A service must never seal/unseal under an impersonated IPC caller.
	var token windows.Token
	err := windows.OpenThreadToken(windows.CurrentThread(), windows.TOKEN_QUERY, true, &token)
	if err == nil {
		_ = token.Close()
		return nil, ErrCorrupt
	}
	if !errors.Is(err, windows.ERROR_NO_TOKEN) {
		return nil, ErrCorrupt
	}
	input := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	entropy := windows.DataBlob{Size: 32, Data: &d.entropy[0]}
	var output windows.DataBlob
	if protect {
		err = windows.CryptProtectData(&input, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	} else {
		err = windows.CryptUnprotectData(&input, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	}
	runtime.KeepAlive(data)
	runtime.KeepAlive(d)
	if err != nil {
		return nil, ErrCorrupt
	}
	defer func() {
		clear(unsafe.Slice(output.Data, output.Size))
		_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	}()
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}
func (d *DPAPI) Seal(data []byte) ([]byte, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return d.crypt(data, true)
}
func (d *DPAPI) Open(data []byte) ([]byte, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return d.crypt(data, false)
}

// WindowsBackend opens a pre-provisioned protected directory. It never repairs
// permissions or initializes state at startup. All file operations are relative
// to an os.Root directory handle, including atomic same-directory rename.
type WindowsBackend struct {
	root       *os.Root
	directory  *os.File
	serviceSID string
	retry      time.Duration
}

func OpenWindows(directory, serviceSID string, retry time.Duration) (*WindowsBackend, error) {
	if !filepath.IsAbs(directory) || len(filepath.VolumeName(directory)) != 2 || retry <= 0 || retry > time.Second {
		return nil, ErrCorrupt
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || user.User.Sid.String() != serviceSID {
		return nil, ErrCorrupt
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrCorrupt
	}
	f, err := root.Open(".")
	if err != nil {
		_ = root.Close()
		return nil, ErrCorrupt
	}
	b := &WindowsBackend{root: root, directory: f, serviceSID: serviceSID, retry: retry}
	if b.check(f, true) != nil {
		_ = b.Close()
		return nil, ErrCorrupt
	}
	return b, nil
}

func (b *WindowsBackend) Close() error { return errors.Join(b.directory.Close(), b.root.Close()) }

func (b *WindowsBackend) check(file *os.File, directory bool) error {
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (!directory && info.NumberOfLinks != 1) {
		return ErrCorrupt
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return ErrCorrupt
	}
	owner, _, err := sd.Owner()
	if err != nil || (owner.String() != b.serviceSID && owner.String() != "S-1-5-18") {
		return ErrCorrupt
	}
	control, _, err := sd.Control()
	if err != nil || (directory && control&windows.SE_DACL_PROTECTED == 0) {
		return ErrCorrupt
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 || acl.AceCount > 2 {
		return ErrCorrupt
	}
	seen := map[string]bool{}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Mask != 0x1f01ff {
			return ErrCorrupt
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if sid != b.serviceSID && sid != "S-1-5-18" {
			return ErrCorrupt
		}
		seen[sid] = true
	}
	if !seen[b.serviceSID] || !seen["S-1-5-18"] {
		return ErrCorrupt
	}
	return nil
}

func (b *WindowsBackend) Exclusive(ctx context.Context, fn func(Slot) error) (err error) {
	if fn == nil {
		return ErrCorrupt
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if b.check(b.directory, true) != nil {
		return ErrCorrupt
	}
	f, err := b.root.OpenFile("state.lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return ErrCorrupt
	}
	defer func() {
		if e := f.Close(); err == nil && e != nil {
			err = ErrCorrupt
		}
	}()
	if b.check(f, false) != nil {
		return ErrCorrupt
	}
	var overlapped windows.Overlapped
	for {
		err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return ErrCorrupt
		}
		timer := time.NewTimer(b.retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	defer func() {
		if e := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped); err == nil && e != nil {
			err = ErrCorrupt
		}
	}()
	if err = ctx.Err(); err != nil {
		return err
	}
	return fn(windowsSlot{backend: b, ctx: ctx})
}

type windowsSlot struct {
	backend *WindowsBackend
	ctx     context.Context
}

func (s windowsSlot) Read(limit int64) ([]byte, error) {
	f, err := s.backend.root.Open("state.bin")
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrMissing
	}
	if err != nil {
		return nil, ErrCorrupt
	}
	defer func() { _ = f.Close() }()
	if s.backend.check(f, false) != nil {
		return nil, ErrCorrupt
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, ErrCorrupt
	}
	return data, nil
}
func (s windowsSlot) Replace(data []byte) (err error) {
	name := "state-" + rand.Text() + ".tmp"
	f, err := s.backend.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrCorrupt
	}
	defer func() { _ = f.Close(); _ = s.backend.root.Remove(name) }()
	if s.backend.check(f, false) != nil {
		return ErrCorrupt
	}
	n, err := f.Write(data)
	if err != nil || n != len(data) {
		return ErrCorrupt
	}
	if f.Sync() != nil || f.Close() != nil {
		return ErrCorrupt
	}
	if err = s.ctx.Err(); err != nil {
		return err
	}
	if err = s.backend.root.Rename(name, "state.bin"); err != nil {
		return ErrUncertain
	}
	return nil
}
