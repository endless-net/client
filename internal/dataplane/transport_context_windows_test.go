//go:build windows

package dataplane

import (
	"bytes"
	"net/netip"
	"testing"

	"github.com/endless-net/client/api/transport"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

func TestNetworkContextUsesStableIdentitiesAndNeverGuessesUnknownNetwork(t *testing.T) {
	key := bytes.Repeat([]byte{85}, 32)
	a := windows.GUID{Data1: 1}
	b := windows.GUID{Data1: 2}
	identities := map[winipcfg.LUID]bool{10: true, 20: true}
	adapters := []*winipcfg.IPAdapterAddresses{{LUID: 10, NetworkGUID: a, OperStatus: winipcfg.IfOperStatusUp}, {LUID: 20, NetworkGUID: b, OperStatus: winipcfg.IfOperStatusUp}}
	guid := func(luid winipcfg.LUID) (*windows.GUID, error) {
		id := windows.GUID{Data1: uint32(luid)}
		return &id, nil
	}
	first, err := contextDigest(key, identities, adapters, guid)
	if err != nil || !transport.ValidDigest(first) {
		t.Fatal("known context unavailable", err)
	}
	adapters[0], adapters[1] = adapters[1], adapters[0]
	second, err := contextDigest(key, identities, adapters, guid)
	if err != nil || second != first {
		t.Fatal("enumeration order changed context")
	}
	adapters[0].NetworkGUID.Data1++
	second, err = contextDigest(key, identities, adapters, guid)
	if err != nil || second == first {
		t.Fatal("network identity change inherited context")
	}
	second, err = contextDigest(bytes.Repeat([]byte{86}, 32), identities, adapters, guid)
	third, _ := contextDigest(key, identities, adapters, guid)
	if err != nil || second == third {
		t.Fatal("context digest is linkable across local keys")
	}
	adapters[0].NetworkGUID = windows.GUID{}
	if _, err := contextDigest(key, identities, adapters, guid); err == nil {
		t.Fatal("zero network GUID reused context")
	}
	adapters[0].NetworkGUID = b
	if _, err := contextDigest(key, identities, adapters[:1], guid); err == nil {
		t.Fatal("incomplete adapter set reused context")
	}
}

func TestNetworkContextSelectsOnlyBoundedDefaultIPv4Interfaces(t *testing.T) {
	var physical, overlay winipcfg.MibIPforwardRow2
	physical.InterfaceLUID, overlay.InterfaceLUID = 10, 20
	if err := physical.DestinationPrefix.SetPrefix(netip.MustParsePrefix("0.0.0.0/0")); err != nil {
		t.Fatal(err)
	}
	if err := overlay.DestinationPrefix.SetPrefix(netip.MustParsePrefix("100.70.0.1/32")); err != nil {
		t.Fatal(err)
	}
	set, err := defaultContextInterfaces([]winipcfg.MibIPforwardRow2{overlay, physical}, 2, 1)
	if err != nil || len(set) != 1 || !set[10] || set[20] {
		t.Fatal("overlay affected underlay context", err)
	}
	if _, err := defaultContextInterfaces([]winipcfg.MibIPforwardRow2{overlay, physical}, 1, 1); err == nil {
		t.Fatal("inventory limit ignored")
	}
	if _, err := defaultContextInterfaces([]winipcfg.MibIPforwardRow2{overlay}, 2, 1); err == nil {
		t.Fatal("unknown underlay produced context")
	}
}
