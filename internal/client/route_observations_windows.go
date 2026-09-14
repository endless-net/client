package client

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func observePlatformOSRoutes(ctx context.Context, iface string, targets []string) []WireGuardRouteInspection {
	return observeRouteTargets(ctx, iface, targets, func(ctx context.Context, addr netip.Addr) (string, error) {
		return windowsRouteInterface(ctx, addr, windows.GetBestInterfaceEx, net.InterfaceByIndex)
	})
}

// GetBestInterfaceEx performs the OS IPv4/IPv6 route lookup directly. Starting
// PowerShell per target made collection overlap every periodic agent update.
// https://learn.microsoft.com/en-us/windows/win32/api/iphlpapi/nf-iphlpapi-getbestinterfaceex
func windowsRouteInterface(ctx context.Context, addr netip.Addr, best func(windows.Sockaddr, *uint32) error, byIndex func(int) (*net.Interface, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !addr.IsValid() || addr.Zone() != "" {
		return "", errors.New("invalid route target")
	}
	var destination windows.Sockaddr
	if addr.Is4() {
		destination = &windows.SockaddrInet4{Addr: addr.As4()}
	} else {
		destination = &windows.SockaddrInet6{Addr: addr.As16()}
	}
	var index uint32
	if err := best(destination, &index); err != nil || index == 0 || uint64(index) > uint64(^uint(0)>>1) {
		return "", errors.New("route interface unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	observed, err := byIndex(int(index))
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil || observed == nil || observed.Index != int(index) {
		return "", errors.New("route interface unavailable")
	}
	return observed.Name, nil
}

func hideRouteObservationWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
