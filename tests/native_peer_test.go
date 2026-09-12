package tests

import (
	"net"
	"net/netip"
	"testing"
)

func nativePeerUnderlay(t *testing.T, clientIPv4, peerIP netip.Addr) netip.Addr {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr() == peerIP {
			t.Fatal("reference peer overlay IP is assigned to the host")
		}
	}
	var underlay netip.Addr
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
			continue
		}
		values, err := iface.Addrs()
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			prefix, err := netip.ParsePrefix(value.String())
			if err != nil {
				continue
			}
			ip := prefix.Addr()
			if ip.Is4() && ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() && ip != clientIPv4 && ip != peerIP {
				underlay = ip
				break
			}
		}
		if underlay.IsValid() {
			break
		}
	}
	if !underlay.IsValid() {
		logNativeInterfaceState(t)
		t.Fatal("runner has no usable IPv4 underlay interface")
	}
	return underlay
}

// Record public OS interface properties without addresses or Client state.
// This distinguishes interface loss/flags from an address-family limitation.
func logNativeInterfaceState(t *testing.T) {
	t.Helper()
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Log("runner interface diagnostics unavailable")
		return
	}
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		ipv4, ipv6, usableIPv4 := 0, 0, 0
		for _, address := range addresses {
			prefix, parseErr := netip.ParsePrefix(address.String())
			if parseErr != nil {
				continue
			}
			ip := prefix.Addr()
			if ip.Is4() {
				ipv4++
				if ip.IsGlobalUnicast() && !ip.IsLinkLocalUnicast() {
					usableIPv4++
				}
			} else if ip.Is6() {
				ipv6++
			}
		}
		t.Logf("runner interface: index=%d up=%t loopback=%t point_to_point=%t ipv4=%d ipv6=%d global_ipv4=%d addresses_available=%t", iface.Index, iface.Flags&net.FlagUp != 0, iface.Flags&net.FlagLoopback != 0, iface.Flags&net.FlagPointToPoint != 0, ipv4, ipv6, usableIPv4, err == nil)
	}
}
