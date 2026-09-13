//go:build windows

package main

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/windows"
)

func diagnosticsOSVersion() map[string]any {
	info := windows.RtlGetVersion()
	servicePack := windows.UTF16ToString(info.CsdVersion[:])
	out := map[string]any{
		"name":               runtime.GOOS,
		"version":            fmt.Sprintf("%d.%d.%d", info.MajorVersion, info.MinorVersion, info.BuildNumber),
		"major":              info.MajorVersion,
		"minor":              info.MinorVersion,
		"build":              info.BuildNumber,
		"platform_id":        info.PlatformId,
		"product_type":       info.ProductType,
		"suite_mask":         info.SuiteMask,
		"service_pack":       servicePack,
		"service_pack_major": info.ServicePackMajor,
		"service_pack_minor": info.ServicePackMinor,
	}
	return out
}
