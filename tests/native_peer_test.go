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
		t.Fatal("runner has no usable IPv4 underlay interface")
	}
	return underlay
}
