//go:build windows && integration

package wintundriver

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// This isolated process loads the verified user-space library and reads its
// version. It never calls CreateAdapter/OpenAdapter/StartSession/DeleteDriver.
func TestWindowsBundledDLLLoadWithoutAdapterMutation(t *testing.T) {
	testPinnedDLLLoad(t, "wintun.dll", "ENDLESSNET_WINTUN_DLL_FIXTURE", Load, &loader, bundled)
}

func TestWindowsBundledWireGuardNTLoadWithoutAdapterMutation(t *testing.T) {
	testPinnedDLLLoad(t, "wireguard.dll", "ENDLESSNET_WIREGUARDNT_DLL_FIXTURE", LoadWireGuardNT, &kernelLoader, func() (manifest, error) {
		return parseManifest(kernelManifestBytes, "wireguard-nt", "wireguard.dll")
	})
}

func testPinnedDLLLoad(t *testing.T, dllName, fixture string, load func() error, pinned *pinnedLoader, manifestForDLL func() (manifest, error)) {
	t.Helper()
	const childMarker = "ENDLESSNET_WINTUN_LOAD_CHILD"
	if mode := os.Getenv(childMarker); mode != "" {
		if mode == "preloaded" {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(filepath.Dir(executable), dllName)
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			m, manifestErr := manifestForDLL()
			verified := manifestErr == nil && verify(file, m.Files[dllName]) == nil
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
			first, second := load(), load()
			if !errors.Is(first, ErrDriver) || !errors.Is(second, ErrDriver) || pinned.handle != 0 {
				t.Fatal("invalid/prior DLL accepted or failure not terminal")
			}
			return
		}
		if err := load(); err != nil {
			t.Fatal(err)
		}
		if pinned.handle == 0 || load() != nil {
			t.Fatal("verified library not retained/idempotent")
		}
		return
	}
	source := os.Getenv(fixture)
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
			dll := filepath.Join(directory, dllName)
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
			command := exec.CommandContext(ctx, child, "-test.run=^"+strings.Split(t.Name(), "/")[0]+"$", "-test.count=1")
			command.Env = append(os.Environ(), childMarker+"="+mode)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("isolated library load failed: %v\n%s", err, output)
			}
		})
	}
}
