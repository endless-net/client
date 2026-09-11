//go:build darwin

package client

import "os"

func hostInstallationBinding() (string, error) {
	return "", nil
}

func platformInstallationStateDir() (string, bool, error) {
	// A system launchd daemon has no user HOME. Administrative CLI enrollment
	// and the daemon must bind to the same machine installation identity.
	if os.Geteuid() == 0 {
		return "/Library/Application Support/EndlessNet", true, nil
	}
	return "", false, nil
}
