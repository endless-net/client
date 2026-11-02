//go:build windows && integration

package wintundriver

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// This isolated process loads the verified user-space library and reads its
// version. It never calls CreateAdapter/OpenAdapter/StartSession/DeleteDriver.
func TestWindowsBundledDLLLoadWithoutAdapterMutation(t *testing.T) {
	const childMarker = "ENDLESSNET_WINTUN_LOAD_CHILD"
	if mode := os.Getenv(childMarker); mode != "" {
		if mode == "preloaded" {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(filepath.Dir(executable), "wintun.dll")
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			m, manifestErr := bundled()
			verified := manifestErr == nil && verify(file, m.Files["wintun.dll"]) == nil
			_ = file.Close()
			if !verified {
				t.Fatal("preload fixture not verified")
			}
			h, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = windows.FreeLibrary(h) }()
		}
		if mode != "valid" {
			first, second := Load(), Load()
			if !errors.Is(first, ErrDriver) || !errors.Is(second, ErrDriver) || loader.handle != 0 {
				t.Fatal("invalid/prior DLL accepted or failure not terminal")
			}
			return
		}
		if err := Load(); err != nil {
			t.Fatal(err)
		}
		if loader.handle == 0 || Load() != nil {
			t.Fatal("verified library not retained/idempotent")
		}
		return
	}
	source := os.Getenv("ENDLESSNET_WINTUN_DLL_FIXTURE")
	if source == "" {
		t.Skip("explicit pinned DLL fixture required; no driver download in test")
	}
	copyFile := func(source, destination string) {
		t.Helper()
		input, err := os.Open(source)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = input.Close() }()
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
		if err != nil {
			t.Fatal(err)
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil || closeErr != nil {
			t.Fatal("fixture copy failed")
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "missing", "changed", "preloaded"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			child := filepath.Join(directory, "driver-load.test.exe")
			copyFile(executable, child)
			dll := filepath.Join(directory, "wintun.dll")
			if mode != "missing" {
				copyFile(source, dll)
			}
			if mode == "changed" {
				if err := os.WriteFile(dll, []byte("untrusted library fixture, never execute"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, child, "-test.run=^TestWindowsBundledDLLLoadWithoutAdapterMutation$", "-test.count=1")
			command.Env = append(os.Environ(), childMarker+"="+mode)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("isolated library load failed: %v\n%s", err, output)
			}
		})
	}
}
