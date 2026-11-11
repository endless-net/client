//go:build windows

package dataplane

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

func defaultContextInterfaces(routes []winipcfg.MibIPforwardRow2, maxRows, maxInterfaces int) (map[winipcfg.LUID]bool, error) {
	if len(routes) > maxRows {
		return nil, ErrNoAuthority
	}
	result := make(map[winipcfg.LUID]bool)
	for _, route := range routes {
		prefix := route.DestinationPrefix.Prefix()
		if prefix.IsValid() && prefix.Addr().Is4() && prefix.Bits() == 0 && !route.Loopback {
			result[route.InterfaceLUID] = true
		}
	}
	if len(result) == 0 || len(result) > maxInterfaces {
		return nil, ErrNoAuthority
	}
	return result, nil
}

func contextDigest(private []byte, interfaces map[winipcfg.LUID]bool, adapters []*winipcfg.IPAdapterAddresses, guid func(winipcfg.LUID) (*windows.GUID, error)) (string, error) {
	if len(private) != 32 || bytes.Equal(private, make([]byte, 32)) || len(interfaces) == 0 {
		return "", ErrNoAuthority
	}
	seen := make(map[winipcfg.LUID]bool)
	parts := make([]string, 0, len(interfaces))
	for _, adapter := range adapters {
		if adapter == nil || !interfaces[adapter.LUID] {
			continue
		}
		id, err := guid(adapter.LUID)
		if err != nil || id == nil || *id == (windows.GUID{}) || adapter.NetworkGUID == (windows.GUID{}) || adapter.OperStatus != winipcfg.IfOperStatusUp || seen[adapter.LUID] {
			return "", ErrNoAuthority
		}
		seen[adapter.LUID] = true
		parts = append(parts, id.String()+"/"+adapter.NetworkGUID.String())
	}
	if len(seen) != len(interfaces) {
		return "", ErrNoAuthority
	}
	sort.Strings(parts)
	mac := hmac.New(sha256.New, private)
	_, _ = mac.Write([]byte("endlessnet/client/transport-context/v1\x00"))
	for _, part := range parts {
		_, _ = mac.Write([]byte(part))
		_, _ = mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func windowsTransportContext(ctx context.Context, private []byte, maxRows, maxInterfaces int) (string, error) {
	if ctx.Err() != nil || maxRows <= 0 || maxInterfaces <= 0 {
		return "", ErrNoAuthority
	}
	routes, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil {
		return "", ErrNoAuthority
	}
	interfaces, err := defaultContextInterfaces(routes, maxRows, maxInterfaces)
	if err != nil {
		return "", err
	}
	adapters, err := winipcfg.GetAdaptersAddresses(windows.AF_INET, winipcfg.GAAFlagSkipAll)
	if err != nil || len(adapters) > maxInterfaces || ctx.Err() != nil {
		return "", ErrNoAuthority
	}
	return contextDigest(private, interfaces, adapters, func(luid winipcfg.LUID) (*windows.GUID, error) { return luid.GUID() })
}
