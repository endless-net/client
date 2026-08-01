//go:build !windows

package main

import (
	"runtime"

	ipc "github.com/endless-net/client/ipc/v1"
)

func diagnosticsOSVersion() map[string]any {
	return map[string]any{
		"name": runtime.GOOS,
	}
}

func serviceIPCDiagnosticsOSVersion() ipc.DiagnosticsOSInfo {
	return ipc.DiagnosticsOSInfo{Name: runtime.GOOS}
}
