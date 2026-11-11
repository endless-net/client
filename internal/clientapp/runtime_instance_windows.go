//go:build windows

package clientapp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// The installer creates the parent with SYSTEM/Administrators write access and
// user read access. This public pointer contains no keys or authorization data;
// the UI still authenticates the actual named-pipe server through the OS.
func publishRuntimeInstance(ctx context.Context, path, instance string, retry time.Duration) error {
	if _, bounded := ctx.Deadline(); !bounded || ctx.Err() != nil || !stateDirectory(path) || !validConfigID(instance) || retry <= 0 {
		return ErrIPC
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".runtime-instance-")
	if err != nil {
		return ErrIPC
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, writeErr := io.WriteString(file, instance+"\n")
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || ctx.Err() != nil {
		return ErrIPC
	}
	from, err := windows.UTF16PtrFromString(file.Name())
	if err != nil {
		return ErrIPC
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrIPC
	}
	for {
		if ctx.Err() != nil {
			return ErrIPC
		}
		err = windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return ErrIPC
		}
		select {
		case <-ctx.Done():
			return ErrIPC
		case <-time.After(retry):
		}
	}
	if ctx.Err() != nil {
		_ = removeRuntimeInstance(path, instance)
		return ErrIPC
	}
	return nil
}

// A previous service process must not remove a newer instance's pointer.
func removeRuntimeInstance(path, instance string) (result error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrIPC
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.DELETE, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return ErrIPC
	}
	file := os.NewFile(uintptr(handle), path)
	defer func() {
		if file.Close() != nil {
			result = ErrIPC
		}
	}()
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil || info.NumberOfLinks != 1 || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return ErrIPC
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 128))
	if readErr != nil {
		return ErrIPC
	}
	if strings.TrimSpace(string(data)) != instance {
		return nil
	}
	// Delete this exact held file object, never a subsequently replaced path.
	deleteFile := byte(1)
	if windows.SetFileInformationByHandle(handle, windows.FileDispositionInfo, &deleteFile, 1) != nil {
		return ErrIPC
	}
	return nil
}
