package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"net"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func TestServiceDiscoveryThroughRunningDNSListener(t *testing.T) {
	opts, _ := signedServiceDNSFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	opts.ListenAddr = "127.0.0.1:0"
	opts.Ready = func(address string) { ready <- address }
	go func() { done <- ServeDNSProxy(ctx, opts) }()
	var address string
	select {
	case address = <-ready:
	case err := <-done:
		t.Fatal("DNS listener failed", err)
	case <-time.After(3 * time.Second):
		t.Fatal("DNS listener not ready")
	}
	conn, err := net.DialTimeout("udp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(dnsTestQuery(t, 1, "db.account.endlessnet", dnsTypeA)); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 1232)
	size, err := conn.Read(response)
	if err != nil {
		t.Fatal(err)
	}
	if got := dnsTestAAnswer(t, response[:size]); got != "100.64.0.2" {
		t.Fatalf("live resolver returned %s", got)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DNS listener did not stop")
	}
}

func signedServiceDNSFixture(t *testing.T) (DNSProxyOptions, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := clientapi.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(public))
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	response := clientapi.RegisterNodeResponse{Revision: clientapi.MapRevision{Network: 1}, Network: clientapi.Network{ID: "net", Name: "test", CIDR: "100.64.0.0/24", Revision: 1}, Node: clientapi.Node{ID: "local", NetworkID: "net", Hostname: "local", PublicKey: key, AssignedIP: "100.64.0.1", ApprovalState: "approved"}, Peers: []clientapi.Peer{{ID: "host", Hostname: "host", PublicKey: key, AllowedIPs: []string{"100.64.0.2/32"}}}}
	response.Network.Services = []clientapi.AdvertisedService{{ID: "db", Name: "db", DNSName: "db.account.endlessnet", Ports: []clientapi.ServicePort{{Protocol: "tcp", Port: 5432}}, ApprovalMode: "manual", ApprovalStatus: "approved", Hosts: []clientapi.ServiceHost{{NodeID: "host", PublicKey: key}}}}
	response.MapSignature, err = clientapi.SignNetworkMap(private, response)
	if err != nil {
		t.Fatal(err)
	}
	return DNSProxyOptions{NetworkMap: response, SigningTrust: &trust}, private
}

func TestServiceDiscoveryUsesSignedHosts(t *testing.T) {
	opts, _ := signedServiceDNSFixture(t)
	for _, name := range []string{"db.account.endlessnet", "host.db.account.endlessnet"} {
		response, err := DNSProxyResponse(t.Context(), dnsTestQuery(t, 1, name, dnsTypeA), opts, time.Second)
		if err != nil || dnsTestRCode(response) != dnsRCodeNoErr {
			t.Fatalf("discovery failed: %v / %v", err, response)
		}
		if dnsTestAAnswer(t, response) != "100.64.0.2" {
			t.Fatal("wrong approved host address")
		}
	}
	response, err := DNSProxyResponse(t.Context(), dnsTestQuery(t, 1, "_db._tcp.db.account.endlessnet", 33), opts, time.Second)
	if err != nil || dnsTestRCode(response) != dnsRCodeNoErr || binary.BigEndian.Uint16(response[6:8]) != 1 {
		t.Fatal("SRV discovery failed", err)
	}
	question, err := parseDNSQuestion(response)
	if err != nil {
		t.Fatal(err)
	}
	if port := binary.BigEndian.Uint16(response[question.QuestionEnd+16 : question.QuestionEnd+18]); port != 5432 {
		t.Fatalf("SRV port = %d", port)
	}
}

func TestServiceDiscoveryIsInstalledByRuntimeRouter(t *testing.T) {
	opts, private := signedServiceDNSFixture(t)
	cfg := Config{MapSigningTrust: opts.SigningTrust}
	router, err := buildWireGuardEngineRouterConfig("endlessnet", 1420, cfg, opts.NetworkMap)
	if err != nil {
		t.Fatal(err)
	}
	if router.DNSProxy == nil || router.DNSProxy.SigningTrust == nil || len(router.DNSDomains) != 1 || router.DNSDomains[0] != "db.account.endlessnet" {
		t.Fatal("runtime did not install catalog resolver")
	}
	next := cloneRegisterNodeResponse(opts.NetworkMap)
	next.Network.Services[0].Hosts = nil
	next.MapSignature, err = clientapi.SignNetworkMap(private, next)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := buildWireGuardEngineRouterConfig("endlessnet", 1420, cfg, next)
	if err != nil {
		t.Fatal(err)
	}
	if dnsProxyOptionsEqual(router.DNSProxy, revoked.DNSProxy) {
		t.Fatal("host revocation does not refresh runtime resolver")
	}
}

func TestServiceDiscoveryFailsClosed(t *testing.T) {
	for _, name := range []string{"tampered", "untrusted", "expired", "revoked", "unknown host", "key rotated", "unknown name"} {
		t.Run(name, func(t *testing.T) {
			opts, private := signedServiceDNSFixture(t)
			query := "db.account.endlessnet"
			switch name {
			case "tampered":
				opts.NetworkMap.Network.Services[0].Ports[0].Port = 22
			case "untrusted":
				opts.SigningTrust = nil
			case "expired":
				opts.NetworkMap.MapSignature, _ = clientapi.SignNetworkMapAt(private, opts.NetworkMap, time.Now().Add(-2*time.Hour), time.Hour)
			case "revoked":
				opts.NetworkMap.Network.Services[0].Hosts = nil
				opts.NetworkMap.MapSignature, _ = clientapi.SignNetworkMap(private, opts.NetworkMap)
			case "unknown host":
				opts.NetworkMap.Peers = nil
				opts.NetworkMap.MapSignature, _ = clientapi.SignNetworkMap(private, opts.NetworkMap)
			case "key rotated":
				opts.NetworkMap.Network.Services[0].Hosts[0].PublicKey = "other"
				opts.NetworkMap.MapSignature, _ = clientapi.SignNetworkMap(private, opts.NetworkMap)
			case "unknown name":
				query = "attacker.db.account.endlessnet"
			}
			response, err := DNSProxyResponse(t.Context(), dnsTestQuery(t, 1, query, dnsTypeA), opts, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if dnsTestRCode(response) == dnsRCodeNoErr {
				t.Fatal("invalid authorization returned DNS answer")
			}
		})
	}
}

func TestServiceMapCloneDetachesHostAuthorization(t *testing.T) {
	opts, _ := signedServiceDNSFixture(t)
	clone := cloneRegisterNodeResponse(opts.NetworkMap)
	clone.Network.Services[0].Hosts[0].PublicKey = "changed"
	if clientapi.VerifyNetworkMapSignatureWithTrustBundle(opts.NetworkMap, *opts.SigningTrust) != nil {
		t.Fatal("runtime clone shares signed host storage")
	}
}
