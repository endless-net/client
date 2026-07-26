//go:build !darwin

package main

import "os"

func isTrustedDiagnosticsPathAlias(_ string, _ os.FileInfo) bool {
	return false
}
