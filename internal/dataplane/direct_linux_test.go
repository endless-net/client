package dataplane

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/endless-net/client/api/transport"
	"github.com/endless-net/client/internal/connectivity"
	"github.com/endless-net/client/internal/state"
	coordinator "github.com/endless-net/coordinator/api/enrollment"
	signing "github.com/endless-net/signing/api/contract"
)

func TestLinuxDirectBetweenNativeNamespaces(t *testing.T) {
	testLinuxNativePeers(t, false, false)
}

func TestLinuxRelayBetweenNativeNamespaces(t *testing.T) {
	testLinuxNativePeers(t, true, false)
}

func TestLinuxAllProtocolsBetweenNativeNamespaces(t *testing.T) {
	testLinuxNativePeers(t, false, true)
}

func testLinuxNativePeers(t *testing.T, withRelay, allProtocols bool) {
	t.Helper()
	if !linuxNetworkTest(t) {
		return
	}
	if role := os.Getenv("EN_DIRECT_TEST_ROLE"); role != "" {
		runLinuxDirectPeer(t, role, os.Getenv("EN_DIRECT_TEST_DIR"), withRelay, allProtocols)
		return
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 39*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	type peer struct {
		cmd  *exec.Cmd
		done chan error
		log  bytes.Buffer
	}
	peers := make(map[string]*peer)
	for _, name := range []string{"a", "b"} {
		p := &peer{done: make(chan error, 1)}
		p.cmd = exec.CommandContext(ctx, "unshare", "--net", "--", executable, "-test.run=^"+t.Name()+"$")
		p.cmd.Env = append(os.Environ(), "EN_DIRECT_TEST_ROLE="+name, "EN_DIRECT_TEST_DIR="+dir)
		if withRelay {
			p.cmd.Env = append(p.cmd.Env, "SSL_CERT_FILE="+filepath.Join(dir, "relay-ca.pem"))
		}
		p.cmd.Stdout, p.cmd.Stderr = &p.log, &p.log
		if err := p.cmd.Start(); err != nil {
			t.Fatal(err)
		}
		go func() { p.done <- p.cmd.Wait() }()
		peers[name] = p
	}
	for _, name := range []string{"a", "b"} {
		if !waitLinuxTestFile(ctx, filepath.Join(dir, "spawn-"+name)) {
			t.Fatalf("peer namespace %s did not start", name)
		}
	}
	if withRelay {
		runLinuxTestCommand(t, ctx, linuxIP, "link", "add", "en-under", "type", "bridge")
		runLinuxTestCommand(t, ctx, linuxIP, "address", "add", "192.0.2.254/24", "dev", "en-under")
		runLinuxTestCommand(t, ctx, linuxIP, "link", "set", "en-under", "up")
		address := startLinuxTestRelay(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "relay-address"), []byte(address), 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		runLinuxTestCommand(t, ctx, linuxIP, "link", "add", "en-under-a", "type", "veth", "peer", "name", "en-under-b")
	}
	for index, name := range []string{"a", "b"} {
		pid := strconv.Itoa(peers[name].cmd.Process.Pid)
		iface := "en-under-" + name
		if withRelay {
			upstream := "en-bridge-" + name
			runLinuxTestCommand(t, ctx, linuxIP, "link", "add", iface, "type", "veth", "peer", "name", upstream)
			runLinuxTestCommand(t, ctx, linuxIP, "link", "set", upstream, "master", "en-under")
			runLinuxTestCommand(t, ctx, linuxIP, "link", "set", upstream, "up")
		}
		runLinuxTestCommand(t, ctx, linuxIP, "link", "set", iface, "netns", pid)
		runLinuxTestCommand(t, ctx, "nsenter", "-t", pid, "-n", linuxIP, "link", "set", "lo", "up")
		runLinuxTestCommand(t, ctx, "nsenter", "-t", pid, "-n", linuxIP, "address", "add",
			"192.0.2."+strconv.Itoa(index+1)+"/24", "dev", iface)
		runLinuxTestCommand(t, ctx, "nsenter", "-t", pid, "-n", linuxIP, "link", "set", iface, "up")
		if withRelay {
			runLinuxTestCommand(t, ctx, "nsenter", "-t", pid, "-n", "/usr/sbin/nft", "add", "table", "inet", "underlay_test")
			runLinuxTestCommand(t, ctx, "nsenter", "-t", pid, "-n", "/usr/sbin/nft", "add", "chain", "inet", "underlay_test", "output", "{ type filter hook output priority 0; policy accept; }")
			runLinuxTestCommand(t, ctx, "nsenter", "-t", pid, "-n", "/usr/sbin/nft", "add", "rule", "inet", "underlay_test", "output", "oifname", iface, "meta", "l4proto", "udp", "drop")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "underlay-ready"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-peers["a"].done; err != nil {
		t.Errorf("client peer: %v\n%s", err, peers["a"].log.String())
	}
	if err := os.WriteFile(filepath.Join(dir, "stop"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-peers["b"].done; err != nil {
		t.Errorf("server peer: %v\n%s", err, peers["b"].log.String())
	}
}

func runLinuxDirectPeer(t *testing.T, role, directory string, withRelay, allProtocols bool) {
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	if err := os.WriteFile(filepath.Join(directory, "spawn-"+role), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if !waitLinuxTestFile(ctx, filepath.Join(directory, "underlay-ready")) {
		t.Fatal("underlay did not come ready")
	}
	var relayAddress string
	if withRelay {
		raw, err := os.ReadFile(filepath.Join(directory, "relay-address"))
		if err != nil {
			t.Fatal(err)
		}
		relayAddress = string(raw)
	}
	fixture := linuxDirectFixture(t, role, relayAddress)
	if allProtocols {
		peer := &fixture.snapshot.Peers[0]
		for _, grant := range [][]coordinator.ACLGrant{peer.ACLGrants, peer.IngressACLGrants} {
			grant[0].AllowedPorts = []coordinator.ACLPort{{Protocol: coordinator.ProtocolAll}}
		}
	}
	store, _ := hintStoreFixture(t, fixture)
	ledgerDir := t.TempDir()
	if err := os.Chmod(ledgerDir, 0700); err != nil {
		t.Fatal(err)
	}
	ledger := state.NewLinuxNetworkLedger(ledgerDir)
	resources, err := NewTransportResources(8)
	if err != nil {
		t.Fatal(err)
	}
	clock, err := connectivity.NewLinuxClock("linux-native-test")
	if err != nil {
		t.Fatal(err)
	}
	sample, err := clock.Sample()
	if err != nil {
		t.Fatal(err)
	}
	fixture.clock.sample = sample
	fixture.verify(t, nil)
	budget, err := connectivity.NewOperationBudget(clock, fixture.profile.ID, 1, sample, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewLinuxSession(ctx, store, ledger, clock, fixture.profile.ID, 1, LinuxSessionConfig{
		MTU: 1420, MaxPeers: 8, MaxGrants: 16, MaxProtected: 16, ListenPort: 51820,
		ProbeInterval: 50 * time.Millisecond, RetryInterval: time.Second,
		Relay: RelayConfig{DialTimeout: time.Second, IOTimeout: 3 * time.Second, HeartbeatInterval: time.Second, Now: time.Now},
	}, resources)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := session.Stop(shutdown); err != nil {
			t.Error(err)
		}
	})
	operation := connectivity.WithOperationBudget(ctx, budget)
	if err := session.Start(operation, fixture.verified); err != nil {
		t.Fatal(err)
	}
	if err := session.CheckReady(operation); err != nil {
		t.Fatal(err)
	}
	if err := budget.Complete(); err != nil {
		t.Fatal(err)
	}
	poll := func() error { return session.ProbePaths(ctx) }
	if withRelay {
		refresh := linuxTestTransportDiscovery(t, ctx, session, fixture, clock, directory, role)
		poll = func() error {
			refresh()
			return session.ProbePaths(ctx)
		}
	}
	if role == "b" {
		for _, port := range []string{"4100", "4101"} {
			linuxTestEcho(t, "100.70.0.2:"+port)
		}
		if err := os.WriteFile(filepath.Join(directory, "listeners-ready"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(filepath.Join(directory, "stop")); err == nil {
				break
			}
			if withRelay {
				if _, err := os.Stat(filepath.Join(directory, "enable-direct")); err == nil {
					enableLinuxTestDirect(t, ctx)
					withRelay = false
				}
			}
			if err := poll(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-ctx.Done():
				t.Fatal("client peer did not finish")
			case <-ticker.C:
			}
		}
		if _, err := os.Stat(filepath.Join(directory, "failed")); err == nil {
			logLinuxDirectTest(t, session)
			t.Fail()
		}
		return
	}
	if !waitLinuxTestFile(ctx, filepath.Join(directory, "listeners-ready")) {
		t.Fatal("server peer did not bind")
	}
	checkLinuxNativeTraffic(t, ctx, session, directory, withRelay, allProtocols, poll)
	if allProtocols {
		checkLinuxICMPEcho(t, ctx)
	}
	if withRelay {
		enableLinuxTestDirect(t, ctx)
		if err := os.WriteFile(filepath.Join(directory, "enable-direct"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		checkLinuxNativeTraffic(t, ctx, session, directory, false, allProtocols, poll)
	}
	connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "udp4", "100.70.0.2:4100")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if !linuxTestExchange(connection) {
		t.Fatal("could not establish UDP flow before loss of authority source")
	}
	epoch := session.BlockSource()
	if err := session.Offline(WithSourceEpoch(ctx, epoch)); err != nil {
		t.Fatal(err)
	}
	if !linuxTestExchange(connection) || linuxTestRoundTrip(ctx, "udp4", "100.70.0.2:4100") {
		t.Fatal("offline policy must preserve the existing flow and reject a new one")
	}
	session.RevokeSource()
	if linuxTestExchange(connection) || linuxTestRoundTrip(ctx, "tcp4", "100.70.0.2:4100") {
		t.Fatal("traffic survived source revocation")
	}
}

func enableLinuxTestDirect(t *testing.T, ctx context.Context) {
	t.Helper()
	runLinuxTestCommand(t, ctx, "/usr/sbin/nft", "delete", "table", "inet", "underlay_test")
}

func checkLinuxICMPEcho(t *testing.T, ctx context.Context) {
	t.Helper()
	peer := &net.IPAddr{IP: net.ParseIP("100.70.0.2")}
	c, err := (&net.ListenConfig{}).ListenPacket(ctx, "ip4:icmp", "100.70.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	request := []byte{8, 0, 0, 0, 0x45, 0x4e, 0, 1, 1, 2, 3, 4}
	binary.BigEndian.PutUint16(request[2:4], packetChecksum(request, 0))
	if _, err := c.WriteTo(request, peer); err != nil {
		t.Fatal(err)
	}
	var reply [1500]byte
	n, from, err := c.ReadFrom(reply[:])
	if err != nil || from.String() != peer.String() || n != len(request) || reply[0] != 0 || !bytes.Equal(reply[4:n], request[4:]) {
		t.Fatalf("ICMP echo through the tunnel: %d bytes, %v", n, err)
	}
}

func checkLinuxNativeTraffic(t *testing.T, ctx context.Context, session *LinuxSession, directory string, withRelay, allProtocols bool, poll func() error) {
	t.Helper()
	var wantDirect, wantRelay uint32 = 1, 0
	if withRelay {
		wantDirect, wantRelay = 0, 1
	}
	for attempt := 0; ; attempt++ {
		view := session.ConnectionLocal()
		if view.DirectPaths == wantDirect && view.RelayPaths == wantRelay {
			break
		}
		if attempt == 260 || ctx.Err() != nil {
			_ = os.WriteFile(filepath.Join(directory, "failed"), nil, 0600)
			logLinuxDirectTest(t, session)
			t.Fatalf("expected %d direct and %d relay paths, got %d and %d", wantDirect, wantRelay, view.DirectPaths, view.RelayPaths)
		}
		if err := poll(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, network := range []string{"tcp4", "udp4"} {
		for attempt := 0; !linuxTestRoundTrip(ctx, network, "100.70.0.2:4100"); attempt++ {
			if attempt == 26 || ctx.Err() != nil {
				_ = os.WriteFile(filepath.Join(directory, "failed"), nil, 0600)
				logLinuxDirectTest(t, session)
				t.Fatalf("approved %s packet did not traverse selected path", network)
			}
			if err := poll(); err != nil {
				t.Fatal(err)
			}
		}
		if got := linuxTestRoundTrip(ctx, network, "100.70.0.2:4101"); got != allProtocols {
			t.Fatalf("%s port 4101 reachable=%v with all protocols=%v", network, got, allProtocols)
		}
	}
	if err := session.ProbePaths(ctx); err != nil {
		t.Fatal(err)
	}
	if view := session.ConnectionLocal(); view.DirectPaths != wantDirect || view.RelayPaths != wantRelay {
		t.Fatalf("unexpected confirmed path: %d direct %d relay", view.DirectPaths, view.RelayPaths)
	}
}

// Model the Coordinator republishing signed maps after both peers advertise
// the dynamic ports of their actual prepared UDP sockets.
func linuxTestTransportDiscovery(t *testing.T, ctx context.Context, session *LinuxSession, f *candidateFixture, clock connectivity.Clock, directory, role string) func() {
	t.Helper()
	localRole, remoteRole := role, "a"
	address := "192.0.2.2"
	if role == "a" {
		remoteRole, address = "b", "192.0.2.1"
	}
	localPath, remotePath := filepath.Join(directory, "ad-"+localRole), filepath.Join(directory, "ad-"+remoteRole)
	var published, previousLocal, previousRemote []byte
	return func() {
		t.Helper()
		local, err := session.TransportAdvertisement(netip.MustParseAddr(address))
		if err != nil {
			t.Fatal(err)
		}
		localRaw, err := json.Marshal(local)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(published, localRaw) {
			if err := os.WriteFile(localPath+".tmp", localRaw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(localPath+".tmp", localPath); err != nil {
				t.Fatal(err)
			}
			published = localRaw
		}
		remoteRaw, err := os.ReadFile(remotePath)
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(previousLocal, localRaw) && bytes.Equal(previousRemote, remoteRaw) {
			return
		}
		var remote coordinator.TransportAdvertisement
		if err := json.Unmarshal(remoteRaw, &remote); err != nil {
			t.Fatal(err)
		}
		f.snapshot.Node.Transport = &local
		p := &f.snapshot.Peers[0]
		p.Transport.PeerAdvertisement = remote
		offer := &p.Transport.Profiles[0]
		offer.LocalEndpoints = coordinator.ProfileEndpoints(local, offer.Profile.ID, offer.Digest)
		offer.PeerEndpoints = coordinator.ProfileEndpoints(remote, offer.Profile.ID, offer.Digest)
		cursor := f.verified.Cursor()
		f.snapshot.Revision.Network++
		f.snapshot.Network.Revision++
		f.clock.sample, err = clock.Sample()
		if err != nil {
			t.Fatal(err)
		}
		endpointExpires := f.clock.sample.Wall.Add(time.Minute)
		f.snapshot.Node.EndpointExpiresAt = &endpointExpires
		p.EndpointExpiresAt = &endpointExpires
		f.verify(t, &cursor)
		if err := session.Refresh(ctx, f.verified); err != nil {
			t.Fatal(err)
		}
		previousLocal, previousRemote = localRaw, remoteRaw
	}
}

func logLinuxDirectTest(t *testing.T, session *LinuxSession) {
	t.Helper()
	t.Logf("transport %+v, switch error %v", session.ConnectionLocal(), session.packets.Err())
	for _, command := range [][]string{{linuxIP, "-s", "link"}, {linuxIP, "-4", "route"}, {"/usr/sbin/nft", "list", "table", "inet", session.receipt.Table}} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		output, err := exec.CommandContext(ctx, command[0], command[1:]...).CombinedOutput()
		cancel()
		t.Logf("%v (%v):\n%s", command, err, output)
	}
}

func linuxDirectFixture(t *testing.T, role, relayAddress string) *candidateFixture {
	t.Helper()
	f := newCandidateFixture(t)
	index := 1
	if role == "b" {
		index = 2
	}
	other := 3 - index
	node, peer := "fixture-node-"+strconv.Itoa(index), "fixture-node-"+strconv.Itoa(other)
	private, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{byte(101 + index)}, 32))
	if err != nil {
		t.Fatal(err)
	}
	peerKey, err := ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{byte(101 + other)}, 32))
	if err != nil {
		t.Fatal(err)
	}
	f.profile.Keys.WireGuardPrivate = private.Bytes()
	f.profile.Attempt.NodeID = node
	var trust signing.ServerKeyResponse
	if err := json.Unmarshal(f.trust.Bundle, &trust); err != nil {
		t.Fatal(err)
	}
	keyID, expires := trust.NodeCredentialTrustBundle.ActiveKeyID, f.profile.Attempt.CredentialEnds
	scopes := []string{"node:register", "node:map", "node:endpoint"}
	payload, err := coordinator.NodeCredentialSigningPayload(keyID, "network", node, scopes, expires)
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{82}, 32)), payload)
	claims := coordinator.NodeCredentialClaims{Version: 3, Algorithm: "ed25519-node-credential-v3", KeyID: keyID,
		NetworkID: "network", NodeID: node, Scopes: scopes, ExpiresAt: expires, Signature: base64.RawURLEncoding.EncodeToString(signature)}
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	f.profile.Secrets.NodeCredential = "enc_" + base64.RawURLEncoding.EncodeToString(raw)
	f.snapshot.Node.ID, f.snapshot.Node.AssignedIP = node, "100.70.0."+strconv.Itoa(index)
	f.snapshot.Node.PublicKey = base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
	p := &f.snapshot.Peers[0]
	p.ID, p.Endpoint = peer, "192.0.2."+strconv.Itoa(other)+":51820"
	p.PublicKey = base64.StdEncoding.EncodeToString(peerKey.PublicKey().Bytes())
	p.AllowedIPs = []string{"100.70.0." + strconv.Itoa(other) + "/32"}
	ports := []coordinator.ACLPort{{Protocol: "tcp", Port: 4100}, {Protocol: "udp", Port: 4100}}
	p.ACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: p.AllowedIPs, AllowedPorts: ports}}
	p.IngressACLGrants = []coordinator.ACLGrant{{DestinationCIDRs: []string{f.snapshot.Node.AssignedIP + "/32"}, AllowedPorts: ports}}
	if relayAddress == "" {
		f.snapshot.Node.Transport, p.Transport = nil, nil
	} else {
		profile := cipherProfile(transport.WireGuard)
		digest, err := transport.ProfileDigest(profile)
		if err != nil {
			t.Fatal(err)
		}
		local := coordinator.TransportAdvertisement{IntentGeneration: 1,
			Capabilities: transport.Capabilities{Version: 1, Engines: []transport.Capability{{Engine: profile.Engine, ProtocolVersion: profile.ProtocolVersion, ProfileVersion: profile.Version}}},
			Endpoints:    []coordinator.TransportEndpoint{}}
		f.snapshot.Node.Transport = &local
		p.Transport.LocalNodeID, p.Transport.LocalPublicKey = node, f.snapshot.Node.PublicKey
		p.Transport.PeerNodeID, p.Transport.PeerPublicKey = peer, p.PublicKey
		p.Transport.PeerAdvertisement = local
		p.Transport.Profiles = []coordinator.TransportProfileOffer{{Profile: profile, Digest: digest, LocalEndpoints: []string{}, PeerEndpoints: []string{}}}
	}
	f.verify(t, nil)
	if relayAddress != "" {
		f.withRelayAt(t, relayAddress)
	}
	return f
}

func waitLinuxTestFile(ctx context.Context, path string) bool {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func runLinuxTestCommand(t *testing.T, ctx context.Context, name string, args ...string) {
	t.Helper()
	if out, err := exec.CommandContext(ctx, name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func linuxTestEcho(t *testing.T, address string) {
	t.Helper()
	listener := &net.ListenConfig{}
	tcp, err := listener.Listen(t.Context(), "tcp4", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tcp.Close() })
	udp, err := listener.ListenPacket(t.Context(), "udp4", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	go func() {
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			_ = c.SetDeadline(time.Now().Add(time.Second))
			_, _ = io.CopyN(c, c, 4)
			_ = c.Close()
		}
	}()
	go func() {
		var data [4]byte
		for {
			n, peer, err := udp.ReadFrom(data[:])
			if err != nil {
				return
			}
			_, _ = udp.WriteTo(data[:n], peer)
		}
	}()
}

func linuxTestRoundTrip(ctx context.Context, network, address string) bool {
	conn, err := (&net.Dialer{Timeout: 350 * time.Millisecond}).DialContext(ctx, network, address)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	return linuxTestExchange(conn)
}

func linuxTestExchange(conn net.Conn) bool {
	_ = conn.SetDeadline(time.Now().Add(350 * time.Millisecond))
	var reply [4]byte
	if _, err := io.WriteString(conn, "ping"); err != nil {
		return false
	}
	_, err := io.ReadFull(conn, reply[:])
	return err == nil && string(reply[:]) == "ping"
}
