//go:build windows

package dataplane

import (
	"context"
	"errors"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// windowsAdapterPresent observes installed Wintun devices, including non-present
// ones. A residual MIB row can outlive its device and IP rows after SDK Close.
// Never open/adopt an adapter or invoke SDK-wide orphan/driver removal here.
func windowsAdapterPresent(ctx context.Context, key windows.GUID, limit int) (present bool, result error) {
	if ctx == nil || key == (windows.GUID{}) || limit <= 0 {
		return false, ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	netClass := windows.GUID{Data1: 0x4d36e972, Data2: 0xe325, Data3: 0x11ce, Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18}}
	set, err := windows.SetupDiGetClassDevsEx(&netClass, `SWD\WINTUN`, 0, 0, 0, "")
	if err != nil {
		return false, errors.Join(ErrAdapterUncertain, adapterError(err))
	}
	defer func() {
		if set.Close() != nil {
			present, result = false, errors.Join(result, ErrAdapterUncertain)
		}
	}()
	for i := 0; i <= limit; i++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		device, err := set.EnumDeviceInfo(i)
		if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
			return false, ctx.Err()
		}
		if err != nil {
			return false, errors.Join(ErrAdapterUncertain, adapterError(err))
		}
		if i == limit {
			return false, ErrAdapterLimit
		}
		handle, err := set.OpenDevRegKey(device, windows.DICS_FLAG_GLOBAL, 0, windows.DIREG_DRV, windows.KEY_QUERY_VALUE)
		if err != nil {
			return false, errors.Join(ErrAdapterUncertain, adapterError(err))
		}
		value, _, readErr := registry.Key(handle).GetStringValue("NetCfgInstanceID")
		closeErr := registry.Key(handle).Close()
		if readErr != nil || closeErr != nil {
			return false, ErrAdapterUncertain
		}
		id, err := windows.GUIDFromString(value)
		if err != nil || id == (windows.GUID{}) {
			return false, ErrAdapterUncertain
		}
		if id == key {
			return true, ctx.Err()
		}
	}
	return false, ErrAdapterLimit
}
