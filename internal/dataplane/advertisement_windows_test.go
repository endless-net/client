package dataplane

import (
	"errors"
	"net/netip"
	"strings"
	"testing"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
)

func TestLANAdvertisementBoundsAndWithdrawal(t *testing.T) {
	listener := preparedListener{"profile", strings.Repeat("a", 64), 31546}
	listeners := []preparedListener{listener}
	addresses := []netip.Addr{netip.MustParseAddr("192.168.0.1"), netip.MustParseAddr("172.16.0.1")}
	for i := len(addresses); i >= 0; i-- {
		ad, err := lanAdvertisement(5, listeners, addresses[:i])
		if err != nil || len(ad.Endpoints) != i || coordinator.ValidateTransportAdvertisement(ad) != nil {
			t.Fatal("incomplete inventory", i, err)
		}
		for j, ep := range ad.Endpoints {
			if ep.Endpoint != netip.AddrPortFrom(addresses[j], listener.port).String() {
				t.Fatal("wrong listener address or port")
			}
		}
	}
	for i := 0; i < 2000; i++ {
		addresses = append(addresses, netip.AddrFrom4([4]byte{10, 1, byte(i >> 8), byte(i)}))
	}
	ad, err := lanAdvertisement(5, listeners, addresses)
	if !errors.Is(err, ErrTransportCapacity) || len(ad.Endpoints) != 0 {
		t.Fatal("partial oversized set", err)
	}
}
