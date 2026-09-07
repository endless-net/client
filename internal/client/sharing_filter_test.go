package client

import (
	"encoding/base64"
	"encoding/binary"
	"net/netip"
	"slices"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
)

func sharingFilterFixture(now time.Time, source bool) clientapi.RegisterNodeResponse {
	left, right := make([]byte, 32), make([]byte, 32)
	right[0] = 1
	g := clientapi.SharePeerGrant{GrantID: "share", RecipientNetworkID: "recipient-net", RecipientNodeID: "recipient", RecipientPublicKey: base64.StdEncoding.EncodeToString(left), RecipientAllowedIPs: []string{"100.64.0.1/32"}, SourceNetworkID: "source-net", SourceNodeID: "source", SourcePublicKey: base64.StdEncoding.EncodeToString(right), SourceAllowedIPs: []string{"100.65.0.1/32"}, Rights: []clientapi.ShareTraffic{{Protocol: "tcp", FirstPort: 443, LastPort: 443}, {Protocol: "udp", FirstPort: 53, LastPort: 53}, {Protocol: "icmp"}}, Initiation: clientapi.ShareInitiationRecipientOnly, Revision: 1, IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute)}
	m := clientapi.RegisterNodeResponse{Network: clientapi.Network{ID: g.RecipientNetworkID, CIDR: "100.64.0.0/24", SharePeerGrants: []clientapi.SharePeerGrant{g}}, Node: clientapi.Node{ID: g.RecipientNodeID, NetworkID: g.RecipientNetworkID, PublicKey: g.RecipientPublicKey, AssignedIP: "100.64.0.1"}, Peers: []clientapi.Peer{{ID: g.SourceNodeID, NetworkID: g.SourceNetworkID, PublicKey: g.SourcePublicKey, AllowedIPs: g.SourceAllowedIPs}}}
	if source {
		m.Network.ID = g.SourceNetworkID
		m.Network.CIDR = "100.65.0.0/24"
		m.Node = clientapi.Node{ID: g.SourceNodeID, NetworkID: g.SourceNetworkID, PublicKey: g.SourcePublicKey, AssignedIP: "100.65.0.1"}
		m.Peers = []clientapi.Peer{{ID: g.RecipientNodeID, NetworkID: g.RecipientNetworkID, PublicKey: g.RecipientPublicKey, AllowedIPs: g.RecipientAllowedIPs}}
	}
	return m
}

func shareTCP(reverse bool, flags byte) []byte {
	packet := applicationTCPPacket("100.64.0.1", "100.65.0.1", 50123, 443)
	if reverse {
		packet = applicationTCPPacket("100.65.0.1", "100.64.0.1", 443, 50123)
	}
	packet[33] = flags
	return packet
}

func TestSharingTCPDirectionStateExpiryAndWithdrawal(t *testing.T) {
	for _, source := range []bool{false, true} {
		now := time.Now()
		m := sharingFilterFixture(now, source)
		f := newSharingPacketFilter()
		f.update(m)
		if f.allows(shareTCP(true, 2), !source, now) {
			t.Fatal("source initiated reverse SYN")
		}
		if f.allows(shareTCP(true, 18), !source, now) {
			t.Fatal("unsolicited SYN ACK accepted")
		}
		if f.allows(shareTCP(false, 16), source, now) {
			t.Fatal("request skipped handshake")
		}
		if !f.allows(shareTCP(false, 2), source, now) {
			t.Fatal("recipient SYN denied")
		}
		if f.allows(shareTCP(true, 2), !source, now) {
			t.Fatal("reverse SYN exploited binding")
		}
		if f.allows(shareTCP(true, 16), !source, now) {
			t.Fatal("reverse data skipped handshake")
		}
		if !f.allows(shareTCP(true, 18), !source, now) || !f.allows(shareTCP(false, 16), source, now) || !f.allows(shareTCP(true, 24), !source, now) {
			t.Fatal("authorized handshake/reply denied")
		}
		wrong := shareTCP(false, 2)
		binary.BigEndian.PutUint16(wrong[22:24], 22)
		if f.allows(wrong, source, now) {
			t.Fatal("wrong destination port allowed")
		}
		if f.allows(shareTCP(true, 16), !source, m.Network.SharePeerGrants[0].ExpiresAt) {
			t.Fatal("established reply outlived grant")
		}
		expiredAt := m.Network.SharePeerGrants[0].ExpiresAt
		m.Network.SharePeerGrants[0].IssuedAt = expiredAt
		m.Network.SharePeerGrants[0].ExpiresAt = expiredAt.Add(time.Minute)
		f.updateAt(m, expiredAt)
		if f.allows(shareTCP(true, 24), !source, expiredAt) {
			t.Fatal("renewed lease resurrected expired established flow")
		}
		if !f.allows(shareTCP(false, 2), source, expiredAt) || !f.allows(shareTCP(true, 18), !source, expiredAt) || !f.allows(shareTCP(false, 16), source, expiredAt) {
			t.Fatal("fresh handshake after lease expiry denied")
		}
		m.Network.SharePeerGrants = nil
		m.Peers = nil
		f.update(m)
		if f.allows(shareTCP(false, 2), source, now) || f.allows(shareTCP(true, 16), !source, now) {
			t.Fatal("withdrawal became unrestricted")
		}
	}
}

func TestSharingUDPAndEchoRequireRecipientRequest(t *testing.T) {
	now := time.Now()
	f := newSharingPacketFilter()
	f.update(sharingFilterFixture(now, true))
	request := applicationTCPPacket("100.64.0.1", "100.65.0.1", 50000, 53)[:28]
	request[9] = 17
	binary.BigEndian.PutUint16(request[2:4], 28)
	binary.BigEndian.PutUint16(request[24:26], 8)
	reply := applicationTCPPacket("100.65.0.1", "100.64.0.1", 53, 50000)[:28]
	reply[9] = 17
	binary.BigEndian.PutUint16(reply[2:4], 28)
	binary.BigEndian.PutUint16(reply[24:26], 8)
	if f.allows(reply, false, now) || !f.allows(request, true, now) || !f.allows(reply, false, now) {
		t.Fatal("UDP direction enforcement failed")
	}
	if f.allows(reply, false, now.Add(30*time.Second)) {
		t.Fatal("idle UDP state survived")
	}
	request[9] = 1
	request[20] = 8
	request[21] = 0
	request[24] = 0
	request[25] = 9
	request[26] = 0
	request[27] = 1
	reply[9] = 1
	reply[20] = 0
	reply[21] = 0
	reply[24] = 0
	reply[25] = 9
	reply[26] = 0
	reply[27] = 1
	if f.allows(reply, false, now) || !f.allows(request, true, now) || !f.allows(reply, false, now) {
		t.Fatal("echo direction enforcement failed")
	}
	reply[27] = 2
	if f.allows(reply, false, now) {
		t.Fatal("unmatched echo sequence allowed")
	}
}

func TestSharingRevisionAndMalformedPacketsFailClosed(t *testing.T) {
	now := time.Now()
	m := sharingFilterFixture(now, false)
	f := newSharingPacketFilter()
	f.update(m)
	if !f.allows(shareTCP(false, 2), false, now) {
		t.Fatal("SYN denied")
	}
	m.Network.SharePeerGrants[0].Revision++
	f.update(m)
	if f.allows(shareTCP(true, 18), true, now) {
		t.Fatal("old revision retained flow")
	}
	for _, mutate := range []func([]byte){func(p []byte) { p[6] = 0x20 }, func(p []byte) { p[32] = 0xf0 }, func(p []byte) { p[2] = 0; p[3] = 20 }} {
		packet := shareTCP(false, 2)
		mutate(packet)
		if f.allows(packet, false, now) {
			t.Fatal("malformed protected packet accepted")
		}
	}
	if _, err := RenderWireGuardWithOptionsChecked("unused", m, WireGuardRenderOptions{}); err == nil {
		t.Fatal("unenforced static sharing export allowed")
	}
	clone := cloneRegisterNodeResponse(m)
	clone.Network.SharePeerGrants[0].Rights[0].LastPort = 65535
	if m.Network.SharePeerGrants[0].Rights[0].LastPort != 443 {
		t.Fatal("grant clone aliases source")
	}
}

func TestSharingTUNEnforcesBothDirections(t *testing.T) {
	now := time.Now()
	m := sharingFilterFixture(now, true)
	sharing := newSharingPacketFilter()
	sharing.update(m)
	applications := newApplicationPacketFilter()
	applications.update(m)
	underlying := &applicationBatchTUN{}
	wrapper := &applicationTUN{Device: underlying, filter: applications, sharing: sharing}
	request := shareTCP(false, 2)
	denied := shareTCP(false, 16)
	if n, err := wrapper.Write([][]byte{denied, request}, 0); err != nil || n != 2 || len(underlying.written) != 1 || !slices.Equal(underlying.written[0], request) {
		t.Fatal("inbound sharing TUN bypass", n, err)
	}
	reply := shareTCP(true, 18)
	underlying.packets = [][]byte{shareTCP(true, 2), reply}
	bufs, sizes := [][]byte{make([]byte, 128), make([]byte, 128)}, make([]int, 2)
	if n, err := wrapper.Read(bufs, sizes, 16); err != nil || n != 1 || !slices.Equal(bufs[0][16:16+sizes[0]], reply) {
		t.Fatal("outbound sharing TUN bypass", n, err)
	}
}

func TestSharingMapInstallSuspendsAccessAndRenewalPreservesFlow(t *testing.T) {
	now := time.Now()
	m := sharingFilterFixture(now, false)
	f := newSharingPacketFilter()
	f.suspend(m)
	if f.allows(shareTCP(false, 2), false, now) {
		t.Fatal("peer installation opened access before grant")
	}
	f.update(m)
	if !f.allows(shareTCP(false, 2), false, now) || !f.allows(shareTCP(true, 18), true, now) || !f.allows(shareTCP(false, 16), false, now) {
		t.Fatal("handshake failed")
	}
	f.suspend(m)
	if f.allows(shareTCP(true, 16), true, now) {
		t.Fatal("map installation left sharing open")
	}
	m.Network.SharePeerGrants[0].ExpiresAt = now.Add(90 * time.Second)
	f.update(m)
	if !f.allows(shareTCP(true, 16), true, now.Add(65*time.Second)) {
		t.Fatal("same-identity renewal lost established flow")
	}
	f.withdraw()
	if f.allows(shareTCP(true, 16), true, now) {
		t.Fatal("failed apply rollback retained sharing")
	}
}

func TestSharingIPv6AndForwardingBoundaries(t *testing.T) {
	now := time.Now()
	m := sharingFilterFixture(now, true)
	g := &m.Network.SharePeerGrants[0]
	g.SourceAllowedIPs = append(g.SourceAllowedIPs, "fd65::1/128")
	g.RecipientAllowedIPs = append(g.RecipientAllowedIPs, "fd64::1/128")
	m.Node.AssignedIPv6 = "fd65::1"
	m.Network.IPv6CIDR = "fd65::/64"
	m.Peers[0].AllowedIPs = append([]string(nil), g.RecipientAllowedIPs...)
	f := newSharingPacketFilter()
	f.update(m)
	packet := make([]byte, 60)
	packet[0], packet[6], packet[52], packet[53] = 0x60, 6, 0x50, 2
	binary.BigEndian.PutUint16(packet[4:6], 20)
	copy(packet[8:24], netip.MustParseAddr("fd64::1").AsSlice())
	copy(packet[24:40], netip.MustParseAddr("fd65::1").AsSlice())
	binary.BigEndian.PutUint16(packet[40:42], 50000)
	binary.BigEndian.PutUint16(packet[42:44], 443)
	if !f.allows(packet, true, now) {
		t.Fatal("authorized IPv6 SYN denied")
	}
	packet[6] = 44
	if f.allows(packet, true, now) {
		t.Fatal("fragment header bypassed sharing")
	}
	transit := applicationTCPPacket("100.64.0.1", "10.0.0.5", 50000, 443)
	transit[33] = 2
	if f.allows(transit, true, now) {
		t.Fatal("grant authorized forwarding beyond shared host")
	}
}
