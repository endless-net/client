package client

import (
	"context"
	"unsafe"

	"golang.org/x/sys/windows"
)

func observePlatformOSDefaultRoute(ctx context.Context) (present bool, observed bool) {
	if err := ctx.Err(); err != nil {
		return false, false
	}
	for _, family := range []uint16{windows.AF_INET, windows.AF_INET6} {
		var table *windows.MibIpForwardTable2
		if err := windows.GetIpForwardTable2(family, &table); err != nil || table == nil {
			return false, false
		}
		if table.NumEntries > 1<<20 {
			windows.FreeMibTable(unsafe.Pointer(table))
			return false, false
		}
		for _, route := range table.Rows() {
			if route.DestinationPrefix.Prefix.Family == family && route.DestinationPrefix.PrefixLength == 0 && zeroWindowsRoutePrefix(&route.DestinationPrefix.Prefix) {
				present = true
			}
		}
		windows.FreeMibTable(unsafe.Pointer(table))
		if err := ctx.Err(); err != nil {
			return false, false
		}
	}
	return present, true
}

func zeroWindowsRoutePrefix(prefix *windows.RawSockaddrInet) bool {
	switch prefix.Family {
	case windows.AF_INET:
		address := (*windows.RawSockaddrInet4)(unsafe.Pointer(prefix))
		return address.Addr == [4]byte{}
	case windows.AF_INET6:
		address := (*windows.RawSockaddrInet6)(unsafe.Pointer(prefix))
		return address.Addr == [16]byte{} && address.Scope_id == 0
	default:
		return false
	}
}
