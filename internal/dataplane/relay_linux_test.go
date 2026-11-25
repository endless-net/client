package dataplane

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	relay "github.com/endless-net/relay/api/wire"
)

type linuxTestRelayClient struct {
	net.Conn
	mu sync.Mutex
}

func (c *linuxTestRelayClient) send(message relay.Message) error {
	raw, err := relay.MarshalMessage(message)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
	_, err = c.Write(raw)
	return err
}

// The fixture speaks the published Relay wire protocol on a real TLS socket
// and forwards encrypted frames. It uses only the maps' fixture signing key.
func startLinuxTestRelay(t *testing.T, directory string) string {
	t.Helper()
	address := net.IPv4(192, 0, 2, 254)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		IPAddresses: []net.IP{address}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "relay-ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp4", net.JoinHostPort(address.String(), "0"), &tls.Config{MinVersion: tls.VersionTLS13,
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	var lock sync.Mutex
	clients := make(map[string]*linuxTestRelayClient)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				client := &linuxTestRelayClient{Conn: conn}
				decoder := relay.NewDecoder(conn)
				_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
				message, err := decoder.Read()
				hello, ok := message.(*relay.ClientHello)
				if err != nil || !ok || !validLinuxTestRelayCredential(hello.Credential) {
					return
				}
				identity := hello.Credential.NodeID
				lock.Lock()
				clients[identity] = client
				lock.Unlock()
				defer func() {
					lock.Lock()
					defer lock.Unlock()
					if clients[identity] == client {
						delete(clients, identity)
					}
				}()
				if client.send(&relay.Ready{Type: relay.MessageReady, ProtocolVersion: relay.Version, Ready: true}) != nil {
					return
				}
				for {
					_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
					message, err := decoder.Read()
					if err != nil {
						return
					}
					switch frame := message.(type) {
					case *relay.Heartbeat:
						if client.send(frame) != nil {
							return
						}
					case *relay.ClientFrame:
						lock.Lock()
						target := clients[frame.PeerID]
						lock.Unlock()
						if target != nil && target != client && frame.PeerNetworkID == "network" {
							_ = target.send(&relay.ServerFrame{Type: relay.MessageServerFrame, ProtocolVersion: relay.Version,
								FromNodeID: identity, FromNetworkID: "network", Payload: frame.Payload})
						}
					}
				}
			}()
		}
	}()
	return listener.Addr().String()
}

func validLinuxTestRelayCredential(credential relay.Credential) bool {
	if credential.NetworkID != "network" || !time.Now().Before(credential.ExpiresAt) ||
		(credential.NodeID != "fixture-node-1" && credential.NodeID != "fixture-node-2") {
		return false
	}
	public := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{83}, 32)).Public().(ed25519.PublicKey)
	payload, err := relay.CredentialSigningPayload(credential.KeyID, credential.NetworkID, credential.NodeID, credential.ExpiresAt)
	if err != nil {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(credential.Signature)
	return err == nil && ed25519.Verify(public, payload, signature)
}
