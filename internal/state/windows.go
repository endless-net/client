//go:build windows

package state

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
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
// to held directory handles, including atomic same-directory rename.
type WindowsBackend struct {
	// WriteLogger receives bounded failure diagnostics. Set it before using the backend.
	WriteLogger *slog.Logger
	root        *os.Root
	directory   *os.File
	serviceSID  string
	retry       time.Duration
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

// createPrivate sets ownership atomically with creation. LocalSystem under SCM
// can have Administrators as its default owner; inherited SYSTEM-only access
// alone therefore does not establish the required state-file ownership.
func (b *WindowsBackend) createPrivate(name string) (*os.File, error) {
	if name == "." || name == ".." || filepath.Base(name) != name || !filepath.IsLocal(name) || filepath.VolumeName(name) != "" {
		return nil, ErrCorrupt
	}
	sddl := "O:" + b.serviceSID + "D:P(A;;FA;;;SY)"
	if b.serviceSID != "S-1-5-18" {
		sddl += "(A;;FA;;;" + b.serviceSID + ")"
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, err
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	oa := windows.OBJECT_ATTRIBUTES{
		RootDirectory: windows.Handle(b.directory.Fd()), ObjectName: objectName,
		Attributes: windows.OBJ_CASE_INSENSITIVE, SecurityDescriptor: sd,
	}
	oa.Length = uint32(unsafe.Sizeof(oa))
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|windows.SYNCHRONIZE,
		&oa, &status, nil, windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_CREATE,
		windows.FILE_NON_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	runtime.KeepAlive(sd)
	runtime.KeepAlive(objectName)
	runtime.KeepAlive(b.directory)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), name), nil
}

func (b *WindowsBackend) openLock() (*os.File, error) {
	f, err := b.root.OpenFile("state.lock", os.O_RDWR, 0)
	if !errors.Is(err, os.ErrNotExist) {
		return f, err
	}
	f, err = b.createPrivate("state.lock")
	if errors.Is(err, windows.STATUS_OBJECT_NAME_COLLISION) {
		// Another first writer won. Existing objects are only checked, never repaired.
		return b.root.OpenFile("state.lock", os.O_RDWR, 0)
	}
	return f, err
}

func (b *WindowsBackend) check(file *os.File, directory bool) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (!directory && info.NumberOfLinks != 1) {
		return ErrCorrupt
	}
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner.String() != b.serviceSID && owner.String() != "S-1-5-18" {
		return ErrCorrupt
	}
	control, _, err := sd.Control()
	if err != nil {
		return err
	}
	if directory && control&windows.SE_DACL_PROTECTED == 0 {
		return ErrCorrupt
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if acl == nil || acl.AceCount == 0 || acl.AceCount > 2 {
		return ErrCorrupt
	}
	seen := map[string]bool{}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Mask != 0x1f01ff {
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
	f, err := b.openLock()
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
func (s windowsSlot) writeFailure(stage string, cause, result error) error {
	if logger := s.backend.WriteLogger; logger != nil {
		reason := "io_error"
		fields := []any{"component", "state", "operation", stage}
		var status windows.NTStatus
		var errno windows.Errno
		switch {
		case errors.As(cause, &status):
			fields = append(fields, "ntstatus", uint32(status), "win32_code", uint32(status.Errno()))
		case errors.As(cause, &errno):
			fields = append(fields, "win32_code", uint32(errno))
		case errors.Is(cause, context.DeadlineExceeded):
			reason = "deadline_exceeded"
		case errors.Is(cause, context.Canceled):
			reason = "canceled"
		case errors.Is(cause, io.ErrShortWrite):
			reason = "short_write"
		case errors.Is(cause, ErrCorrupt):
			reason = "security_check_failed"
		}
		logger.Error("client.state.write_failed", append(fields, "error_code", reason)...)
	}
	return result
}

func (s windowsSlot) Replace(data []byte) (err error) {
	name := "state-" + rand.Text() + ".tmp"
	f, err := s.backend.createPrivate(name)
	if err != nil {
		return s.writeFailure("create_temp", err, ErrCorrupt)
	}
	defer func() { _ = f.Close(); _ = s.backend.root.Remove(name) }()
	if err := s.backend.check(f, false); err != nil {
		return s.writeFailure("check_temp", err, ErrCorrupt)
	}
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return s.writeFailure("write_temp", err, ErrCorrupt)
	}
	if err := f.Sync(); err != nil {
		return s.writeFailure("flush_temp", err, ErrCorrupt)
	}
	if err := f.Close(); err != nil {
		return s.writeFailure("close_temp", err, ErrCorrupt)
	}
	if err = s.ctx.Err(); err != nil {
		return s.writeFailure("before_replace", err, err)
	}
	if err = s.backend.root.Rename(name, "state.bin"); err != nil {
		return s.writeFailure("replace_state", err, ErrUncertain)
	}
	return nil
}
