//go:build linux

package client

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func captureNativeExitLANSource(ctx context.Context, own string) (*exitLANSource, error) {
	return captureExitLANSource(ctx, own, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return runExitCommand(ctx, "", name, args...)
	}, inspectExitLANPhysical)
}

// Only these kernel-owned sysfs scalars/symlinks are read; there is no directory
// enumeration, driver configuration access or arbitrary caller-supplied path.
// This excludes known virtual bindings, not hardware emulation. A nonvirtual
// sysfs candidate still needs platform qualification before LAN capability.
func inspectExitLANPhysical(ctx context.Context, name string) (exitLANPhysical, bool, error) {
	fail := func() (exitLANPhysical, bool, error) {
		if ctx.Err() != nil {
			return exitLANPhysical{}, false, ctx.Err()
		}
		return exitLANPhysical{}, false, errExitLANSource
	}
	if err := ctx.Err(); err != nil {
		return exitLANPhysical{}, false, err
	}
	if !exitLANInterfaceName(name) || name == "lo" {
		return fail()
	}
	netPath, err := filepath.EvalSymlinks(filepath.Join("/sys/class/net", name))
	if err != nil {
		return fail()
	}
	validPath := func(path string) bool {
		return strings.HasPrefix(path, "/sys/devices/") && !strings.HasPrefix(path, "/sys/devices/virtual/")
	}
	if !validPath(netPath) {
		return exitLANPhysical{}, false, nil
	}
	devicePath, err := filepath.EvalSymlinks(filepath.Join(netPath, "device"))
	if os.IsNotExist(err) {
		return exitLANPhysical{}, false, nil
	}
	if err != nil || !validPath(devicePath) {
		return fail()
	}
	readBinding := func(field string) (string, error) {
		target, err := os.Readlink(filepath.Join(devicePath, field))
		if err != nil {
			return "", errExitLANSource
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(devicePath, target)
		}
		target = filepath.Clean(target)
		if !strings.HasPrefix(target, "/sys/bus/") || len(target) > 4096 {
			return "", errExitLANSource
		}
		return target, nil
	}
	driverPath, err := readBinding("driver")
	if err != nil {
		return fail()
	}
	subsystemPath, err := readBinding("subsystem")
	if err != nil {
		return fail()
	}
	driver, subsystem := filepath.Base(driverPath), filepath.Base(subsystemPath)
	if subsystemPath != "/sys/bus/"+subsystem || driverPath != subsystemPath+"/drivers/"+driver {
		return fail()
	}
	if exitLANVirtualDevice(devicePath, driver, subsystem) {
		return exitLANPhysical{}, false, nil
	}
	read := func(field string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		file, err := os.Open(filepath.Join(netPath, field))
		if err != nil {
			return "", errExitLANSource
		}
		defer func() { _ = file.Close() }()
		raw, err := io.ReadAll(io.LimitReader(file, 257))
		if err != nil || len(raw) == 0 || len(raw) > 256 {
			return "", errExitLANSource
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}
	number := func(field string) (uint32, error) {
		value, err := read(field)
		if err != nil {
			return 0, err
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return 0, errExitLANSource
		}
		return uint32(n), nil
	}
	index, err := number("ifindex")
	if err != nil {
		return fail()
	}
	link, err := number("iflink")
	if err != nil {
		return fail()
	}
	typ, err := number("type")
	if err != nil {
		return fail()
	}
	if index == 0 || index > 1<<31-1 || index != link || typ != 1 {
		return exitLANPhysical{}, false, nil
	}
	address, err := read("address")
	if err != nil {
		return fail()
	}
	carrier, err := number("carrier")
	if err != nil {
		return fail()
	}
	state, err := read("operstate")
	if err != nil {
		return fail()
	}
	changes, err := number("carrier_changes")
	if err != nil {
		return fail()
	}
	if ctx.Err() != nil {
		return exitLANPhysical{}, false, ctx.Err()
	}
	return exitLANPhysical{Index: int(index), LinkIndex: int(link), Type: int(typ), DevicePath: devicePath, HardwareAddress: address, Driver: driver, Subsystem: subsystem, CarrierChanges: changes, Up: carrier == 1 && state == "up"}, true, nil
}
