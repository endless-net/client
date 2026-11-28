package dataplane

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

var (
	dnsSettingsDLL   = windows.NewLazySystemDLL("iphlpapi.dll")
	getInterfaceDNS  = dnsSettingsDLL.NewProc("GetInterfaceDnsSettings")
	freeInterfaceDNS = dnsSettingsDLL.NewProc("FreeInterfaceDnsSettings")
)

// GetInterfaceDnsSettings takes a GUID by value, using the same Windows ABI
// as winipcfg.SetInterfaceDnsSettings. The result belongs to IP Helper.
func readWindowsSearch(guid windows.GUID) ([]string, error) {
	if err := getInterfaceDNS.Find(); err != nil {
		return nil, err
	}
	if err := freeInterfaceDNS.Find(); err != nil {
		return nil, err
	}
	settings := winipcfg.DnsInterfaceSettings{Version: winipcfg.DnsInterfaceSettingsVersion1}
	var rc uintptr
	switch runtime.GOARCH {
	case "amd64":
		rc, _, _ = getInterfaceDNS.Call(uintptr(unsafe.Pointer(&guid)), uintptr(unsafe.Pointer(&settings)))
	case "arm64":
		w := *(*[2]uint64)(unsafe.Pointer(&guid))
		rc, _, _ = getInterfaceDNS.Call(uintptr(w[0]), uintptr(w[1]), uintptr(unsafe.Pointer(&settings)))
	case "386", "arm":
		w := *(*[4]uint32)(unsafe.Pointer(&guid))
		rc, _, _ = getInterfaceDNS.Call(uintptr(w[0]), uintptr(w[1]), uintptr(w[2]), uintptr(w[3]), uintptr(unsafe.Pointer(&settings)))
	default:
		return nil, ErrConfiguration
	}
	if rc != 0 {
		return nil, syscall.Errno(rc)
	}
	defer func() { _, _, _ = freeInterfaceDNS.Call(uintptr(unsafe.Pointer(&settings))) }()
	if settings.SearchList == nil {
		return nil, nil
	}
	list := strings.ToLower(windows.UTF16PtrToString(settings.SearchList))
	return strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }), nil
}

func writeWindowsSearch(guid windows.GUID, domains []string) error {
	list, err := windows.UTF16PtrFromString(strings.Join(domains, ","))
	if err != nil {
		return err
	}
	return winipcfg.SetInterfaceDnsSettings(guid, &winipcfg.DnsInterfaceSettings{Version: winipcfg.DnsInterfaceSettingsVersion1, Flags: winipcfg.DnsInterfaceSettingsFlagSearchList, SearchList: list})
}

// Search domains belong to the session's newly created adapter. Its journaled
// removal on Stop or crash retires this interface-specific setting as well.
func (s *WindowsSession) changeSearch(ctx context.Context, before, after []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	record := s.tunnel.Record()
	row, err := s.identity(record.LUID)
	if err != nil {
		return err
	}
	guid, err := canonicalGUID(record.ID)
	if err != nil || row.GUID != guid || row.LUID != record.LUID || adapterProof(row) != record.Fingerprint {
		return ErrAdapterOwnership
	}
	current, err := s.readSearch(guid)
	if err != nil {
		return err
	}
	if !slices.Equal(current, before) {
		return ErrIPConflict
	}
	if slices.Equal(before, after) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.writeSearch(guid, after); err != nil {
		return err
	}
	current, err = s.readSearch(guid)
	if err != nil {
		return err
	}
	if !slices.Equal(current, after) {
		return ErrIPConflict
	}
	return ctx.Err()
}
