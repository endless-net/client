// Package testrelay implements a fixed-peer contract participant for native
// Client tests. It does not exercise the production Relay implementation.
package testrelay

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/netip"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	relay "github.com/endless-net/relay/protocol/v1"
)

type Server struct {
	Endpoint                      relay.Endpoint
	Credential                    relay.Credential
	CertificatePEM                []byte
	peerNetwork, peerID           string
	peerAddress                   *net.UDPAddr
	configureReturnPath           func(netip.AddrPort) error
	listener                      net.Listener
	mu                            sync.Mutex
	connections                   map[net.Conn]struct{}
	closed                        bool
	workers                       sync.WaitGroup
	unavailable                   atomic.Bool
	authenticated, sent, received atomic.Uint64
}

// New only forwards frames for the configured peer and accepts the credential
// issued for this test's Client. Keys stay in memory; only a public CA is exposed.
func New(t testing.TB, networkID, nodeID, peerID, peerAddress string, configureReturnPath func(netip.AddrPort) error) *Server {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("could not create reference Relay signing identity")
	}
	credential, err := relay.Sign(private, networkID, nodeID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal("could not issue reference Relay credential")
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal("could not create reference Relay certificate")
	}
	listener, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}})
	if err != nil {
		t.Fatal("could not listen for reference Relay TLS")
	}
	address, err := net.ResolveUDPAddr("udp4", peerAddress)
	if err != nil {
		_ = listener.Close()
		t.Fatal("invalid reference WireGuard endpoint")
	}
	s := &Server{Endpoint: relay.Endpoint{ID: "reference-relay", Addr: listener.Addr().String(), Protocol: relay.EndpointProtocolTLS}, Credential: *credential, CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), peerNetwork: networkID, peerID: peerID, peerAddress: address, listener: listener, connections: map[net.Conn]struct{}{}}
	s.workers.Add(1)
	s.configureReturnPath = configureReturnPath
	go s.accept()
	t.Cleanup(s.Close)
	return s
}

func (s *Server) accept() {
	defer s.workers.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.connections[conn] = struct{}{}
		s.workers.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.workers.Done()
			defer func() { _ = conn.Close(); s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock() }()
			s.serve(conn)
		}()
	}
}

func (s *Server) serve(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), relay.MaxFramePayloadBytes*2+4096)
	if !scanner.Scan() {
		return
	}
	var hello relay.ClientHello
	if relay.DecodeStrict(scanner.Bytes(), &hello) != nil || hello.Type != relay.MessageClientHello || hello.ProtocolVersion != relay.Version || !reflect.DeepEqual(hello.Credential, s.Credential) || s.unavailable.Load() {
		return
	}
	udp, err := net.DialUDP("udp4", nil, s.peerAddress)
	if err != nil {
		return
	}
	defer func() { _ = udp.Close() }()
	// The reference WireGuard engine does not learn roaming endpoints. Configure
	// this connection's return path before advertising readiness or forwarding.
	if s.configureReturnPath != nil && s.configureReturnPath(udp.LocalAddr().(*net.UDPAddr).AddrPort()) != nil {
		return
	}
	encoder := json.NewEncoder(conn)
	if encoder.Encode(relay.Ready{Type: relay.MessageReady, ProtocolVersion: relay.Version, Ready: true}) != nil {
		return
	}
	s.authenticated.Add(1)
	_ = conn.SetDeadline(time.Time{})
	done := make(chan struct{})
	var writers sync.WaitGroup
	var writeMu sync.Mutex
	write := func(value any) error { writeMu.Lock(); defer writeMu.Unlock(); return encoder.Encode(value) }
	writers.Add(1)
	go func() {
		defer writers.Done()
		buffer := make([]byte, relay.MaxFramePayloadBytes+1)
		for {
			n, err := udp.Read(buffer)
			if err != nil {
				return
			}
			if n == 0 || n > relay.MaxFramePayloadBytes || s.unavailable.Load() {
				continue
			}
			if write(relay.ServerFrame{Type: relay.MessageServerFrame, ProtocolVersion: relay.Version, FromNetworkID: s.peerNetwork, FromNodeID: s.peerID, Payload: buffer[:n]}) != nil {
				_ = conn.Close()
				return
			}
			s.received.Add(1)
		}
	}()
	if hello.HeartbeatIntervalMS >= relay.MinHeartbeatIntervalMS && hello.HeartbeatIntervalMS <= relay.MaxHeartbeatIntervalMS {
		writers.Add(1)
		go func() {
			defer writers.Done()
			ticker := time.NewTicker(time.Duration(hello.HeartbeatIntervalMS) * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case now := <-ticker.C:
					if write(relay.Heartbeat{Type: relay.MessageHeartbeat, ProtocolVersion: relay.Version, Time: now.UTC().Format(time.RFC3339Nano)}) != nil {
						_ = conn.Close()
						return
					}
				}
			}
		}()
	}
	defer func() { close(done); _ = udp.Close(); _ = conn.Close(); writers.Wait() }()
	for scanner.Scan() {
		var frame relay.ClientFrame
		if relay.DecodeStrict(scanner.Bytes(), &frame) != nil || frame.Type != relay.MessageClientFrame || frame.ProtocolVersion != relay.Version || frame.PeerNetworkID != s.peerNetwork || frame.PeerID != s.peerID || len(frame.Payload) == 0 || len(frame.Payload) > relay.MaxFramePayloadBytes {
			return
		}
		if s.unavailable.Load() {
			return
		}
		if _, err := udp.Write(frame.Payload); err != nil {
			return
		}
		s.sent.Add(1)
	}
}

// SetUnavailable interrupts existing TLS sessions and rejects new sessions.
// The listener remains available so the Client can recover at the same origin.
func (s *Server) SetUnavailable(value bool) {
	s.unavailable.Store(value)
	if value {
		s.mu.Lock()
		for conn := range s.connections {
			_ = conn.Close()
		}
		s.mu.Unlock()
	}
}

func (s *Server) Counts() (authenticated, toPeer, fromPeer uint64) {
	return s.authenticated.Load(), s.sent.Load(), s.received.Load()
}

func (s *Server) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	_ = s.listener.Close()
	for conn := range s.connections {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.workers.Wait()
}
