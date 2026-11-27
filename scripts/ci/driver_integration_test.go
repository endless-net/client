//go:build integration

package main

import (
	"path/filepath"
	"testing"
)

// Exercise the same upstream fetch, digest check and extraction as the
// Windows bundle build. Unit tests cover corrupted archives without network.
func TestDownloadWindowsDriverPayloads(t *testing.T) {
	files, err := windowsDriverEntries(t.Context(), filepath.Join("..", ".."), target{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"wireguard.dll": true, "WIREGUARD-NT-LICENSE.txt": true, "wintun.dll": true, "WINTUN-LICENSE.txt": true}
	for _, f := range files {
		if !want[f.name] || len(f.content) == 0 {
			t.Fatalf("unexpected driver payload %s", f.name)
		}
		delete(want, f.name)
	}
	if len(want) != 0 {
		t.Fatalf("missing driver payloads %v", want)
	}
}
