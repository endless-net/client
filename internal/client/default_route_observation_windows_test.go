package client

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestZeroWindowsRoutePrefix(t *testing.T) {
	tests := []struct {
		name   string
		family uint16
		set    func(*windows.RawSockaddrInet)
		want   bool
	}{
		{name: "ipv4 default", family: windows.AF_INET, want: true},
		{name: "ipv4 nonzero", family: windows.AF_INET, set: func(prefix *windows.RawSockaddrInet) {
			(*windows.RawSockaddrInet4)(unsafe.Pointer(prefix)).Addr = [4]byte{192, 0, 2, 1}
		}},
		{name: "ipv6 default", family: windows.AF_INET6, want: true},
		{name: "ipv6 nonzero", family: windows.AF_INET6, set: func(prefix *windows.RawSockaddrInet) {
			(*windows.RawSockaddrInet6)(unsafe.Pointer(prefix)).Addr = [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
		}},
		{name: "ipv6 scoped", family: windows.AF_INET6, set: func(prefix *windows.RawSockaddrInet) {
			(*windows.RawSockaddrInet6)(unsafe.Pointer(prefix)).Scope_id = 1
		}},
		{name: "unknown family", family: 0xFFFF},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var prefix windows.RawSockaddrInet
			prefix.Family = tc.family
			if tc.set != nil {
				tc.set(&prefix)
			}
			if got := zeroWindowsRoutePrefix(&prefix); got != tc.want {
				t.Fatalf("zero default prefix = %t, want %t", got, tc.want)
			}
		})
	}
}
