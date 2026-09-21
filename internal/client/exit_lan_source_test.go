package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type exitLANFixture struct {
	links, addresses, ipv4, ipv6 string
	physical                     exitLANPhysical
	eligible                     bool
	calls                        int
}

func newExitLANFixture() *exitLANFixture {
	return &exitLANFixture{
		links:     `[{"ifindex":2,"ifname":"eth0","flags":["UP","LOWER_UP","BROADCAST"],"link_type":"ether","address":"02:00:00:00:00:01"}]`,
		addresses: `[{"ifindex":2,"ifname":"eth0","addr_info":[{"family":"inet","local":"192.168.10.2","prefixlen":24,"scope":"global","valid_life_time":4294967295,"preferred_life_time":4294967295},{"family":"inet6","local":"2001:db8:10::2","prefixlen":64,"scope":"global","valid_life_time":5000,"preferred_life_time":4000}]}]`,
		ipv4:      `[{"dst":"192.168.10.0/24","dev":"eth0","protocol":"2","scope":"253","prefsrc":"192.168.10.2","flags":[]},{"dst":"default","dev":"eth0","gateway":"192.168.10.1"}]`,
		ipv6:      `[{"dst":"2001:db8:10::/64","dev":"eth0","protocol":"2","metric":256,"flags":[],"pref":"medium"}]`,
		physical:  exitLANPhysical{Index: 2, LinkIndex: 2, Type: 1, DevicePath: "/sys/devices/pci0000:00/0000:00:01.0", HardwareAddress: "02:00:00:00:00:01", Driver: "e1000e", Subsystem: "pci", CarrierChanges: 4, Up: true}, eligible: true,
	}
}
func (f *exitLANFixture) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.calls++
	if name != "ip" {
		return nil, errors.New("unexpected command")
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("missing collection deadline")
	}
	command := strings.Join(args, " ")
	switch command {
	case "-j -d link show":
		return []byte(f.links), nil
	case "-j address show":
		return []byte(f.addresses), nil
	case "-j -N -4 route show table main":
		return []byte(f.ipv4), nil
	case "-j -N -6 route show table main":
		return []byte(f.ipv6), nil
	default:
		return nil, errors.New("unexpected arguments")
	}
}
func (f *exitLANFixture) inspect(context.Context, string) (exitLANPhysical, bool, error) {
	return f.physical, f.eligible, nil
}

func TestExitLANSourceRejectsPathLikeNamesBeforeRead(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../eth0", "eth0/..", "/eth0", " eth0", "eth0 ", "lo"} {
		f := newExitLANFixture()
		if _, err := captureExitLANSource(t.Context(), name, f.run, f.inspect); err == nil || f.calls != 0 {
			t.Fatal("invalid scope reached command/inspection boundary", name, err)
		}
	}
}

func TestExitLANSourceStablePhysicalTopology(t *testing.T) {
	f := newExitLANFixture()
	source, err := captureExitLANSource(t.Context(), "endlessnet", f.run, f.inspect)
	if err != nil || len(source.Links) != 1 || len(source.Links[0].Routes) != 2 || f.calls != 8 {
		t.Fatal("stable physical topology missing", err)
	}
	if source.Links[0].Addresses[0].Addr() != netip.MustParseAddr("192.168.10.2") || source.Links[0].Routes[0].Prefix != netip.MustParsePrefix("192.168.10.0/24") {
		t.Fatal("host/prefix identity lost")
	}
	if source.ValidUntil.IsZero() || source.ValidUntil.After(time.Now().Add(4000*time.Second)) || source.ValidUntil.Before(time.Now().Add(3990*time.Second)) {
		t.Fatal("finite preferred lifetime was not retained conservatively")
	}
	f.physical.HardwareAddress = "changed"
	if source.Links[0].HardwareAddress != "02:00:00:00:00:01" {
		t.Fatal("snapshot aliases inspector")
	}
}

func TestExitLANSourceRejectsAmbiguousAndChangingEvidence(t *testing.T) {
	for _, scenario := range []string{"gateway_overlap", "via", "nhid", "duplicate_index", "wrong_index", "virtual_identity", "path_escape", "changed_carrier", "changed_route", "cancelled", "oversize", "duplicate_json", "unknown_route", "foreign_prefsrc"} {
		t.Run(scenario, func(t *testing.T) {
			f := newExitLANFixture()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "gateway_overlap":
				f.ipv4 = strings.TrimSuffix(f.ipv4, "]") + `,{"dst":"192.168.10.128/25","dev":"eth0","gateway":"192.168.10.1"}]`
			case "via":
				f.ipv4 = strings.TrimSuffix(f.ipv4, "]") + `,{"dst":"192.168.10.128/25","dev":"eth0","via":{"family":"inet6","host":"2001:db8::1"}}]`
			case "nhid":
				f.ipv4 = strings.TrimSuffix(f.ipv4, "]") + `,{"dst":"192.168.10.128/25","nhid":5}]`
			case "duplicate_index":
				f.links = strings.TrimSuffix(f.links, "]") + `,{"ifindex":2,"ifname":"eth1"}]`
			case "wrong_index":
				f.physical.Index = 3
			case "virtual_identity":
				f.physical.DevicePath = "/sys/devices/virtual/net/eth0"
			case "path_escape":
				f.physical.DevicePath = "/sys/devices/pci/../../elsewhere"
			case "duplicate_json":
				f.links = `[{"ifindex":2,"ifindex":3}]`
			case "oversize":
				f.links = strings.Repeat(" ", 1<<20+1)
			case "unknown_route":
				f.ipv4 = strings.Replace(f.ipv4, `"scope":"253"`, `"scope":"253","unknown_widening":true`, 1)
			case "foreign_prefsrc":
				f.ipv4 = strings.Replace(f.ipv4, `"prefsrc":"192.168.10.2"`, `"prefsrc":"192.168.10.254"`, 1)
			}
			inspect := func(ctx context.Context, name string) (exitLANPhysical, bool, error) {
				if f.calls >= 5 && scenario == "changed_carrier" {
					f.physical.CarrierChanges++
				}
				return f.inspect(ctx, name)
			}
			run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if scenario == "cancelled" && f.calls == 1 {
					cancel()
				}
				if scenario == "changed_route" && f.calls == 4 {
					f.ipv4 = `[]`
				}
				return f.run(ctx, name, args...)
			}
			source, err := captureExitLANSource(ctx, "endlessnet", run, inspect)
			if err == nil || source != nil {
				t.Fatal("ambiguous source accepted")
			}
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}

func TestExitLANSourceExcludesUnsupportedLinksAndAddresses(t *testing.T) {
	for _, scenario := range []string{"virtual", "virtio", "vmxnet3", "master", "kind", "tun", "own", "linklocal", "tentative", "no_direct_route"} {
		t.Run(scenario, func(t *testing.T) {
			f := newExitLANFixture()
			own := "endlessnet"
			switch scenario {
			case "virtual":
				f.eligible = false
			case "virtio":
				f.physical.Subsystem = "virtio"
			case "vmxnet3":
				f.physical.Driver = "vmxnet3"
			case "master":
				f.links = strings.Replace(f.links, `"ifname":"eth0"`, `"ifname":"eth0","master":"br0"`, 1)
			case "kind":
				f.links = strings.Replace(f.links, `"ifname":"eth0"`, `"ifname":"eth0","linkinfo":{"info_kind":"veth"}`, 1)
			case "tun":
				f.links = strings.Replace(f.links, `"ether"`, `"none"`, 1)
			case "own":
				own = "eth0"
			case "linklocal":
				f.addresses = strings.ReplaceAll(f.addresses, "192.168.10.2", "169.254.10.2")
				f.addresses = strings.ReplaceAll(f.addresses, "2001:db8:10::2", "fe80::2")
			case "tentative":
				f.addresses = strings.ReplaceAll(f.addresses, `"scope":"global"`, `"scope":"global","tentative":true`)
			case "no_direct_route":
				f.ipv4 = `[]`
				f.ipv6 = `[]`
			}
			source, err := captureExitLANSource(t.Context(), own, f.run, f.inspect)
			if err != nil {
				t.Fatal(err)
			}
			for _, link := range source.Links {
				if len(link.Routes) != 0 {
					t.Fatal("unsupported scope produced direct permissions")
				}
			}
		})
	}
}

func TestExitLANSourceRejectsOverlappingPhysicalAttachments(t *testing.T) {
	f := newExitLANFixture()
	var links, addresses []map[string]any
	if json.Unmarshal([]byte(f.links), &links) != nil || json.Unmarshal([]byte(f.addresses), &addresses) != nil {
		t.Fatal("fixture")
	}
	link := map[string]any{}
	for k, v := range links[0] {
		link[k] = v
	}
	link["ifindex"] = 3
	link["ifname"] = "wlan0"
	links = append(links, link)
	address := map[string]any{}
	for k, v := range addresses[0] {
		address[k] = v
	}
	address["ifindex"] = 3
	address["ifname"] = "wlan0"
	addresses = append(addresses, address)
	raw, _ := json.Marshal(links)
	f.links = string(raw)
	raw, _ = json.Marshal(addresses)
	f.addresses = string(raw)
	f.ipv4 = strings.TrimSuffix(f.ipv4, "]") + `,{"dst":"192.168.10.0/24","dev":"wlan0","metric":999}]`
	inspect := func(ctx context.Context, name string) (exitLANPhysical, bool, error) {
		p, ok, err := f.inspect(ctx, name)
		if name == "wlan0" {
			p.Index = 3
			p.LinkIndex = 3
			p.DevicePath = "/sys/devices/pci0000:00/0000:00:02.0"
		}
		return p, ok, err
	}
	if _, err := captureExitLANSource(t.Context(), "endlessnet", f.run, inspect); err == nil {
		t.Fatal("metrics resolved ambiguous physical overlap")
	}
}

func TestExitLANSourceKeepsPublicAndHostPrefixesForPolicySubtraction(t *testing.T) {
	for _, cidr := range []string{"8.8.8.2/24", "10.0.0.0/8", "192.0.2.0/31", "192.0.2.1/32", "fc00::2/64", "2001:db8::2/64"} {
		p := netip.MustParsePrefix(cidr)
		if !exitLANPrefixAllowed(p) {
			t.Fatal("topology discarded valid prefix", cidr)
		}
	}
	for _, cidr := range []string{"0.0.0.0/0", "::/0", "169.254.0.0/16", "fe80::/64", "ff02::/16", "224.0.0.0/24"} {
		if exitLANPrefixAllowed(netip.MustParsePrefix(cidr)) {
			t.Fatal("excluded topology accepted", cidr)
		}
	}
}

func TestExitLANSourcePublicWiFiAndLifetimeCountdown(t *testing.T) {
	f := newExitLANFixture()
	f.links = strings.ReplaceAll(f.links, "eth0", "wlan0")
	f.addresses = strings.ReplaceAll(strings.ReplaceAll(f.addresses, "eth0", "wlan0"), "192.168.10.2", "8.8.8.2")
	f.ipv4 = strings.ReplaceAll(strings.ReplaceAll(f.ipv4, "eth0", "wlan0"), "192.168.10.", "8.8.8.")
	f.ipv6 = strings.ReplaceAll(f.ipv6, "eth0", "wlan0")
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if f.calls == 4 {
			f.addresses = strings.ReplaceAll(f.addresses, `"preferred_life_time":4000`, `"preferred_life_time":3999`)
		}
		return f.run(ctx, name, args...)
	}
	source, err := captureExitLANSource(t.Context(), "endlessnet", run, f.inspect)
	if err != nil || len(source.Links) != 1 || source.Links[0].Name != "wlan0" || source.Links[0].Routes[1].Prefix != netip.MustParsePrefix("8.8.8.0/24") || source.ValidUntil.IsZero() || source.ValidUntil.After(time.Now().Add(3999*time.Second)) {
		t.Fatal("public physical topology or conservative countdown failed", err)
	}
}
