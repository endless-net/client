package testrelay

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	relay "github.com/endless-net/relay/protocol/v1"
)

func TestFixedPeerContractForwardingAndRecovery(t *testing.T) {
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var configured atomic.Value
	go func() {
		defer close(done)
		buffer := make([]byte, 2048)
		for {
			n, address, err := udp.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			if endpoint, ok := configured.Load().(netip.AddrPort); !ok || endpoint != address.AddrPort() {
				continue
			}
			if _, err := udp.WriteToUDP(buffer[:n], address); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { _ = udp.Close(); <-done })
	s := New(t, "network", "client", "peer", udp.LocalAddr().String(), func(endpoint netip.AddrPort) error {
		configured.Store(endpoint)
		return nil
	})
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(s.CertificatePEM) {
		t.Fatal("invalid public fixture CA")
	}
	dial := func(credential relay.Credential, ready bool) net.Conn {
		t.Helper()
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp4", s.Endpoint.Addr, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13})
		if err != nil {
			t.Fatal("fixture TLS authentication failed")
		}
		t.Cleanup(func() { _ = conn.Close() })
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		if json.NewEncoder(conn).Encode(relay.ClientHello{Type: relay.MessageClientHello, ProtocolVersion: relay.Version, Credential: credential}) != nil {
			t.Fatal("could not send hello")
		}
		var response relay.Ready
		err = json.NewDecoder(conn).Decode(&response)
		if ready {
			if err != nil || !response.Ready || response.Type != relay.MessageReady || response.ProtocolVersion != relay.Version {
				t.Fatal("fixture did not authenticate known credential")
			}
		} else if err == nil {
			t.Fatal("fixture accepted denied authentication")
		}
		return conn
	}
	exchange := func(conn net.Conn) {
		t.Helper()
		payload := []byte("opaque-wireguard-datagram")
		frame := relay.ClientFrame{Type: relay.MessageClientFrame, ProtocolVersion: relay.Version, PeerNetworkID: "network", PeerID: "peer", Payload: payload}
		if json.NewEncoder(conn).Encode(frame) != nil {
			t.Fatal("could not send client frame")
		}
		var response relay.ServerFrame
		if json.NewDecoder(conn).Decode(&response) != nil || response.Type != relay.MessageServerFrame || response.ProtocolVersion != relay.Version || response.FromNetworkID != "network" || response.FromNodeID != "peer" || !bytes.Equal(response.Payload, payload) {
			t.Fatal("fixture did not preserve datagram and sender scope")
		}
	}
	dial(relay.Credential{}, false)
	conn := dial(s.Credential, true)
	exchange(conn)
	wrong := dial(s.Credential, true)
	if json.NewEncoder(wrong).Encode(relay.ClientFrame{Type: relay.MessageClientFrame, ProtocolVersion: relay.Version, PeerNetworkID: "foreign", PeerID: "peer", Payload: []byte("denied")}) != nil {
		t.Fatal("could not send denied frame")
	}
	if _, err := bufio.NewReader(wrong).ReadByte(); err == nil {
		t.Fatal("fixture accepted foreign peer scope")
	}
	s.SetUnavailable(true)
	if _, err := bufio.NewReader(conn).ReadByte(); err == nil {
		t.Fatal("unavailable fixture retained data session")
	}
	dial(s.Credential, false)
	s.SetUnavailable(false)
	exchange(dial(s.Credential, true))
	authenticated, sent, received := s.Counts()
	if authenticated < 3 || sent != 2 || received < 1 {
		t.Fatal("fixture forwarding observations missing")
	}
}
