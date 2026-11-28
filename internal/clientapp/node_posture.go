package clientapp

import (
	coord "github.com/endless-net/coordinator/api/enrollment"
	"net"
	"net/netip"
	"runtime"
	"slices"
	"strings"
	"time"
)

func collectNodePosture(at time.Time) *coord.NodePostureMetadata {
	if at.IsZero() {
		return nil
	}
	m := &coord.NodePostureMetadata{ObservedAt: at, Platform: runtime.GOOS}
	m.OSVersion, m.KernelVersion, m.HardwareSerial = platformPosture()
	m.LocalAddresses = postureLocalAddresses(net.InterfaceAddrs())
	if coord.ValidateNodePostureMetadata(m) != nil {
		return nil
	}
	return m
}

// Exclusion checks require a complete observation of the interface addresses.
func postureLocalAddresses(addrs []net.Addr, err error) []string {
	if err != nil {
		return nil
	}
	var values []string
	for _, a := range addrs {
		if a == nil {
			return nil
		}
		prefix, err := netip.ParsePrefix(a.String())
		if err != nil {
			return nil
		}
		addr := prefix.Addr().Unmap()
		if addr.IsGlobalUnicast() || addr.IsLinkLocalUnicast() {
			values = append(values, addr.String())
		}
	}
	slices.Sort(values)
	values = slices.Compact(values)
	if len(values) > 64 {
		return nil
	}
	return values
}

func postureText(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 128 {
		return ""
	}
	for _, r := range text {
		if r < 0x20 || r > 0x7e {
			return ""
		}
	}
	return text
}
func systemSerial(text string) string {
	serial := postureText(text)
	switch strings.ToLower(serial) {
	case "none", "unknown", "not specified", "to be filled by o.e.m.", "default string", "system serial number":
		return ""
	}
	return serial
}
