//go:build windows

package dataplane

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestFilterDWORDResultsPreserveMissingAndErrorClasses(t *testing.T) {
	if filterCallStatus(0) != nil {
		t.Fatal("success became failure")
	}
	for _, code := range []uint32{uint32(windows.FWP_E_FILTER_NOT_FOUND), uint32(windows.FWP_E_SUBLAYER_NOT_FOUND), uint32(windows.ERROR_ACCESS_DENIED), 0x80320009} {
		for _, status := range []uintptr{uintptr(code), uintptr(uint64(code) | 0xffffffff00000000)} {
			err := filterCallStatus(status)
			if !errors.Is(err, windows.Errno(code)) {
				t.Fatalf("DWORD classification lost for %x", status)
			}
			if filterMissing(err) != (code == uint32(windows.FWP_E_FILTER_NOT_FOUND)) || sublayerMissing(err) != (code == uint32(windows.FWP_E_SUBLAYER_NOT_FOUND)) {
				t.Fatal("unrelated native error treated as absence")
			}
		}
	}
}
