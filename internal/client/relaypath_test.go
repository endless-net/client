package client

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"

	relay "github.com/endless-net/relay/protocol/v1"
	"github.com/endless-net/relay/relaytest"
)

func TestDialRelayFallsBackToNextEndpoint(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS := relayPathTestTLSConfigs(t)
	liveAddr := freeTCPAddrForRelayPathTest(t)
	deadAddr := freeTCPAddrForRelayPathTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	server := newRelayPathTestServer(t, liveAddr, publicKey, serverTLS)
	go func() {
		errCh <- server.ListenAndServe(ctx)
	}()
	waitTCPForRelayPathTest(t, liveAddr)
	credential, err := relay.Sign(privateKey, "net-1", "node-a", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	conn, result, err := DialRelay(context.Background(), []relay.Endpoint{
		{ID: "relay-dead", Addr: deadAddr, Protocol: "relay-v1-tls"},
		{ID: "relay-live", Addr: liveAddr, Protocol: "relay-v1-tls"},
	}, *credential, RelayDialOptions{Timeout: 200 * time.Millisecond, TLSConfig: clientTLS})
	if err != nil {
		t.Fatalf("DialRelay failed: %v; result=%#v", err, result)
	}
	defer func() { _ = conn.Close() }()
	if result.Selected == nil || result.Selected.ID != "relay-live" {
		t.Fatalf("selected relay = %#v", result.Selected)
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("attempts = %#v, want 2", result.Attempts)
	}
	if result.Attempts[0].Error == "" {
		t.Fatalf("first relay attempt unexpectedly succeeded: %#v", result.Attempts)
	}
	if result.Attempts[1].Error != "" {
		t.Fatalf("second relay attempt failed: %#v", result.Attempts)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("relay server stopped with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay server did not stop")
	}
}

func TestDialRelayRejectsUnsupportedProtocol(t *testing.T) {
	_, result, err := DialRelay(context.Background(), []relay.Endpoint{
		{ID: "relay-udp", Addr: "127.0.0.1:1", Protocol: "udp"},
	}, relay.Credential{}, RelayDialOptions{Timeout: 10 * time.Millisecond})
	if err == nil {
		t.Fatalf("DialRelay succeeded with unsupported protocol; result=%#v", result)
	}
	if len(result.Attempts) != 0 {
		t.Fatalf("invalid endpoint was dialed: %#v", result.Attempts)
	}
}

func TestRelayCandidateSelectionUsesRendezvousWithinTenMilliseconds(t *testing.T) {
	connA, peerA := net.Pipe()
	defer func() { _ = connA.Close() }()
	defer func() { _ = peerA.Close() }()
	connB, peerB := net.Pipe()
	defer func() { _ = connB.Close() }()
	defer func() { _ = peerB.Close() }()
	candidates := []measuredRelayCandidate{
		{endpoint: relay.Endpoint{ID: "relay-a"}, attempt: RelayDialAttempt{Duration: 5 * time.Millisecond}, conn: connA},
		{endpoint: relay.Endpoint{ID: "relay-b"}, attempt: RelayDialAttempt{Duration: 12 * time.Millisecond}, conn: connB},
	}
	ordered := orderMeasuredRelayCandidates(candidates, "node-stable")
	if len(ordered) != 2 {
		t.Fatalf("ordered candidates = %#v", ordered)
	}
	want := 0
	if relayRendezvousScore("node-stable", "relay-b") > relayRendezvousScore("node-stable", "relay-a") {
		want = 1
	}
	if ordered[0] != want {
		t.Fatalf("selected candidate index = %d, want rendezvous winner %d", ordered[0], want)
	}
}

func TestRelayCandidateSelectionUsesMinimumRTTOutsideWindow(t *testing.T) {
	connA, peerA := net.Pipe()
	defer func() { _ = connA.Close() }()
	defer func() { _ = peerA.Close() }()
	connB, peerB := net.Pipe()
	defer func() { _ = connB.Close() }()
	defer func() { _ = peerB.Close() }()
	candidates := []measuredRelayCandidate{
		{endpoint: relay.Endpoint{ID: "relay-a"}, attempt: RelayDialAttempt{Duration: 5 * time.Millisecond}, conn: connA},
		{endpoint: relay.Endpoint{ID: "relay-b"}, attempt: RelayDialAttempt{Duration: 20 * time.Millisecond}, conn: connB},
	}
	ordered := orderMeasuredRelayCandidates(candidates, "node-stable")
	if len(ordered) != 2 || ordered[0] != 0 {
		t.Fatalf("ordered candidates = %#v, want minimum RTT first", ordered)
	}
}

func TestRelayCandidateSelectionUsesMinimumRTTAtTenMillisecondBoundary(t *testing.T) {
	connA, peerA := net.Pipe()
	defer func() { _ = connA.Close() }()
	defer func() { _ = peerA.Close() }()
	connB, peerB := net.Pipe()
	defer func() { _ = connB.Close() }()
	defer func() { _ = peerB.Close() }()
	nodeID := "boundary"
	for relayRendezvousScore(nodeID, "relay-b") <= relayRendezvousScore(nodeID, "relay-a") {
		nodeID += "-next"
	}
	candidates := []measuredRelayCandidate{
		{endpoint: relay.Endpoint{ID: "relay-a"}, attempt: RelayDialAttempt{Duration: 5 * time.Millisecond}, conn: connA},
		{endpoint: relay.Endpoint{ID: "relay-b"}, attempt: RelayDialAttempt{Duration: 15 * time.Millisecond}, conn: connB},
	}
	ordered := orderMeasuredRelayCandidates(candidates, nodeID)
	if len(ordered) != 2 || ordered[0] != 0 {
		t.Fatalf("ordered candidates = %#v, want minimum RTT first at exact 10ms boundary", ordered)
	}
}

func TestDialRelayPrefersLowerPriorityPeerRelay(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS := relayPathTestTLSConfigs(t)
	publicAddr := freeTCPAddrForRelayPathTest(t)
	peerAddr := freeTCPAddrForRelayPathTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startRelayPathTestServer(t, ctx, publicAddr, publicKey, serverTLS)
	startRelayPathTestServer(t, ctx, peerAddr, publicKey, serverTLS)
	credential, err := relay.Sign(privateKey, "net-1", "node-a", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	conn, result, err := DialRelay(context.Background(), []relay.Endpoint{
		{ID: "relay-public", Addr: publicAddr, Protocol: "relay-v1-tls", Region: "eu-central", Priority: 40},
		{ID: "relay-peer", Addr: peerAddr, Protocol: "relay-v1-tls", Region: "eu-west", Priority: 10},
	}, *credential, RelayDialOptions{Timeout: 200 * time.Millisecond, TLSConfig: clientTLS})
	if err != nil {
		t.Fatalf("DialRelay failed: %v; result=%#v", err, result)
	}
	defer func() { _ = conn.Close() }()
	if result.Selected == nil || result.Selected.ID != "relay-peer" || result.Selected.Priority != 10 {
		t.Fatalf("selected relay = %#v, want prioritized peer relay", result.Selected)
	}
	if len(result.Attempts) != 1 || result.Attempts[0].ID != "relay-peer" || result.Attempts[0].Priority != 10 {
		t.Fatalf("attempts = %#v, want peer relay first", result.Attempts)
	}
}

func TestDialRelayFallsBackToPublicWhenPeerRelayFails(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS := relayPathTestTLSConfigs(t)
	publicAddr := freeTCPAddrForRelayPathTest(t)
	deadPeerAddr := freeTCPAddrForRelayPathTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startRelayPathTestServer(t, ctx, publicAddr, publicKey, serverTLS)
	credential, err := relay.Sign(privateKey, "net-1", "node-a", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	conn, result, err := DialRelay(context.Background(), []relay.Endpoint{
		{ID: "relay-public", Addr: publicAddr, Protocol: "relay-v1-tls", Priority: 40},
		{ID: "relay-peer-down", Addr: deadPeerAddr, Protocol: "relay-v1-tls", Priority: 10},
	}, *credential, RelayDialOptions{Timeout: 200 * time.Millisecond, TLSConfig: clientTLS})
	if err != nil {
		t.Fatalf("DialRelay failed: %v; result=%#v", err, result)
	}
	defer func() { _ = conn.Close() }()
	if result.Selected == nil || result.Selected.ID != "relay-public" || result.Selected.Priority != 40 {
		t.Fatalf("selected relay = %#v, want public fallback", result.Selected)
	}
	if len(result.Attempts) != 2 {
		t.Fatalf("attempts = %#v, want peer failure then public fallback", result.Attempts)
	}
	if result.Attempts[0].ID != "relay-peer-down" || result.Attempts[0].Error == "" {
		t.Fatalf("first attempt = %#v, want failed peer relay", result.Attempts[0])
	}
	if result.Attempts[1].ID != "relay-public" || result.Attempts[1].Error != "" {
		t.Fatalf("second attempt = %#v, want successful public relay", result.Attempts[1])
	}
}

func TestWatchRelayFailoverSwitchesAfterCurrentRelayStops(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS := relayPathTestTLSConfigs(t)
	addrA := freeTCPAddrForRelayPathTest(t)
	addrB := freeTCPAddrForRelayPathTest(t)
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	serverA := newRelayPathTestServer(t, addrA, publicKey, serverTLS)
	serverB := newRelayPathTestServer(t, addrB, publicKey, serverTLS)
	go func() {
		_ = serverA.ListenAndServe(ctxA)
	}()
	go func() {
		_ = serverB.ListenAndServe(ctxB)
	}()
	waitTCPForRelayPathTest(t, addrA)
	waitTCPForRelayPathTest(t, addrB)
	credential, err := relay.Sign(privateKey, "net-1", "node-a", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	watchCtx, cancelWatch := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancelWatch()
	results := make(chan struct {
		result RelayWatchResult
		err    error
	}, 1)
	go func() {
		result, err := WatchRelayFailover(watchCtx, []relay.Endpoint{
			{ID: "relay-a", Addr: addrA, Protocol: "relay-v1-tls", Priority: 0},
			{ID: "relay-b", Addr: addrB, Protocol: "relay-v1-tls", Priority: 10},
		}, *credential, RelayDialOptions{Timeout: 200 * time.Millisecond, TLSConfig: clientTLS, HeartbeatInterval: 100 * time.Millisecond})
		results <- struct {
			result RelayWatchResult
			err    error
		}{result: result, err: err}
	}()
	time.Sleep(250 * time.Millisecond)
	cancelA()
	select {
	case got := <-results:
		if got.err != nil {
			t.Fatalf("WatchRelayFailover error = %v; result=%#v", got.err, got.result)
		}
		if !got.result.Reconnected || !got.result.Switched {
			t.Fatalf("watch result = %#v, want switched reconnect", got.result)
		}
		if got.result.Initial == nil || got.result.Initial.ID != "relay-a" {
			t.Fatalf("initial relay = %#v, want relay-a", got.result.Initial)
		}
		if got.result.Current == nil || got.result.Current.ID != "relay-b" {
			t.Fatalf("current relay = %#v, want relay-b", got.result.Current)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WatchRelayFailover did not return after current relay stopped")
	}
}

func startRelayPathTestServer(t *testing.T, ctx context.Context, addr string, publicKey ed25519.PublicKey, serverTLS *tls.Config) {
	t.Helper()
	errCh := make(chan error, 1)
	server := newRelayPathTestServer(t, addr, publicKey, serverTLS)
	go func() {
		errCh <- server.ListenAndServe(ctx)
	}()
	waitTCPForRelayPathTest(t, addr)
	t.Cleanup(func() {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("relay server %s stopped with error: %v", addr, err)
			}
		case <-time.After(2 * time.Second):
		}
	})
}

func relayPathTestTrustBundle(t testing.TB, publicKey ed25519.PublicKey) relay.SigningTrustBundle {
	t.Helper()
	bundle, err := relay.NewSigningTrustBundle(base64.RawURLEncoding.EncodeToString(publicKey))
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func newRelayPathTestServer(t testing.TB, addr string, publicKey ed25519.PublicKey, serverTLS *tls.Config) *relaytest.Server {
	t.Helper()
	server, err := relaytest.NewServer(relaytest.Config{
		Addr:        addr,
		RelayID:     "relay-test",
		TLSConfig:   serverTLS,
		TrustBundle: relayPathTestTrustBundle(t, publicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestNewRelayTLSConfigRequiresTLS13(t *testing.T) {
	cfg := NewRelayTLSConfig(nil)
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatalf("relay TLS min version = %#x, want TLS 1.3", cfg.MinVersion)
	}
}

func TestAuthenticateRelayConnRejectsOversizedControlLine(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		reader := bufio.NewReader(serverConn)
		_, _ = reader.ReadBytes('\n')
		chunk := strings.Repeat("x", 4096)
		for written := 0; written <= MaxRelayControlLineBytes; written += len(chunk) {
			if _, err := io.WriteString(serverConn, chunk); err != nil {
				return
			}
		}
	}()

	err := authenticateRelayConnWithHeartbeat(clientConn, relay.Credential{}, 0)
	if err == nil || !strings.Contains(err.Error(), "relay control line exceeds") {
		t.Fatalf("relay authentication error = %v, want oversized relay control line rejection", err)
	}
	_ = clientConn.Close()
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("relay test peer did not stop")
	}
}

func TestAuthenticateRelayConnRejectsNonCanonicalResponses(t *testing.T) {
	for _, response := range []string{
		`{"type":"ready","ready":true}`,
		`{"type":"ready","protocol_version":2,"ready":true}`,
		`{"type":"ready","protocol_version":1,"ready":true,"legacy":true}`,
		`{"type":"ready","protocol_version":1,"ready":true} {}`,
		`{"type":"legacy_ready","protocol_version":1,"ready":true}`,
	} {
		t.Run(response, func(t *testing.T) {
			clientConnection, serverConnection := net.Pipe()
			defer func() { _ = clientConnection.Close() }()
			serverDone := make(chan error, 1)
			go func() {
				defer func() { _ = serverConnection.Close() }()
				line, err := bufio.NewReader(serverConnection).ReadBytes('\n')
				if err != nil {
					serverDone <- err
					return
				}
				var hello relay.ClientHello
				if err := relay.DecodeStrict(line, &hello); err != nil {
					serverDone <- err
					return
				}
				if hello.Type != relay.MessageClientHello || hello.ProtocolVersion != relay.Version {
					serverDone <- errors.New("client sent a non-canonical relay hello")
					return
				}
				_, err = io.WriteString(serverConnection, response+"\n")
				serverDone <- err
			}()
			if err := authenticateRelayConnWithHeartbeat(clientConnection, relay.Credential{}, 0); err == nil {
				t.Fatal("invalid relay response was accepted")
			}
			if err := <-serverDone; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRunRelayDataplaneBridgeForwardsUDPDatagrams(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS, clientTLS := relayPathTestTLSConfigs(t)
	relayAddr := freeTCPAddrForRelayPathTest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	server := newRelayPathTestServer(t, relayAddr, publicKey, serverTLS)
	go func() {
		errCh <- server.ListenAndServe(ctx)
	}()
	waitTCPForRelayPathTest(t, relayAddr)

	credentialA, err := relay.Sign(privateKey, "net-1", "node-a", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	credentialB, err := relay.Sign(privateKey, "net-1", "node-b", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	wgA := listenUDPForRelayBridgeTest(t)
	defer func() { _ = wgA.Close() }()
	wgB := listenUDPForRelayBridgeTest(t)
	defer func() { _ = wgB.Close() }()
	relayEndpoint := relay.Endpoint{ID: "relay-1", Addr: relayAddr, Protocol: "relay-v1-tls"}
	mapA := clientapi.RegisterNodeResponse{
		Relays:          []relay.Endpoint{relayEndpoint},
		RelayCredential: credentialA,
		Peers: []clientapi.Peer{{
			ID:       "node-b",
			Hostname: "node-b",
		}},
	}
	mapB := clientapi.RegisterNodeResponse{
		Relays:          []relay.Endpoint{relayEndpoint},
		RelayCredential: credentialB,
		Peers: []clientapi.Peer{{
			ID:       "node-a",
			Hostname: "node-a",
		}},
	}
	bridgeCtx, bridgeCancel := context.WithCancel(context.Background())
	defer bridgeCancel()
	readyA := make(chan RelayDataplaneBridgeStatus, 1)
	readyB := make(chan RelayDataplaneBridgeStatus, 1)
	doneA := make(chan error, 1)
	doneB := make(chan error, 1)
	go func() {
		doneA <- RunRelayDataplaneBridge(bridgeCtx, RelayDataplaneBridgeOptions{
			NetworkMap:          mapA,
			WireGuardListenAddr: wgA.LocalAddr().String(),
			Timeout:             time.Second,
			TLSConfig:           clientTLS,
			Ready:               func(status RelayDataplaneBridgeStatus) { readyA <- status },
		})
	}()
	go func() {
		doneB <- RunRelayDataplaneBridge(bridgeCtx, RelayDataplaneBridgeOptions{
			NetworkMap:          mapB,
			WireGuardListenAddr: wgB.LocalAddr().String(),
			Timeout:             time.Second,
			TLSConfig:           clientTLS,
			Ready:               func(status RelayDataplaneBridgeStatus) { readyB <- status },
		})
	}()
	statusA := requireRelayBridgeReadyForTest(t, readyA)
	_ = requireRelayBridgeReadyForTest(t, readyB)
	nodeBEndpoint := statusA.PeerEndpoints["node-b"]
	if nodeBEndpoint == "" {
		t.Fatalf("bridge A peer endpoints = %#v, missing node-b", statusA.PeerEndpoints)
	}
	nodeBAddr, err := net.ResolveUDPAddr("udp", nodeBEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("wireguard-over-relay")
	if _, err := wgA.WriteToUDP(payload, nodeBAddr); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 128)
	if err := wgB.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, _, err := wgB.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != string(payload) {
		t.Fatalf("relayed payload = %q, want %q", got, payload)
	}
	bridgeCancel()
	for _, done := range []chan error{doneA, doneB} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("bridge stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("bridge did not stop")
		}
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("relay server stopped with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay server did not stop")
	}
}

func freeTCPAddrForRelayPathTest(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func waitTCPForRelayPathTest(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("relay %s did not accept TCP connections: %v", addr, lastErr)
}

func listenUDPForRelayBridgeTest(t *testing.T) *net.UDPConn {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func requireRelayBridgeReadyForTest(t *testing.T, ready <-chan RelayDataplaneBridgeStatus) RelayDataplaneBridgeStatus {
	t.Helper()
	select {
	case status := <-ready:
		return status
	case <-time.After(2 * time.Second):
		t.Fatal("relay dataplane bridge did not become ready")
		return RelayDataplaneBridgeStatus{}
	}
}
