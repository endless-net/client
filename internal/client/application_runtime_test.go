package client

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	clientapi "github.com/endless-net/client-api/clientapi/v1"
	clientrpc "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client-api/clientapi/v1/clientrpc/clientrpcconnect"
	"github.com/tailscale/wireguard-go/tun"
	"github.com/tailscale/wireguard-go/tun/tuntest"
)

func signedApplicationFixture(t *testing.T, connector bool) (Config, clientapi.RegisterNodeResponse, ed25519.PrivateKey) {
	t.Helper()
	opts, key := signedServiceDNSFixture(t)
	m := opts.NetworkMap
	m.Network.Services = nil
	self := applicationSelf(m)
	other := clientapi.ServiceHost{NodeID: m.Peers[0].ID, PublicKey: m.Peers[0].PublicKey}
	source, host := self, other
	if connector {
		source, host = other, self
	}
	m.Network.Applications = []clientapi.Application{{ID: "app", Name: "portal", TargetType: "url", Target: "https://portal.example", PolicyHash: strings.Repeat("a", 64), DNSEnabled: true, Sources: []clientapi.ServiceHost{source}, Connectors: []clientapi.ServiceHost{host}, Routes: []clientapi.ApplicationRoute{{Connector: host, CIDRs: []string{"10.1.2.3/32"}, ExpiresAt: time.Now().Add(time.Minute)}}}}
	resignApplicationMap(t, &m, key)
	return Config{NodeID: m.Node.ID, NetworkID: m.Network.ID, MapSigningTrust: opts.SigningTrust, NodeCredential: "test-node-credential"}, m, key
}

func resignApplicationMap(t *testing.T, m *clientapi.RegisterNodeResponse, key ed25519.PrivateKey) {
	t.Helper()
	var err error
	m.MapSignature, err = clientapi.SignNetworkMap(key, *m)
	if err != nil {
		t.Fatal(err)
	}
}

func applicationTCPPacket(source, destination string, sourcePort, destinationPort uint16) []byte {
	packet := make([]byte, 40)
	packet[0], packet[9] = 0x45, 6
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], netip.MustParseAddr(source).AsSlice())
	copy(packet[16:20], netip.MustParseAddr(destination).AsSlice())
	binary.BigEndian.PutUint16(packet[20:22], sourcePort)
	binary.BigEndian.PutUint16(packet[22:24], destinationPort)
	packet[32] = 0x50
	return packet
}

func TestApplicationConnectorEnforcesEveryPacketAndWithdrawal(t *testing.T) {
	_, m, _ := signedApplicationFixture(t, true)
	filter := newApplicationPacketFilter()
	filter.update(m)
	now := time.Now()
	request := applicationTCPPacket("100.64.0.2", "10.1.2.3", 40000, 443)
	reply := applicationTCPPacket("10.1.2.3", "100.64.0.2", 443, 40000)
	if !filter.allows(request, true, now) || !filter.allows(reply, false, now) {
		t.Fatal("approved connection denied")
	}
	for name, packet := range map[string][]byte{
		"source":              applicationTCPPacket("100.64.0.99", "10.1.2.3", 40000, 443),
		"port":                applicationTCPPacket("100.64.0.2", "10.1.2.3", 40000, 80),
		"unknown destination": applicationTCPPacket("100.64.0.2", "10.1.2.4", 40000, 443),
		"truncated":           request[:25],
	} {
		if filter.allows(packet, true, now) {
			t.Fatalf("accepted %s", name)
		}
	}
	fragment := slices.Clone(request)
	fragment[6] = 0x20
	if filter.allows(fragment, true, now) {
		t.Fatal("fragment bypass")
	}
	if filter.allows(request, true, now.Add(2*time.Minute)) {
		t.Fatal("expired route accepted")
	}
	m.Network.Applications[0].Sources = nil
	filter.update(m)
	if filter.allows(request, true, now) || filter.allows(reply, false, now) {
		t.Fatal("existing flow survives source revocation")
	}
	m.Network.Applications = nil
	filter.update(m)
	if filter.allows(request, true, now) {
		t.Fatal("removed application became unrestricted forwarding")
	}
	restarted := newApplicationPacketFilter()
	restarted.update(m)
	if restarted.allows(request, true, now) {
		t.Fatal("restart permits stale OS forwarding")
	}
}

func TestApplicationSourceRoutesDNSAndSignature(t *testing.T) {
	cfg, m, key := signedApplicationFixture(t, false)
	router, err := buildWireGuardEngineRouterConfig("endlessnet", 1420, cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(router.Routes, netip.MustParsePrefix("10.1.2.3/32")) || !slices.Contains(router.DNSDomains, "portal.example") || router.DNSProxy == nil {
		t.Fatalf("route/DNS not installed: %+v", router)
	}
	if err := clientapi.VerifyNetworkMapSignatureWithTrustBundle(router.DNSProxy.NetworkMap, *cfg.MapSigningTrust); err != nil {
		t.Fatal("derived configuration mutated signed map", err)
	}
	response, err := DNSProxyResponse(t.Context(), dnsTestQuery(t, 1, "portal.example", dnsTypeA), *router.DNSProxy, time.Second)
	if err != nil || dnsTestAAnswer(t, response) != "10.1.2.3" {
		t.Fatal("signed DNS missing", err)
	}
	filter := newApplicationPacketFilter()
	filter.update(m)
	if !filter.allows(applicationTCPPacket("100.64.0.1", "10.1.2.3", 40000, 443), false, time.Now()) {
		t.Fatal("source denied")
	}
	if filter.allows(applicationTCPPacket("100.64.0.1", "10.1.2.3", 40000, 80), false, time.Now()) {
		t.Fatal("source port bypass")
	}
	m.Network.Applications[0].Routes[0].CIDRs[0] = "10.1.2.4/32"
	if _, err := buildWireGuardEngineRouterConfig("endlessnet", 1420, cfg, m); err == nil {
		t.Fatal("unsigned route accepted")
	}
	resignApplicationMap(t, &m, key)
	m.Network.Applications[0].Routes = nil
	resignApplicationMap(t, &m, key)
	router, err = buildWireGuardEngineRouterConfig("endlessnet", 1420, cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	response, err = DNSProxyResponse(t.Context(), dnsTestQuery(t, 1, "portal.example", dnsTypeA), *router.DNSProxy, time.Second)
	if err != nil || dnsTestRCode(response) != dnsRCodeNX {
		t.Fatal("withdrawn app fell through DNS", err)
	}
	if slices.Contains(router.Routes, netip.MustParsePrefix("10.1.2.3/32")) {
		t.Fatal("withdrawn route retained")
	}
}

type applicationResolverStub struct {
	addresses []netip.Addr
	err       error
	calls     int
}

func (r *applicationResolverStub) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.calls++
	return r.addresses, r.err
}

type applicationReporterStub struct {
	requests    []*clientrpc.ReportApplicationDiscoveryRequest
	credentials []string
}

func (r *applicationReporterStub) ReportApplicationDiscovery(_ context.Context, request *connect.Request[clientrpc.ReportApplicationDiscoveryRequest]) (*connect.Response[clientrpc.ReportApplicationDiscoveryResponse], error) {
	r.requests = append(r.requests, request.Msg)
	r.credentials = append(r.credentials, request.Header().Get("Authorization"))
	return connect.NewResponse(&clientrpc.ReportApplicationDiscoveryResponse{}), nil
}

func TestConnectorResolvesAndReportsUsingNativeNodeRPC(t *testing.T) {
	cfg, m, _ := signedApplicationFixture(t, true)
	reporter := &applicationReporterStub{}
	_, handler := clientrpcconnect.NewConnectorServiceHandler(reporter)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != clientrpcconnect.ConnectorServiceReportApplicationDiscoveryProcedure || r.Header.Get("Content-Type") != "application/proto" {
			t.Errorf("unexpected transport %s %s", r.URL.Path, r.Header.Get("Content-Type"))
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	cfg.ControlPlaneURLs = []string{server.URL}
	resolver := &applicationResolverStub{addresses: []netip.Addr{netip.MustParseAddr("10.1.2.3"), netip.MustParseAddr("10.1.2.3")}}
	reportApplicationDiscoveries(t.Context(), cfg, m, resolver, server.Client())
	if len(reporter.requests) != 1 || len(reporter.requests[0].Addresses) != 1 || reporter.requests[0].PolicyHash != m.Network.Applications[0].PolicyHash || reporter.credentials[0] != "Bearer test-node-credential" {
		t.Fatalf("invalid report: %+v", reporter)
	}
	resolver.err = errors.New("NXDOMAIN")
	reportApplicationDiscoveries(t.Context(), cfg, m, resolver, server.Client())
	if len(reporter.requests) != 2 || len(reporter.requests[1].Addresses) != 0 {
		t.Fatal("negative DNS did not withdraw")
	}
	m.Network.Applications[0].Target = "tampered.example"
	reportApplicationDiscoveries(t.Context(), cfg, m, resolver, server.Client())
	if len(reporter.requests) != 2 || resolver.calls != 2 {
		t.Fatal("unverified map triggered discovery")
	}
}

func TestApplicationRoutesReachRunningWireGuardAndRevoke(t *testing.T) {
	cfg, m, key := signedApplicationFixture(t, false)
	cfg.PrivateKey = testWireGuardEngineKey(1)
	m.Node.PublicKey, m.Peers[0].PublicKey = testWireGuardEnginePublicKey(1), testWireGuardEnginePublicKey(2)
	m.Network.Applications[0].Sources[0].PublicKey = m.Node.PublicKey
	m.Network.Applications[0].Connectors[0].PublicKey = m.Peers[0].PublicKey
	m.Network.Applications[0].Routes[0].Connector.PublicKey = m.Peers[0].PublicKey
	resignApplicationMap(t, &m, key)
	fakeTUN := tuntest.NewChannelTUN()
	router := &testWireGuardEngineRouter{}
	engine, err := NewWireGuardEngine(WireGuardEngineOptions{Interface: "endlessnet", router: router, tunFactory: func(string, int) (tun.Device, error) { return fakeTUN.TUN(), nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	if _, err := engine.Configure(t.Context(), cfg, m); err != nil {
		t.Fatal(err)
	}
	ipc, err := engine.device.IpcGet()
	if err != nil || !strings.Contains(ipc, "allowed_ip=10.1.2.3/32") || len(router.configured) != 1 {
		t.Fatal("application route missing from live WireGuard", err)
	}
	packet := applicationTCPPacket("100.64.0.1", "10.1.2.3", 40000, 443)
	if !engine.applicationFilter.allows(packet, false, time.Now()) {
		t.Fatal("live filter did not activate grant")
	}
	m.Network.Applications = nil
	resignApplicationMap(t, &m, key)
	if _, err := engine.Configure(t.Context(), cfg, m); err != nil {
		t.Fatal(err)
	}
	ipc, err = engine.device.IpcGet()
	if err != nil || strings.Contains(ipc, "allowed_ip=10.1.2.3/32") || engine.applicationFilter.allows(packet, false, time.Now()) {
		t.Fatal("live route/filter did not revoke", err)
	}
}

type applicationBatchTUN struct {
	tun.Device
	packets [][]byte
	written [][]byte
}

func (d *applicationBatchTUN) Write(bufs [][]byte, offset int) (int, error) {
	for _, buf := range bufs {
		d.written = append(d.written, slices.Clone(buf[offset:]))
	}
	return len(bufs), nil
}
func (d *applicationBatchTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	for i, p := range d.packets {
		copy(bufs[i][offset:], p)
		sizes[i] = len(p)
	}
	return len(d.packets), nil
}

func TestApplicationTUNFiltersBothDirectionsInBatches(t *testing.T) {
	_, m, _ := signedApplicationFixture(t, true)
	f := newApplicationPacketFilter()
	f.update(m)
	allowed := applicationTCPPacket("100.64.0.2", "10.1.2.3", 40000, 443)
	denied := applicationTCPPacket("100.64.0.99", "10.1.2.3", 40000, 443)
	underlying := &applicationBatchTUN{}
	wrapper := &applicationTUN{Device: underlying, filter: f}
	if n, err := wrapper.Write([][]byte{denied, allowed}, 0); err != nil || n != 2 || len(underlying.written) != 1 || !slices.Equal(underlying.written[0], allowed) {
		t.Fatal("inbound bypass or batch corruption", n, err)
	}
	reply := applicationTCPPacket("10.1.2.3", "100.64.0.2", 443, 40000)
	underlying.packets = [][]byte{applicationTCPPacket("10.1.2.4", "100.64.0.2", 443, 40000), reply}
	bufs, sizes := [][]byte{make([]byte, 128), make([]byte, 128)}, make([]int, 2)
	if n, err := wrapper.Read(bufs, sizes, 16); err != nil || n != 1 || !slices.Equal(bufs[0][16:16+sizes[0]], reply) {
		t.Fatal("reply filter bypass or batch corruption", n, err)
	}
}

func TestApplicationConnectorChoiceAndRouteCollision(t *testing.T) {
	cfg, m, key := signedApplicationFixture(t, false)
	backup := clientapi.ServiceHost{NodeID: "backup", PublicKey: m.Peers[0].PublicKey}
	m.Peers = append(m.Peers, clientapi.Peer{ID: backup.NodeID, Hostname: "backup", PublicKey: backup.PublicKey, AllowedIPs: []string{"100.64.0.3/32"}})
	m.Network.Applications[0].Connectors = append(m.Network.Applications[0].Connectors, backup)
	m.Network.Applications[0].Routes = append(m.Network.Applications[0].Routes, clientapi.ApplicationRoute{Connector: backup, CIDRs: []string{"10.1.2.3/32"}, ExpiresAt: time.Now().Add(time.Minute)})
	resignApplicationMap(t, &m, key)
	if err := verifyApplicationMap(cfg, m); err != nil {
		t.Fatal(err)
	}
	peers := applicationRoutePeers(m, time.Now())
	if slices.Contains(peers[0].AllowedIPs, "10.1.2.3/32") || !slices.Contains(peers[1].AllowedIPs, "10.1.2.3/32") {
		t.Fatal("connector selection is not deterministic")
	}
	m.Network.Applications[0].Routes[1].ExpiresAt = time.Now().Add(-time.Second)
	peers = applicationRoutePeers(m, time.Now())
	if !slices.Contains(peers[0].AllowedIPs, "10.1.2.3/32") || slices.Contains(peers[1].AllowedIPs, "10.1.2.3/32") {
		t.Fatal("expired connector retained route")
	}
	m.Peers[1].AllowedIPs = append(m.Peers[1].AllowedIPs, "10.1.2.3/32")
	resignApplicationMap(t, &m, key)
	if err := verifyApplicationMap(cfg, m); err == nil {
		t.Fatal("conflicting generic peer route accepted")
	}
}

func TestApplicationIPv6Enforcement(t *testing.T) {
	_, m, _ := signedApplicationFixture(t, true)
	m.Network.IPv6CIDR = "fd00::/64"
	m.Node.AssignedIPv6 = "fd00::1"
	m.Peers[0].AllowedIPs = append(m.Peers[0].AllowedIPs, "fd00::2/128")
	m.Network.Applications[0].Routes[0].CIDRs = []string{"fd01::3/128"}
	f := newApplicationPacketFilter()
	f.update(m)
	packet := make([]byte, 60)
	packet[0], packet[6] = 0x60, 6
	binary.BigEndian.PutUint16(packet[4:6], 20)
	copy(packet[8:24], netip.MustParseAddr("fd00::2").AsSlice())
	copy(packet[24:40], netip.MustParseAddr("fd01::3").AsSlice())
	binary.BigEndian.PutUint16(packet[40:42], 40000)
	binary.BigEndian.PutUint16(packet[42:44], 443)
	packet[52] = 0x50
	if !f.allows(packet, true, time.Now()) {
		t.Fatal("IPv6 grant rejected")
	}
	packet[6] = 44
	if f.allows(packet, true, time.Now()) {
		t.Fatal("IPv6 fragment bypass")
	}
}
