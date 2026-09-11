//go:build !windows && !linux && !darwin

package client

func hostInstallationBinding() (string, error) {
	return "", nil
}

func platformInstallationStateDir() (string, bool, error) {
	return "", false, nil
}
