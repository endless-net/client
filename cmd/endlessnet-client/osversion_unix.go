//go:build linux || darwin

package main

import (
	"bytes"
	"runtime"

	"golang.org/x/sys/unix"
)

func diagnosticsOSVersion() map[string]any {
	return diagnosticsOSVersionWithUname(unix.Uname)
}

func diagnosticsOSVersionWithUname(uname func(*unix.Utsname) error) map[string]any {
	out := map[string]any{"name": runtime.GOOS}
	var info unix.Utsname
	if uname == nil || uname(&info) != nil {
		return out
	}
	end := bytes.IndexByte(info.Release[:], 0)
	// Keep a bounded public kernel release only. Uname's hostname and build
	// description can include machine-specific data and are never projected.
	if end <= 0 || end > 240 {
		return out
	}
	for _, b := range info.Release[:end] {
		if b < '!' || b > '~' {
			return out
		}
	}
	kernel := "Linux"
	if runtime.GOOS == "darwin" {
		kernel = "Darwin"
	}
	// Darwin's kernel release is not the macOS product version. Label both
	// platforms explicitly so the RPC version string preserves that distinction.
	out["version"] = kernel + " kernel " + string(info.Release[:end])
	return out
}
