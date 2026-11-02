//go:build windows

package connectivity

import (
	"math"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowsClock requires Windows 10's precise biased interrupt-time counter.
// It uses only the system DLL; no wall/Go-monotonic fallback can hide suspend.
// https://learn.microsoft.com/en-us/windows/win32/sysinfo/interrupt-time
// API set: https://learn.microsoft.com/en-us/uwp/win32-and-com/win32-apis
type WindowsClock struct {
	epoch string
	query *windows.LazyProc
}

func NewWindowsClock(epoch string) (*WindowsClock, error) {
	if !identifier(epoch) {
		return nil, ErrConfig
	}
	query := windows.NewLazySystemDLL("api-ms-win-core-realtime-l1-1-1.dll").NewProc("QueryInterruptTimePrecise")
	if query.Find() != nil {
		return nil, ErrClock
	}
	return &WindowsClock{epoch: epoch, query: query}, nil
}

func (c *WindowsClock) Sample() (ClockSample, error) {
	if c == nil || c.query == nil {
		return ClockSample{}, ErrClock
	}
	var before, after uint64
	_, _, _ = c.query.Call(uintptr(unsafe.Pointer(&before)))
	wall := time.Now().UTC()
	_, _, _ = c.query.Call(uintptr(unsafe.Pointer(&after)))
	if before > math.MaxInt64/100 || after > math.MaxInt64/100 || after < before {
		return ClockSample{}, ErrClock
	}
	return ClockSample{Wall: wall, Uptime: time.Duration(before) * 100, UpperUptime: time.Duration(after) * 100, Epoch: c.epoch}, nil
}
