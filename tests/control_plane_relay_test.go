package tests

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
	"github.com/endless-net/client/internal/testclient"
	"github.com/endless-net/client/internal/testcontrol"
	"github.com/endless-net/client/internal/testrelay"
	"github.com/endless-net/client/internal/testwireguard"
	relay "github.com/endless-net/relay/protocol/v1"
)

// HC-028/HC-030: no direct endpoint is published. All real native application
// traffic must cross the public TLS Relay contract before reaching WireGuard.
func TestControlPlaneNativeRelayTraffic(t *testing.T) {
	runNativeRelayTraffic(t, false)
}

func TestControlPlaneNativeRelayFailover(t *testing.T) {
	runNativeRelayTraffic(t, true)
}

func runNativeRelayTraffic(t *testing.T, failover bool) {
	t.Helper()
	requireControlScenario(t)
	binary := requiredPath(t, "ENDLESSNET_PACKET_PROBE")
	for _, ipv6 := range []bool{false, true} {
		name := "ipv4"
		if ipv6 {
			name = "ipv6"
		}
		t.Run(name, func(t *testing.T) {
			s := testcontrol.NewTLS(t)
			network, token, err := s.AddNetwork("relay-traffic", "198.18.94.0/24")
			if err != nil {
				t.Fatal(err)
			}
			n := testclient.New(t, s)
			n.TrustControlTLS(s)
			n.Enroll(s, network.Name, token, "--route-table", "auto")
			n.Start()
			defer n.Stop()
			initial := n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId != "" && v.ActiveProfileId != "" && v.GetStoredState().GetCachedMapValid() && nativeOverlayAddress(v, false).IsValid()
			})
			peerIP := netip.MustParseAddr("198.18.94.20")
			clientIP := nativeOverlayAddress(initial, false)
			if ipv6 {
				if err := s.UpdateMap(initial.NodeId, func(m *api.NetworkMapSnapshot) { m.Network.IPv6CIDR = "fd94::/64"; m.Node.AssignedIPv6 = "fd94::1" }); err != nil {
					t.Fatal(err)
				}
				n.AwaitNativeStatus(func(v *ipc.Status) bool {
					return v.NodeId == initial.NodeId && v.ActiveProfileId == initial.ActiveProfileId && nativeOverlayAddress(v, true).String() == "fd94::1" && v.GetStoredState().GetCachedMapValid()
				})
				clientIP, peerIP = netip.MustParseAddr("fd94::1"), netip.MustParseAddr("fd94::20")
			}
			underlay := nativePeerUnderlay(t, nativeOverlayAddress(initial, false), peerIP)
			m, err := s.Snapshot(initial.NodeId)
			if err != nil {
				t.Fatal(err)
			}
			reference := testwireguard.NewTCP(t, m.Node.PublicKey, clientIP, peerIP, underlay)
			transport := testrelay.New(t, network.ID, initial.NodeId, "relay-peer", reference.Endpoint, reference.ConfigureClientEndpoint)
			primary := transport
			var backup *testrelay.Server
			endpoints := []relay.Endpoint{primary.Endpoint}
			certificates := append([]byte(nil), primary.CertificatePEM...)
			if failover {
				backup = testrelay.NewWithCredential(t, primary.Credential, "relay-peer", reference.Endpoint, reference.ConfigureClientEndpoint)
				backup.Endpoint.ID = "reference-relay-backup"
				backup.Endpoint.Priority = 10
				endpoints = append(endpoints, backup.Endpoint)
				certificates = append(certificates, backup.CertificatePEM...)
			}
			caFile := filepath.Join(t.TempDir(), "relay-ca.pem")
			if err := os.WriteFile(caFile, certificates, 0o600); err != nil {
				t.Fatal("could not write public Relay test CA")
			}
			n.Stop()
			n.AgentArgs = append(n.AgentArgs, "--relay-ca-cert", caFile)
			if err := s.UpdateMap(initial.NodeId, func(m *api.NetworkMapSnapshot) {
				m.Peers = []api.Peer{{ID: "relay-peer", NetworkID: network.ID, Hostname: "relay-peer", PublicKey: reference.PublicKey, AllowedIPs: []string{netip.PrefixFrom(peerIP, peerIP.BitLen()).String()}}}
				m.Relays = endpoints
				m.RelayCredential = &transport.Credential
			}); err != nil {
				t.Fatal(err)
			}
			expectedMap, err := s.Snapshot(initial.NodeId)
			if err != nil {
				t.Fatal(err)
			}
			n.Start()
			selected := func() {
				t.Helper()
				var last *ipc.Diagnostics
				defer func() {
					if !t.Failed() {
						return
					}
					authenticated, sent, received := transport.Counts()
					var relayOK, selectedRelay, selectedPath bool
					if agent := last.GetStatus().GetAgent(); agent != nil {
						relayOK = agent.RelayOk
						selectedRelay = agent.GetSelectedRelay().GetId() == transport.Endpoint.ID
						for _, peer := range last.GetPeers() {
							selectedPath = selectedPath || peer.Id == "relay-peer" && peer.SelectedPath == ipc.PathKind_PATH_KIND_RELAY
						}
					}
					t.Logf("Relay status wait: agent_present=%t relay_ok=%t relay_selected=%t peer_relay_path=%t auth=%d frames_to_peer=%d frames_from_peer=%d", last.GetStatus().GetAgent() != nil, relayOK, selectedRelay, selectedPath, authenticated, sent, received)
				}()
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				if err := testclient.Await(ctx, func() bool {
					response := &ipc.GetDiagnosticsResponse{}
					if n.NativeService("diagnostics", response, "--profile-id", initial.ActiveProfileId, "--timeout", "1s") != nil {
						return false
					}
					last = response.Diagnostics
					v := last.GetStatus()
					if !nativePeerMapApplied(v, initial, expectedMap.Revision.Network, 1) || !v.Agent.RelayOk || v.Agent.GetSelectedRelay().GetId() != transport.Endpoint.ID || !last.GetTunnel().GetOk() || last.GetTunnel().GetFailure() != nil {
						return false
					}
					for _, p := range last.GetPeers() {
						if p.Id == "relay-peer" && p.SelectedPath == ipc.PathKind_PATH_KIND_RELAY {
							return true
						}
					}
					return false
				}); err != nil {
					t.Fatal("native diagnostics did not confirm the selected Relay path")
				}
			}
			address := net.JoinHostPort(peerIP.String(), "24001")
			probe := func(protocol string) bool { return applicationProbe(t, binary, "", protocol, address) }
			reachable := func() {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				var tcpOK, udpOK bool
				if err := testclient.Await(ctx, func() bool {
					tcpOK, udpOK = probe("tcp"), probe("udp")
					return tcpOK && udpOK
				}); err != nil {
					authenticated, sent, received := transport.Counts()
					initiations, responses, other := reference.HandshakeCounts()
					requests, echoes := reference.PacketCounts()
					v := &ipc.GetDiagnosticsResponse{}
					statusErr := n.NativeService("diagnostics", v, "--profile-id", initial.ActiveProfileId, "--timeout", "1s")
					var handshake, loopback bool
					var rx, tx uint64
					if statusErr == nil && v.GetDiagnostics().GetTunnel() != nil {
						for _, peer := range v.Diagnostics.Tunnel.Peers {
							handshake = handshake || peer.LatestHandshake != nil && peer.LatestHandshake.Seconds > 0
							rx += peer.ReceivedBytes
							tx += peer.TransmittedBytes
							if endpoint, err := netip.ParseAddrPort(peer.Endpoint); err == nil {
								loopback = loopback || endpoint.Addr().IsLoopback()
							}
						}
					}
					t.Fatalf("native Relay exchange unavailable: tcp=%t udp=%t auth=%d frames_to_peer=%d frames_from_peer=%d reference_init=%d reference_response=%d reference_other=%d application_requests=%d echoes=%d status_available=%t client_handshake=%t client_loopback_endpoint=%t rx=%d tx=%d", tcpOK, udpOK, authenticated, sent, received, initiations, responses, other, requests, echoes, statusErr == nil, handshake, loopback, rx, tx)
				}
				authenticated, sent, received := transport.Counts()
				if authenticated == 0 || sent == 0 || received == 0 {
					t.Fatal("application traffic bypassed Relay contract participant")
				}
				t.Log("native Relay TCP and UDP exchange succeeded; verifying public path status")
				selected()
			}
			reachable()
			// Establish both sockets through the primary Relay. Keep these exact
			// probe processes and connections through failover and outage recovery.
			tcp := startApplicationSession(t, binary, "", "tcp", address, 15*time.Second)
			udp := startApplicationSession(t, binary, "", "udp", address)
			tcp("ok")
			udp("ok")
			if backup != nil {
				primary.SetUnavailable(true)
				transport = backup
				reachable()
				tcp("ok")
				udp("ok")
			}
			transport.SetUnavailable(true)
			tcp("blocked")
			udp("blocked")
			if probe("tcp") || probe("udp") {
				t.Fatal("application remained reachable without its only Relay path")
			}
			n.AwaitNativeStatus(func(v *ipc.Status) bool {
				return v.NodeId == initial.NodeId && v.ActiveProfileId == initial.ActiveProfileId && v.GetStoredState().GetNodeCredentialPresent() && v.GetStoredState().GetCachedMapPresent()
			})
			primary.SetUnavailable(false)
			transport = primary
			reachable()
			tcp("recover")
			udp("ok")
			n.Stop()
			n.Start()
			reachable()
			registrations := 0
			for _, event := range s.Events() {
				if event.Kind == "registered" {
					registrations++
				}
			}
			if registrations != 1 {
				t.Fatal("Relay recovery unexpectedly registered a new Client identity")
			}
		})
	}
}
