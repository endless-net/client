//go:build windows && integration

package dataplane

import (
	"testing"
)

// This check is read-only. It does not create an adapter/address/route or use
// installed Client state, SCM, WFP, DNS configuration or privileged mutation.
func TestIPHelperReadOnlyInventory(t *testing.T) {
	api := nativeIPAPI{}
	addresses, err := api.addresses()
	if err != nil {
		t.Fatal(err)
	}
	routes, err := api.routes()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range addresses {
		if !r.Address.Addr().Is4() {
			t.Fatal("invalid IPv4 native address projection")
		}
	}
	for _, r := range routes {
		if !r.DestinationPrefix.Prefix().IsValid() || !r.DestinationPrefix.Prefix().Addr().Is4() {
			t.Fatal("invalid IPv4 native route projection")
		}
	}
	if len(addresses) > 0 {
		key, err := api.guid(addresses[0].InterfaceLUID)
		if err != nil || key == nil {
			t.Fatal("LUID/GUID native lookup", err)
		}
		row, err := addresses[0].InterfaceLUID.Interface()
		if err != nil || row == nil || row.InterfaceLUID != addresses[0].InterfaceLUID || row.InterfaceGUID != *key || row.InterfaceIndex == 0 {
			t.Fatal("native interface index/identity lookup", err)
		}
	}
	t.Logf("read-only native inventory: addresses=%d routes=%d", len(addresses), len(routes))
}
