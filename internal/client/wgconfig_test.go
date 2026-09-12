package client

import (
	"encoding/base64"
	"strings"
	"testing"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestRenderWireGuardCheckedRejectsMapInjection(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	response := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{ID: "net-1", Name: "default", CIDR: "100.64.0.0/24", DNS: []string{"1.1.1.1"}},
		Node:    clientapi.Node{ID: "node-1", NetworkID: "net-1", Hostname: "node-1", PublicKey: key, AssignedIP: "100.64.0.2"},
		Peers: []clientapi.Peer{{
			ID: "node-2", Hostname: "node-2\n[Interface]", PublicKey: key,
			Endpoint: "node-2.example.test:51820", AllowedIPs: []string{"100.64.0.3/32"},
		}},
	}
	if rendered, err := RenderWireGuardWithOptionsChecked("private-key", response, WireGuardRenderOptions{}); err == nil || rendered != "" {
		t.Fatalf("RenderWireGuardWithOptionsChecked rendered an unsafe map: rendered=%q err=%v", rendered, err)
	}
}

func TestRenderWireGuardListenPort(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Network: clientapi.Network{
			DNS: []string{"100.91.0.1", "100.91.0.53"},
		},
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
	}, WireGuardRenderOptions{ListenPort: 7072})

	for _, want := range []string{
		"Address = 100.91.0.2/32",
		"ListenPort = 7072",
		"DNS = 100.91.0.1, 100.91.0.53",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderWireGuardDoesNotFlattenSplitDNSIntoGlobalOverride(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Network: clientapi.Network{
			DNS: []string{"192.0.2.53"},
			DNSConfig: &clientapi.DNSConfig{
				OverrideLocalDNS: false,
				Nameservers: []clientapi.DNSNameserver{
					{ID: "global", Address: "192.0.2.53", Scope: "global"},
					{ID: "split", Address: "198.51.100.53", Scope: "split", SplitDomains: []string{"corp.example"}},
				},
			},
		},
		Node: clientapi.Node{AssignedIP: "100.91.0.2"},
	}, WireGuardRenderOptions{})
	if strings.Contains(rendered, "DNS =") {
		t.Fatalf("static config flattened split DNS into a global override:\n%s", rendered)
	}
}

func TestRenderWireGuardIncludesTypedGlobalOverride(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Network: clientapi.Network{
			DNSConfig: &clientapi.DNSConfig{
				OverrideLocalDNS: true,
				Nameservers: []clientapi.DNSNameserver{
					{ID: "primary", Address: "192.0.2.53", Scope: "global"},
					{ID: "backup", Address: "192.0.2.54", Scope: "global"},
				},
			},
		},
		Node: clientapi.Node{AssignedIP: "100.91.0.2"},
	}, WireGuardRenderOptions{})
	if !strings.Contains(rendered, "DNS = 192.0.2.53, 192.0.2.54") {
		t.Fatalf("static config omitted typed global override:\n%s", rendered)
	}
}

func TestRenderWireGuardOmitsListenPortWhenUnset(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
	}, WireGuardRenderOptions{})

	if strings.Contains(rendered, "ListenPort") {
		t.Fatalf("rendered config should not include ListenPort:\n%s", rendered)
	}
}

func TestRenderWireGuardIncludesExplicitMTU(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
	}, WireGuardRenderOptions{MTU: 1280})

	if !strings.Contains(rendered, "MTU = 1280") {
		t.Fatalf("rendered config missing explicit MTU:\n%s", rendered)
	}
}

func TestRenderWireGuardIncludesExplicitRouteTable(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
	}, WireGuardRenderOptions{RouteTable: "51820"})

	if !strings.Contains(rendered, "Table = 51820") {
		t.Fatalf("rendered config missing explicit route table:\n%s", rendered)
	}
}

func TestRenderWireGuardIncludesIPv6Overlay(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP:   "100.91.0.2",
			AssignedIPv6: "fd7a:115c:a1e0::2",
		},
		Peers: []clientapi.Peer{
			{
				ID:         "peer-a",
				Hostname:   "peer-a",
				PublicKey:  "peer-public-key",
				AllowedIPs: []string{"100.91.0.3/32", "fd7a:115c:a1e0::3/128"},
			},
		},
	}, WireGuardRenderOptions{})

	for _, want := range []string{
		"Address = 100.91.0.2/32, fd7a:115c:a1e0::2/128",
		"AllowedIPs = 100.91.0.3/32, fd7a:115c:a1e0::3/128",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderWireGuardPrefersSameLANEndpointCandidate(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
		Peers: []clientapi.Peer{
			{
				ID:                 "peer-a",
				Hostname:           "peer-a",
				PublicKey:          "peer-public-key",
				AllowedIPs:         []string{"100.91.0.3/32"},
				Endpoint:           "198.51.100.20:51820",
				EndpointCandidates: []string{"203.0.113.20:51820", "192.168.55.7:51820"},
			},
		},
	}, WireGuardRenderOptions{
		Interfaces: []NetworkInterfaceStatus{
			{Name: "eth0", Flags: []string{"up", "broadcast"}, Prefixes: []string{"192.168.55.12/24"}},
		},
	})

	if !strings.Contains(rendered, "Endpoint = 192.168.55.7:51820") {
		t.Fatalf("rendered config did not prefer LAN candidate:\n%s", rendered)
	}
	if strings.Contains(rendered, "Endpoint = 198.51.100.20:51820") {
		t.Fatalf("rendered config used public endpoint instead of LAN candidate:\n%s", rendered)
	}
	if strings.Contains(rendered, "PersistentKeepalive") {
		t.Fatalf("rendered same-LAN candidate should not include PersistentKeepalive:\n%s", rendered)
	}
}

func TestRenderWireGuardPrefersSameLANIPv6EndpointCandidate(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
		Peers: []clientapi.Peer{
			{
				ID:                 "peer-a",
				Hostname:           "peer-a",
				PublicKey:          "peer-public-key",
				AllowedIPs:         []string{"100.91.0.3/32"},
				Endpoint:           "198.51.100.20:51820",
				EndpointCandidates: []string{"[2001:db8:55::7]:51820", "203.0.113.20:51820"},
			},
		},
	}, WireGuardRenderOptions{
		Interfaces: []NetworkInterfaceStatus{
			{Name: "eth0", Flags: []string{"up", "broadcast"}, Prefixes: []string{"2001:db8:55::12/64"}},
		},
	})

	if !strings.Contains(rendered, "Endpoint = [2001:db8:55::7]:51820") {
		t.Fatalf("rendered config did not prefer same-prefix IPv6 candidate:\n%s", rendered)
	}
	if strings.Contains(rendered, "Endpoint = 198.51.100.20:51820") {
		t.Fatalf("rendered config used public endpoint instead of IPv6 candidate:\n%s", rendered)
	}
	if strings.Contains(rendered, "PersistentKeepalive") {
		t.Fatalf("rendered same-LAN IPv6 candidate should not include PersistentKeepalive:\n%s", rendered)
	}
}

func TestRenderWireGuardUsesIPv4CandidateWhenLocalHostHasOnlyIPv4(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
		Peers: []clientapi.Peer{
			{
				ID:                 "peer-a",
				Hostname:           "peer-a",
				PublicKey:          "peer-public-key",
				AllowedIPs:         []string{"100.91.0.3/32"},
				Endpoint:           "[2001:db8:55::7]:51820",
				EndpointCandidates: []string{"198.51.100.20:51820"},
			},
		},
	}, WireGuardRenderOptions{
		Interfaces: []NetworkInterfaceStatus{
			{Name: "eth0", Flags: []string{"up", "broadcast"}, Addresses: []string{"192.168.55.12"}, Prefixes: []string{"192.168.55.12/24"}},
		},
	})

	if !strings.Contains(rendered, "Endpoint = 198.51.100.20:51820") {
		t.Fatalf("rendered config did not use IPv4 candidate on IPv4-only host:\n%s", rendered)
	}
	if strings.Contains(rendered, "Endpoint = [2001:db8:55::7]:51820") {
		t.Fatalf("rendered config used IPv6 endpoint on IPv4-only host:\n%s", rendered)
	}
}

func TestRenderWireGuardUsesIPv4CandidateWhenPrimaryEndpointIsIPv6(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
		Peers: []clientapi.Peer{
			{
				ID:                 "peer-a",
				Hostname:           "peer-a",
				PublicKey:          "peer-public-key",
				AllowedIPs:         []string{"100.91.0.3/32"},
				Endpoint:           "[2001:db8:55::7]:51820",
				EndpointCandidates: []string{"198.51.100.20:51820"},
			},
		},
	}, WireGuardRenderOptions{})

	if !strings.Contains(rendered, "Endpoint = 198.51.100.20:51820") {
		t.Fatalf("rendered config did not use IPv4 candidate for IPv6 primary endpoint:\n%s", rendered)
	}
	if strings.Contains(rendered, "Endpoint = [2001:db8:55::7]:51820") {
		t.Fatalf("rendered config used IPv6 endpoint despite IPv4 candidate:\n%s", rendered)
	}
}

func TestRenderWireGuardUsesIPv6EndpointWhenLocalHostHasIPv6(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
		Peers: []clientapi.Peer{
			{
				ID:                 "peer-a",
				Hostname:           "peer-a",
				PublicKey:          "peer-public-key",
				AllowedIPs:         []string{"100.91.0.3/32"},
				Endpoint:           "[2001:db8:56::7]:51820",
				EndpointCandidates: []string{"198.51.100.20:51820"},
			},
		},
	}, WireGuardRenderOptions{
		Interfaces: []NetworkInterfaceStatus{
			{Name: "eth0", Flags: []string{"up", "broadcast"}, Addresses: []string{"2001:db8:55::12"}, Prefixes: []string{"2001:db8:55::12/64"}},
		},
	})

	if !strings.Contains(rendered, "Endpoint = [2001:db8:56::7]:51820") {
		t.Fatalf("rendered config did not use IPv6 endpoint on IPv6-capable host:\n%s", rendered)
	}
	if strings.Contains(rendered, "Endpoint = 198.51.100.20:51820") {
		t.Fatalf("rendered config used IPv4 endpoint on IPv6-only host:\n%s", rendered)
	}
}

func TestRenderWireGuardKeepsStaticLANEndpointAlive(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
		Peers: []clientapi.Peer{
			{
				ID:         "peer-a",
				Hostname:   "peer-a",
				PublicKey:  "peer-public-key",
				AllowedIPs: []string{"100.91.0.3/32"},
				Endpoint:   "192.168.55.7:51820",
			},
		},
	}, WireGuardRenderOptions{
		Interfaces: []NetworkInterfaceStatus{
			{Name: "eth0", Flags: []string{"up", "broadcast"}, Prefixes: []string{"192.168.55.12/24"}},
		},
	})

	if !strings.Contains(rendered, "Endpoint = 192.168.55.7:51820") || !strings.Contains(rendered, "PersistentKeepalive = 25") {
		t.Fatalf("rendered static LAN endpoint should keep PersistentKeepalive:\n%s", rendered)
	}
}

func TestRenderWireGuardUsesMappedDirectCandidateWhenPrimaryEndpointMissing(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
		Peers: []clientapi.Peer{
			{
				ID:                 "peer-a",
				Hostname:           "peer-a",
				PublicKey:          "peer-public-key",
				AllowedIPs:         []string{"100.91.0.3/32"},
				EndpointCandidates: []string{"198.51.100.20:51820"},
			},
		},
	}, WireGuardRenderOptions{})

	if !strings.Contains(rendered, "Endpoint = 198.51.100.20:51820") {
		t.Fatalf("rendered config did not use mapped direct candidate:\n%s", rendered)
	}
	if !strings.Contains(rendered, "PersistentKeepalive = 25") {
		t.Fatalf("rendered mapped direct candidate should include PersistentKeepalive:\n%s", rendered)
	}
}

func TestRenderWireGuardIgnoresUnusableEndpointCandidates(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{
			AssignedIP: "100.91.0.2",
		},
		Peers: []clientapi.Peer{
			{
				ID:                 "peer-a",
				Hostname:           "peer-a",
				PublicKey:          "peer-public-key",
				AllowedIPs:         []string{"100.91.0.3/32"},
				Endpoint:           "198.51.100.20:51820",
				EndpointCandidates: []string{"127.0.0.1:51820", "169.254.20.1:51820", "10.7.0.4:51820"},
			},
		},
	}, WireGuardRenderOptions{
		Interfaces: []NetworkInterfaceStatus{
			{Name: "lo", Flags: []string{"up", "loopback"}, Prefixes: []string{"127.0.0.1/8"}},
			{Name: "wg0", Flags: []string{"up", "point_to_point"}, Prefixes: []string{"10.7.0.2/24"}},
		},
	})

	if !strings.Contains(rendered, "Endpoint = 198.51.100.20:51820") {
		t.Fatalf("rendered config did not fall back to public endpoint:\n%s", rendered)
	}
	if !strings.Contains(rendered, "PersistentKeepalive = 25") {
		t.Fatalf("rendered public fallback should include PersistentKeepalive:\n%s", rendered)
	}
	for _, bad := range []string{"127.0.0.1:51820", "169.254.20.1:51820", "10.7.0.4:51820"} {
		if strings.Contains(rendered, "Endpoint = "+bad) {
			t.Fatalf("rendered config used unusable candidate %s:\n%s", bad, rendered)
		}
	}
}

func TestRenderWireGuardUsesRelayDataplaneEndpointOverrides(t *testing.T) {
	response := clientapi.RegisterNodeResponse{
		Node: clientapi.Node{AssignedIP: "100.91.0.2"},
		Peers: []clientapi.Peer{
			{
				ID:         "peer-b",
				Hostname:   "peer-b",
				PublicKey:  "peer-b-public-key",
				AllowedIPs: []string{"100.91.0.3/32"},
				Endpoint:   "198.51.100.20:51820",
			},
			{
				ID:         "peer-a",
				Hostname:   "peer-a",
				PublicKey:  "peer-a-public-key",
				AllowedIPs: []string{"100.91.0.4/32"},
				Endpoint:   "198.51.100.21:51820",
			},
		},
	}
	overrides, err := RelayDataplaneEndpointOverrides(response.Peers, "127.0.0.1:62000")
	if err != nil {
		t.Fatal(err)
	}
	rendered := renderWireGuardValidated("private-key", response, WireGuardRenderOptions{
		ListenPort:            51820,
		PeerEndpointOverrides: overrides,
	})

	for _, want := range []string{
		"# peer-b (peer-b)",
		"Endpoint = 127.0.0.1:62001",
		"# peer-a (peer-a)",
		"Endpoint = 127.0.0.1:62000",
		"PersistentKeepalive = 25",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered relay dataplane config missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "198.51.100.20:51820") || strings.Contains(rendered, "198.51.100.21:51820") {
		t.Fatalf("rendered relay dataplane config leaked direct endpoints:\n%s", rendered)
	}
}

func TestRenderWireGuardSubnetRouterSNATHooksAreOptIn(t *testing.T) {
	response := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{CIDR: "100.91.0.0/24"},
		Node: clientapi.Node{
			AssignedIP:    "100.91.0.2",
			AdvertisedIPs: []string{"10.98.0.0/24"},
			AssignedIPv6:  "fd7a:115c:a1e0::2",
		},
	}
	withoutSNAT := renderWireGuardValidated("private-key", response, WireGuardRenderOptions{ListenPort: 51820})
	if strings.Contains(withoutSNAT, "MASQUERADE") || strings.Contains(withoutSNAT, "net.ipv4.ip_forward") {
		t.Fatalf("rendered config included subnet router hooks without opt-in:\n%s", withoutSNAT)
	}

	rendered := renderWireGuardValidated("private-key", response, WireGuardRenderOptions{
		ListenPort:       51820,
		SubnetRouterSNAT: true,
	})
	for _, want := range []string{
		"sysctl -w net.ipv4.ip_forward=1",
		"ip route get 10.98.0.0",
		"FORWARD -i %i -o \"$lan_if\" -s 100.91.0.0/24 -d 10.98.0.0/24 -j ACCEPT",
		"FORWARD -i \"$lan_if\" -o %i -s 10.98.0.0/24 -d 100.91.0.0/24 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
		"POSTROUTING -s 100.91.0.0/24 -d 10.98.0.0/24",
		"MASQUERADE",
		"PreDown = lan_if=",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered subnet router SNAT config missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderWireGuardExitRouterSNATSelectsDefaultInterface(t *testing.T) {
	response := clientapi.RegisterNodeResponse{
		Network: clientapi.Network{CIDR: "100.91.0.0/24"},
		Node:    clientapi.Node{AssignedIP: "100.91.0.2", AdvertisedIPs: []string{"0.0.0.0/0"}},
	}
	rendered := renderWireGuardValidated("private-key", response, WireGuardRenderOptions{SubnetRouterSNAT: true})
	if !strings.Contains(rendered, "ip route get 0.0.0.1") {
		t.Fatalf("exit router did not probe a concrete address through the default route:\n%s", rendered)
	}
	if strings.Contains(rendered, "ip route get 0.0.0.0") {
		t.Fatalf("exit router probed the locally routed unspecified address:\n%s", rendered)
	}
}

func TestRenderWireGuardExitLANBlockHooksAreOptIn(t *testing.T) {
	response := clientapi.RegisterNodeResponse{
		Node: clientapi.Node{AssignedIP: "100.91.0.2"},
		Peers: []clientapi.Peer{
			{
				ID:                 "exit-peer",
				Hostname:           "exit-peer",
				PublicKey:          "exit-public-key",
				AllowedIPs:         []string{"100.91.0.3/32", "0.0.0.0/0"},
				Endpoint:           "172.20.0.3:51820",
				EndpointCandidates: []string{"10.0.0.4:51820", "[fd00::4]:51820"},
			},
		},
	}
	withoutBlock := renderWireGuardValidated("private-key", response, WireGuardRenderOptions{ListenPort: 51820})
	if strings.Contains(withoutBlock, "ENLAN-") {
		t.Fatalf("rendered config included exit LAN block hooks without opt-in:\n%s", withoutBlock)
	}

	rendered := renderWireGuardValidated("private-key", response, WireGuardRenderOptions{
		ListenPort:   51820,
		ExitBlockLAN: true,
	})
	for _, want := range []string{
		"PostUp = iptables -N ENLAN-%i",
		"PostUp = iptables -A ENLAN-%i -d 10.0.0.4/32 -j RETURN",
		"PostUp = iptables -A ENLAN-%i -d 172.20.0.3/32 -j RETURN",
		"PostUp = iptables -A ENLAN-%i ! -o %i -d 10.0.0.0/8 -j REJECT",
		"PostUp = iptables -A ENLAN-%i ! -o %i -d 172.16.0.0/12 -j REJECT",
		"PostUp = iptables -A ENLAN-%i ! -o %i -d 192.168.0.0/16 -j REJECT",
		"PreDown = iptables -D OUTPUT -j ENLAN-%i",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered exit LAN block config missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "ip6tables") {
		t.Fatalf("IPv6 LAN block hooks rendered without IPv6 default route:\n%s", rendered)
	}
}

func TestRenderWireGuardExitLANBlockRequiresDefaultRoute(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{AssignedIP: "100.91.0.2"},
		Peers: []clientapi.Peer{
			{
				ID:         "peer",
				Hostname:   "peer",
				PublicKey:  "peer-public-key",
				AllowedIPs: []string{"100.91.0.3/32", "10.20.0.0/24"},
				Endpoint:   "172.20.0.3:51820",
			},
		},
	}, WireGuardRenderOptions{ExitBlockLAN: true})
	if strings.Contains(rendered, "ENLAN-") {
		t.Fatalf("rendered exit LAN block hooks without a default-route peer:\n%s", rendered)
	}
}

func TestRenderWireGuardIncludesACLPortFirewallHooks(t *testing.T) {
	rendered := renderWireGuardValidated("private-key", clientapi.RegisterNodeResponse{
		Node: clientapi.Node{AssignedIP: "100.91.0.2"},
		Peers: []clientapi.Peer{
			{
				ID:           "peer-a",
				Hostname:     "peer-a",
				PublicKey:    "peer-a-public-key",
				AllowedIPs:   []string{"100.91.0.3/32", "fd7a:115c:a1e0::3/128"},
				AllowedPorts: []clientapi.ACLPort{{Protocol: "udp", Port: 18132}, {Protocol: "udp", Port: 18132}, {Protocol: "tcp", Port: 443}},
			},
			{
				ID:         "peer-b",
				Hostname:   "peer-b",
				PublicKey:  "peer-b-public-key",
				AllowedIPs: []string{"100.91.0.4/32"},
			},
			{
				ID:            "peer-c",
				Hostname:      "peer-c",
				PublicKey:     "peer-c-public-key",
				AllowedIPs:    []string{"100.91.0.5/32"},
				ACLRestricted: true,
			},
			{
				ID:            "peer-d",
				Hostname:      "peer-d",
				PublicKey:     "peer-d-public-key",
				AllowedIPs:    []string{"100.91.0.6/32"},
				AllowedPorts:  []clientapi.ACLPort{{Protocol: "icmp"}},
				ACLRestricted: true,
			},
		},
	}, WireGuardRenderOptions{})

	for _, want := range []string{
		"PostUp = iptables -N ENACL-%i",
		"PostUp = iptables -A ENACL-%i -o %i -m conntrack --ctstate ESTABLISHED,RELATED --ctdir REPLY -j RETURN",
		"PostUp = iptables -A ENACL-%i -o %i -d 100.91.0.3/32 -p tcp --dport 443 -j RETURN",
		"PostUp = iptables -A ENACL-%i -o %i -d 100.91.0.3/32 -p udp --dport 18132 -j RETURN",
		"PostUp = iptables -A ENACL-%i -o %i -d 100.91.0.3/32 -j REJECT",
		"PostUp = iptables -A ENACL-%i -o %i -d 100.91.0.5/32 -j REJECT",
		"PostUp = iptables -A ENACL-%i -o %i -d 100.91.0.6/32 -p icmp -j RETURN",
		"PostUp = iptables -A ENACL-%i -o %i -d 100.91.0.6/32 -j REJECT",
		"PostUp = ip6tables -A ENACL-%i -o %i -d fd7a:115c:a1e0::3/128 -p udp --dport 18132 -j RETURN",
		"PreDown = iptables -D OUTPUT -j ENACL-%i",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "100.91.0.4/32 -j REJECT") {
		t.Fatalf("unrestricted peer should not receive ACL firewall hooks:\n%s", rendered)
	}
}

func TestLocalEndpointCandidatesFiltersUnusableInterfaces(t *testing.T) {
	candidates := LocalEndpointCandidates(51820, []NetworkInterfaceStatus{
		{Name: "lo", Flags: []string{"up", "loopback"}, Addresses: []string{"127.0.0.1"}},
		{Name: "eth0", Flags: []string{"up", "broadcast"}, Addresses: []string{"192.168.55.12", "169.254.10.2"}},
		{Name: "wg0", Flags: []string{"up", "point_to_point"}, Addresses: []string{"10.7.0.2"}},
		{Name: "down0", Flags: []string{"broadcast"}, Addresses: []string{"192.168.56.12"}},
		{Name: "eth1", Flags: []string{"up", "broadcast"}, Addresses: []string{"192.168.55.12"}},
	})

	if got, want := strings.Join(candidates, ","), "192.168.55.12:51820"; got != want {
		t.Fatalf("local endpoint candidates = %q, want %q", got, want)
	}
}

func TestLocalEndpointCandidatesIncludesUsableIPv6(t *testing.T) {
	candidates := LocalEndpointCandidates(51820, []NetworkInterfaceStatus{
		{Name: "eth0", Flags: []string{"up", "broadcast"}, Addresses: []string{"192.168.55.12", "2001:db8:55::12", "fe80::12", "::1"}},
		{Name: "wg0", Flags: []string{"up", "point_to_point"}, Addresses: []string{"2001:db8:55::99"}},
	})

	if got, want := strings.Join(candidates, ","), "192.168.55.12:51820,[2001:db8:55::12]:51820"; got != want {
		t.Fatalf("local endpoint candidates = %q, want %q", got, want)
	}
}
