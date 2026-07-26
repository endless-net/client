//go:build linux

package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const linuxInstallationStateDir = "/var/lib/endlessnet"

func hostInstallationBinding() (string, error) {
	return "", nil
}

func platformInstallationStateDir() (string, bool, error) {
	return resolveLinuxInstallationStateDir(os.Getenv("ENDLESSNET_INSTALLATION_STATE_DIR"), os.Geteuid())
}

func resolveLinuxInstallationStateDir(configuredDir string, effectiveUID int) (string, bool, error) {
	if dir := strings.TrimSpace(configuredDir); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", true, fmt.Errorf("ENDLESSNET_INSTALLATION_STATE_DIR must be an absolute path")
		}
		return filepath.Clean(dir), true, nil
	}
	if effectiveUID == 0 {
		return linuxInstallationStateDir, true, nil
	}
	return "", false, nil
}
