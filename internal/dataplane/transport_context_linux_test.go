package dataplane

import (
	"bytes"
	"net"
	"strings"
	"testing"
)

func TestLinuxTransportHintGatewayIdentity(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	routes := "Iface Destination Gateway Flags RefCnt Use Metric Mask\neth0 00000000 0100000A 0003 0 0 0 00000000"
	arp := "IP address HW type Flags HW address Mask Device\n10.0.0.1 0x1 0x2 02:00:00:00:00:01 * eth0"
	lookup := func(string) (*net.Interface, error) {
		return &net.Interface{Flags: net.FlagUp, HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 2}}, nil
	}
	first, err := linuxContextDigest(key, routes, arp, lookup)
	if err != nil || len(first) != 64 {
		t.Fatalf("digest = %q, %v", first, err)
	}
	next, err := linuxContextDigest(key, routes, strings.Replace(arp, ":01", ":03", 1), lookup)
	if err != nil || next == first {
		t.Fatalf("changed gateway = %q, %v", next, err)
	}
	for _, invalid := range []string{"", strings.Replace(arp, "0x2", "0x0", 1), arp + "\n" + strings.Split(arp, "\n")[1]} {
		if _, err := linuxContextDigest(key, routes, invalid, lookup); err == nil {
			t.Fatal("accepted unresolved or ambiguous gateway")
		}
	}
}
