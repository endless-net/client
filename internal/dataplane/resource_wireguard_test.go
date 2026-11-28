package dataplane

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/endless-net/client/internal/connectivity"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	"github.com/tailscale/wireguard-go/device"
)

func TestRecipientExitKeyAndIndependentSubnet(t *testing.T) {
	m, private := engineMap(t)
	now := time.Unix(1800000000, 0).UTC()
	end := now.Add(time.Minute)
	m.MapSignature = &coordinator.MapSignature{IssuedAt: now, ExpiresAt: end}
	m.Network.CIDR = "100.64.0.0/24"
	m.Node.ID = "recipient"
	peer := &m.Peers[0]
	peer.ID, peer.EndpointExpiresAt = "router", &end
	host := coordinator.ServiceHost{NodeID: peer.ID, PublicKey: peer.PublicKey}
	app := func(id, prefix string) coordinator.Application {
		return coordinator.Application{ID: id, TargetType: "cidr", Target: prefix,
			AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 443}}, Connectors: []coordinator.ServiceHost{host},
			Routes: []coordinator.ApplicationRoute{{Connector: host, CIDRs: []string{prefix}, Metric: 1, ExpiresAt: end}}}
	}
	exit, subnet := app("exit", "0.0.0.0/0"), app("office", "10.72.0.0/24")
	m.Network.Applications = []coordinator.Application{exit, subnet}
	config, _, _, err := configuration(m, private, "", nil, 4, 51820)
	if err != nil || !strings.Contains(string(config), "allowed_ip=0.0.0.0/0\n") {
		t.Fatal("missing exit in WireGuard configuration", err)
	}
	check := func(address, want string) {
		t.Helper()
		rules, _, err := recipientPacketRules(m)
		if err != nil || rules.lookup(netip.MustParseAddr(address)).peer != want {
			t.Fatalf("lookup %s, want %q: %v, %v", address, want, rules, err)
		}
	}
	check("8.8.8.8", host.NodeID)
	check("10.72.0.8", host.NodeID)
	check("10.73.0.8", "")
	check("100.64.0.5", "")
	m.Network.Applications = []coordinator.Application{subnet}
	check("8.8.8.8", "")
	check("10.72.0.8", host.NodeID)
	exit.Routes[0].Connector.PublicKey = m.Node.PublicKey
	m.Network.Applications = []coordinator.Application{exit}
	if _, _, err := recipientPacketRules(m); err == nil {
		t.Fatal("exit bound to wrong public key")
	}
}

func TestResourcePathSelectionAndWireGuardReplacement(t *testing.T) {
	m, private := engineMap(t)
	now := time.Unix(1800000000, 0).UTC()
	end := now.Add(time.Minute)
	m.MapSignature = &coordinator.MapSignature{IssuedAt: now, ExpiresAt: end}
	m.Node.ID = "source"
	m.Network.CIDR = "100.64.0.0/24"
	m.Peers[0].ID = "r1"
	m.Peers[0].EndpointExpiresAt = &end
	r2 := m.Peers[0]
	key, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{31}, 32))
	if err != nil {
		t.Fatal(err)
	}
	r2.ID, r2.PublicKey, r2.AllowedIPs = "r2", base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), []string{"100.64.0.3/32"}
	m.Peers = append(m.Peers, r2)
	resource := "10.72.0.0/24"
	app := coordinator.Application{ID: "printer", Target: resource, TargetType: "cidr", AllowedPorts: []coordinator.ServicePort{{Protocol: "tcp", Port: 443}}}
	for i, peer := range m.Peers {
		host := coordinator.ServiceHost{NodeID: peer.ID, PublicKey: peer.PublicKey}
		app.Connectors = append(app.Connectors, host)
		app.Routes = append(app.Routes, coordinator.ApplicationRoute{Connector: host, CIDRs: []string{resource}, ExpiresAt: end, Metric: uint32(20 - i*10), Masquerade: i == 1})
	}
	m.Network.Applications = []coordinator.Application{app}
	t.Run("domain addresses keep individual routers and DNS lease", func(t *testing.T) {
		domain := app
		domain.TargetType, domain.Target, domain.DNSEnabled = "domain", "printer.corp.example", true
		domain.PolicyHash = strings.Repeat("ab", 32)
		domain.Routes = append([]coordinator.ApplicationRoute(nil), app.Routes...)
		domain.Routes[0].CIDRs = []string{"10.72.0.1/32", "10.72.0.2/32"}
		domain.Routes[1].CIDRs = []string{"10.72.0.2/32", "10.72.0.3/32"}
		snapshot := m
		snapshot.Network.Applications = []coordinator.Application{domain}
		for _, down := range []string{"", "r2"} {
			rules, _, err := recipientRulesWithHealth(snapshot, func(h coordinator.ServiceHost) bool { return h.NodeID != down })
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"r1", "r2", "r2"}
			if down == "r2" {
				want = []string{"r1", "r1", ""}
			}
			for i, address := range []string{"10.72.0.1", "10.72.0.2", "10.72.0.3"} {
				if r := rules.lookup(netip.MustParseAddr(address)); r.peer != want[i] {
					t.Fatal(down, address, r.peer, want[i])
				}
			}
		}
		config, _, _, err := configuration(snapshot, private, "", nil, 4, 51820)
		if err != nil || strings.Count(string(config), "allowed_ip=10.72.0.") != 3 {
			t.Fatal("wireguard routes", err)
		}
		dns, err := connectivity.CompileMapDNS(snapshot, func(time.Time) bool { return true })
		if err != nil || !dns.ExactServiceDomain(domain.Target) {
			t.Fatal("missing domain assignment", err)
		}
		domain.Routes[0].CIDRs = []string{"10.72.0.0/24"}
		if _, _, err := recipientPacketRules(snapshot); err == nil {
			t.Fatal("domain supplied a whole subnet")
		}
	})
	read := func(at time.Time, down string, want string) {
		t.Helper()
		paths, err := connectivity.SelectResourcePaths(m, at, func(h coordinator.ServiceHost) bool { return h.NodeID != down })
		if err != nil {
			t.Fatal(err)
		}
		if want == "" {
			if len(paths) != 0 {
				t.Fatal(paths)
			}
			return
		}
		if len(paths) != 1 || paths[0].Route.Connector.NodeID != want || paths[0].Route.Masquerade != (want == "r2") {
			t.Fatal(paths)
		}
	}
	read(now, "", "r2")
	read(now, "r2", "r1")
	read(end, "", "")
	owner := func(config []byte) string {
		t.Helper()
		current := ""
		found := ""
		for _, line := range strings.Split(string(config), "\n") {
			if k, ok := strings.CutPrefix(line, "public_key="); ok {
				current = k
			}
			if line == "allowed_ip="+resource {
				if found != "" {
					t.Fatal("prefix assigned twice")
				}
				found = current
			}
		}
		return found
	}
	pubHex := func(peer coordinator.Peer) string {
		pub, err := base64.StdEncoding.DecodeString(peer.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(pub)
	}
	config, pub, peers, err := configuration(m, private, "", nil, 4, 51820)
	if err != nil || owner(config) != pubHex(r2) {
		t.Fatal("initial prefix owner", err)
	}
	engine, err := NewEngine(newInertTUN(), &inertBind{}, 4, 51820, engineLease(t))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	apply := func() [2]*device.Peer {
		t.Helper()
		if err := engine.apply(t.Context(), m, private); err != nil {
			t.Fatal(err)
		}
		actual := engine.device.(*device.Device)
		var sessions [2]*device.Peer
		for i, p := range m.Peers {
			sessions[i] = actual.LookupPeer(peerKey(t, p.PublicKey))
			if sessions[i] == nil {
				t.Fatal("missing peer after apply")
			}
		}
		return sessions
	}
	initial := apply()
	if initial != apply() {
		t.Fatal("identical assignment reset peer")
	}
	// A UAPI entry cannot survive the underlying path lease. Refuse to
	// apply a map that would leave a longer-lived native assignment.
	short := now.Add(time.Second)
	m.Network.Applications[0].Routes[1].ExpiresAt = short
	if _, _, _, err := configuration(m, private, pub, peers, 4, 51820); err == nil {
		t.Fatal("short path installed for full map lease")
	}
	read(short, "", "r1")
	m.Network.Applications[0].Routes[1].ExpiresAt = end
	other := app
	other.ID = "other"
	other.Routes = app.Routes[:1]
	other.Connectors = app.Connectors[:1]
	m.Network.Applications = append(m.Network.Applications, other)
	if _, _, _, err := configuration(m, private, pub, peers, 4, 51820); err == nil {
		t.Fatal("same prefix assigned to two cryptographic identities")
	}
	m.Network.Applications = m.Network.Applications[:1]
	m.Network.Applications[0].Routes[0].Metric = 10
	read(now, "", "r1")
	m.Network.Applications[0].Routes = app.Routes[:1]
	m.Network.Applications[0].Connectors = app.Connectors[:1]
	config, _, _, err = configuration(m, private, pub, peers, 4, 51820)
	if err != nil || owner(config) != pubHex(m.Peers[0]) || strings.Count(string(config), "replace_allowed_ips=true") != 2 {
		t.Fatal("prefix did not move with replacement", err)
	}
	moved := apply()
	if moved[0] == initial[0] || moved[1] == initial[1] {
		t.Fatal("subnet reassignment retained old crypto queues")
	}
	m.Network.Applications[0].Connectors[0].PublicKey = r2.PublicKey
	if _, _, _, err = configuration(m, private, pub, peers, 4, 51820); err == nil {
		t.Fatal("wrong router key accepted")
	}
	m.Network.Applications[0].Routes = nil
	m.Network.Applications[0].Connectors = nil
	config, _, _, err = configuration(m, private, pub, peers, 4, 51820)
	if err != nil || owner(config) != "" || strings.Count(string(config), "replace_allowed_ips=true") != 2 {
		t.Fatal("withdrawal retained resource prefix", err)
	}
	withdrawn := apply()
	if withdrawn[0] == moved[0] || withdrawn[1] != moved[1] {
		t.Fatal("withdrawal did not reset exactly the affected peer")
	}
}
