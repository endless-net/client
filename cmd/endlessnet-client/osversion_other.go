//go:build !windows && !linux && !darwin

package main

import (
	"runtime"
)

func diagnosticsOSVersion() map[string]any {
	return map[string]any{
		"name": runtime.GOOS,
	}
}
