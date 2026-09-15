//go:build linux

package client

func newPlatformExitGuard(name string) (*linuxExitGuard, error) {
	return newLinuxExitGuard(name, defaultWireGuardEngineRouteTable, nil)
}
