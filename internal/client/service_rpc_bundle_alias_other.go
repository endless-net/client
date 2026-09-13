//go:build !darwin

package client

import "os"

func isTrustedDiagnosticsPathAlias(_ string, _ os.FileInfo) bool {
	return false
}
