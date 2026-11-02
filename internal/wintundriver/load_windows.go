//go:build windows

package wintundriver

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

var loader struct {
	sync.Once
	handle windows.Handle // retained for the process lifetime
	err    error
}

// Load verifies and pins the adjacent DLL before the upstream lazy loader can
// search by basename. A prior foreign module or any failure is terminal for
// this process. It never searches PATH/System32 for a substitute Wintun binary.
func Load() error {
	loader.Do(func() { loader.handle, loader.err = load() })
	return loader.err
}

func module(name string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var h windows.Handle
	err = windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, p, &h)
	return h, err
}

func load() (windows.Handle, error) {
	if runtime.GOARCH != "amd64" {
		return 0, ErrDriver
	}
	m, err := bundled()
	if err != nil {
		return 0, err
	}
	// The package's own Once owns all accepted preloads. Do not adopt an
	// already mapped same-name module whose original bytes cannot be proven.
	if _, err := module("wintun.dll"); !os.IsNotExist(err) && err != windows.ERROR_MOD_NOT_FOUND {
		return 0, ErrDriver
	}
	executable, err := os.Executable()
	if err != nil {
		return 0, ErrDriver
	}
	path := filepath.Join(filepath.Dir(executable), "wintun.dll")
	file, unlock, err := lockBinary(path)
	if err != nil {
		return 0, ErrDriver
	}
	defer unlock()
	if verify(file, m.Files["wintun.dll"]) != nil {
		return 0, ErrDriver
	}
	h, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return 0, ErrDriver
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = windows.FreeLibrary(h)
		}
	}()
	actual, err := module("wintun.dll")
	if err != nil || actual != h {
		return 0, ErrDriver
	}
	// Trigger upstream initialization while only the verified DLL is mapped.
	// Its default logger is then disabled before any adapter operation.
	if wintun.Version() != m.Version {
		return 0, ErrDriver
	}
	setLogger, err := windows.GetProcAddress(h, "WintunSetLogger")
	if err != nil {
		return 0, ErrDriver
	}
	callSetLogger(setLogger)
	accepted = true
	return h, nil
}

// lockBinary holds every ancestor against rename/replacement and holds the DLL
// against writes/deletion from before hashing through LoadLibraryEx. Reparse
// points and UNC/device paths are rejected. No ACL or file content is changed.
func lockBinary(path string) (*os.File, func(), error) {
	var handles []windows.Handle
	var file *os.File
	var released sync.Once
	unlock := func() {
		released.Do(func() {
			if file != nil {
				_ = file.Close()
			}
			for i := len(handles) - 1; i >= 0; i-- {
				_ = windows.CloseHandle(handles[i])
			}
		})
	}
	fail := func() (*os.File, func(), error) { unlock(); return nil, nil, ErrDriver }
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' || !filepath.IsAbs(path) || filepath.Clean(path) != path || !strings.EqualFold(filepath.Base(path), "wintun.dll") {
		return fail()
	}
	current := volume + `\`
	parts := strings.Split(strings.TrimPrefix(path, current), `\`)
	for i, part := range parts {
		current = filepath.Join(current, part)
		p, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return fail()
		}
		access, share, flags := uint32(windows.FILE_READ_ATTRIBUTES), uint32(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE), uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS)
		last := i == len(parts)-1
		if last {
			access, share = windows.GENERIC_READ, windows.FILE_SHARE_READ
		}
		h, err := windows.CreateFile(p, access, share, nil, windows.OPEN_EXISTING, flags, 0)
		if err != nil {
			return fail()
		}
		handles = append(handles, h)
		var info windows.ByHandleFileInformation
		if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || last == (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) {
			return fail()
		}
		if last {
			file = os.NewFile(uintptr(h), path)
			handles = handles[:len(handles)-1]
		}
	}
	if file == nil {
		return fail()
	}
	return file, unlock, nil
}
