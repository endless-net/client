package client

import (
	"encoding/binary"
	"net/netip"
	"strconv"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestResourceCompilerServiceDenialPreservesTransportPortAndHostScope(t *testing.T) {
	for _, disabled := range []string{"tcp", "udp"} {
		t.Run(disabled, func(t *testing.T) {
			opts, key := signedServiceDNSFixture(t)
			source := opts.NetworkMap
			source.Network.Services[0].Ports = []api.ServicePort{{Protocol: "tcp", Port: 5432}, {Protocol: "udp", Port: 5432}}
			resignApplicationMap(t, &source, key)
			cfg := Config{NodeID: source.Node.ID, NetworkID: source.Network.ID, CachedMap: &source, MapSigningTrust: opts.SigningTrust,
				MapRevision: source.Network.Revision, MapGlobalRevision: source.Revision.Global,
				ResourcePreferences: map[string]bool{rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, "db\x00"+disabled+"\x005432"): false}}
			now := time.Now()
			rules, err := compileResourceDenials(cfg, now)
			if err != nil {
				t.Fatal(err)
			}
			filter := &resourcePacketFilter{}
			if err := filter.suspend(rules, source.MapSignature.ExpiresAt); err != nil {
				t.Fatal(err)
			}
			filter.commit()
			for _, inbound := range []bool{false, true} {
				for _, protocol := range []string{"tcp", "udp"} {
					for _, host := range []string{"100.64.0.2", "100.64.0.3"} {
						for _, port := range []uint16{5432, 5433} {
							from, to, srcPort, dstPort := "100.64.0.1", host, uint16(50000), port
							if inbound {
								from, to, srcPort, dstPort = to, from, dstPort, srcPort
							}
							packet := applicationTCPPacket(from, to, srcPort, dstPort)
							if protocol == "udp" {
								packet = packet[:28]
								packet[9] = 17
								binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
								binary.BigEndian.PutUint16(packet[24:26], 8)
							}
							denied := protocol == disabled && host == "100.64.0.2" && port == 5432
							if filter.allows(packet, inbound, now) == denied {
								t.Fatalf("wrong service scope: inbound=%v protocol=%s host=%s port=%d denied=%v", inbound, protocol, host, port, denied)
							}
						}
					}
				}
			}
		})
	}
}

func TestResourceCompilerSubnetOverlapAndApplicationPort(t *testing.T) {
	cfg, source, key := signedApplicationFixture(t, false)
	source.Peers[0].AllowedIPs = append(source.Peers[0].AllowedIPs, "198.18.0.0/15", "198.18.0.1/32")
	resignApplicationMap(t, &source, key)
	cfg.CachedMap, cfg.MapRevision, cfg.MapGlobalRevision = &source, source.Network.Revision, source.Revision.Global
	cfg.ResourcePreferences = map[string]bool{
		rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SUBNET, source.Peers[0].ID+"\x00198.18.0.0/15"): false,
		rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, source.Peers[0].ID):                       true,
		rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_APPLICATION, source.Network.Applications[0].ID): false,
	}
	now := time.Now()
	rules, err := compileResourceDenials(cfg, now)
	if err != nil {
		t.Fatal(err)
	}
	f := &resourcePacketFilter{}
	if err := f.suspend(rules, source.MapSignature.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	f.commit()
	if f.allows(applicationTCPPacket("100.64.0.1", "198.18.0.1", 50000, 80), false, now) {
		t.Fatal("enabled host reopened overlapping disabled subnet")
	}
	if f.allows(applicationTCPPacket("100.64.0.1", "10.1.2.3", 50000, 443), false, now) || !f.allows(applicationTCPPacket("100.64.0.1", "10.1.2.3", 50000, 80), false, now) {
		t.Fatal("application target port restriction lost")
	}
	if !f.allows(applicationTCPPacket("100.64.0.1", "100.64.0.2", 50000, 80), false, now) {
		t.Fatal("resource compiler expanded subnet denial to ordinary host")
	}
	cfg.ResourcePreferences[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, "missing")] = false
	if _, err := compileResourceDenials(cfg, now); err == nil {
		t.Fatal("stale local choice silently ignored")
	}
}

func TestResourceCompilerManagedServiceAndSignature(t *testing.T) {
	opts, key := signedServiceDNSFixture(t)
	source := opts.NetworkMap
	service := source.Network.Services[0]
	source.Network.ClientPolicy = &api.ClientPolicy{Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceService, ID: service.ID, Source: api.ClientPolicyAccount, PolicyID: "service", Locked: true, Enabled: false}}}
	resignApplicationMap(t, &source, key)
	cfg := Config{NodeID: source.Node.ID, NetworkID: source.Network.ID, CachedMap: &source, MapSigningTrust: opts.SigningTrust, MapRevision: source.Network.Revision, MapGlobalRevision: source.Revision.Global, ResourcePreferences: map[string]bool{}}
	for _, port := range service.Ports {
		cfg.ResourcePreferences[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, service.ID+"\x00"+port.Protocol+"\x00"+strconv.Itoa(int(port.Port)))] = true
	}
	rules, err := compileResourceDenials(cfg, time.Now())
	if err != nil || len(rules) == 0 {
		t.Fatal("locked service compiled no denials", err)
	}
	for _, port := range service.Ports {
		found := false
		for _, rule := range rules {
			if rule.port == uint16(port.Port) && rule.prefix.IsSingleIP() {
				found = true
			}
		}
		if !found {
			t.Fatal("service port missing from compiled policy", port)
		}
	}
	for _, rule := range rules {
		if !rule.prefix.IsValid() || rule.prefix == netip.MustParsePrefix("0.0.0.0/0") {
			t.Fatal("service compiled default route denial")
		}
	}
	source.Network.Services[0].Name = "tampered"
	if _, err := compileResourceDenials(cfg, time.Now()); err == nil {
		t.Fatal("compiler accepted tampered signed source")
	}
}
