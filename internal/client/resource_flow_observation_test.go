package client

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

type resourceFailTUN struct{ applicationBatchTUN }

func (d *resourceFailTUN) Write([][]byte, int) (int, error) {
	return 0, errors.New("TUN delivery failed")
}

func resourceTestTCP(source, destination string, sourcePort, destinationPort uint16, flags byte) []byte {
	raw := applicationTCPPacket(source, destination, sourcePort, destinationPort)
	raw[33] = flags
	return raw
}

func resourceTestUDP(source, destination string, sourcePort, destinationPort uint16, payload []byte) []byte {
	raw := make([]byte, 28+len(payload))
	raw[0], raw[9] = 0x45, 17
	binary.BigEndian.PutUint16(raw[2:4], uint16(len(raw)))
	copy(raw[12:16], netip.MustParseAddr(source).AsSlice())
	copy(raw[16:20], netip.MustParseAddr(destination).AsSlice())
	binary.BigEndian.PutUint16(raw[20:22], sourcePort)
	binary.BigEndian.PutUint16(raw[22:24], destinationPort)
	binary.BigEndian.PutUint16(raw[24:26], uint16(8+len(payload)))
	copy(raw[28:], payload)
	return raw
}

func TestResourceFlowRequiresAllowedCorrelatedReplyAndExpiry(t *testing.T) {
	for _, protocol := range []string{"tcp", "udp"} {
		t.Run(protocol, func(t *testing.T) {
			now := time.Now()
			observer := &resourceFlowObserver{}
			var request, response []byte
			if protocol == "tcp" {
				request = resourceTestTCP("100.64.0.1", "100.64.0.2", 40000, 5432, 0x02)
				response = resourceTestTCP("100.64.0.2", "100.64.0.1", 5432, 40000, 0x12)
			} else {
				request = resourceTestUDP("100.64.0.1", "100.64.0.2", 40000, 5432, []byte("q"))
				response = resourceTestUDP("100.64.0.2", "100.64.0.1", 5432, 40000, []byte("a"))
			}
			observer.observe(response, true, now)
			if len(observer.samples(now)) != 0 {
				t.Fatal("unsolicited reply became positive evidence")
			}
			observer.observe(request, false, now)
			wrongPort := slices.Clone(response)
			binary.BigEndian.PutUint16(wrongPort[20:22], 9999)
			observer.observe(wrongPort, true, now)
			if len(observer.samples(now)) != 0 {
				t.Fatal("foreign port confirmed a flow")
			}
			invalid := slices.Clone(response)
			if protocol == "tcp" {
				invalid[33] = 0x14 // RST-ACK
			} else {
				invalid = invalid[:28] // no response payload
				binary.BigEndian.PutUint16(invalid[2:4], uint16(len(invalid)))
				binary.BigEndian.PutUint16(invalid[24:26], 8)
			}
			observer.observe(invalid, true, now)
			if len(observer.samples(now)) != 0 {
				t.Fatal("failed application response became positive evidence")
			}
			observer.observe(response, true, now)
			samples := observer.samples(now)
			if len(samples) != 1 || !observer.current(samples[0], now) || observer.current(samples[0], now.Add(resourceFlowLifetime)) {
				t.Fatal("correlated reply lifetime was not exact", samples)
			}
			observer.reset()
			if observer.current(samples[0], now) || len(observer.samples(now)) != 0 {
				t.Fatal("reconfiguration retained old positive evidence")
			}
		})
	}
}

func TestResourceFlowMatchesExactSignedServiceAndSubnet(t *testing.T) {
	opts, _ := signedServiceDNSFixture(t)
	source := &opts.NetworkMap
	peer := source.Peers[0]
	source.Peers[0].AllowedIPs = append(source.Peers[0].AllowedIPs, "10.2.0.0/24")
	source.Network.ClientPolicy = &api.ClientPolicy{Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceSubnet, ID: peer.ID, CIDR: "100.64.0.2/32"}}}
	serviceID := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, "db\x00tcp\x005432")
	hostSubnetID := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SUBNET, peer.ID+"\x00100.64.0.2/32")
	subnetID := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SUBNET, peer.ID+"\x0010.2.0.0/24")
	sample := resourceFlowSample{key: resourceFlowKey{local: netip.MustParseAddr("100.64.0.1"), remote: netip.MustParseAddr("100.64.0.2"), localPort: 40000, remotePort: 5432, protocol: 6}}
	if got := resourceFlowIDs(source, source.Peers[0], sample, peerDNSRecords(*source)); !slices.Contains(got, serviceID) || !slices.Contains(got, hostSubnetID) {
		t.Fatal("exact service or single-IP subnet proof missing", got)
	}
	sample.key.remotePort = 5433
	if got := resourceFlowIDs(source, source.Peers[0], sample, peerDNSRecords(*source)); slices.Contains(got, serviceID) {
		t.Fatal("wrong port inherited service proof", got)
	}
	sample.key.remote, sample.key.remotePort = netip.MustParseAddr("10.2.0.42"), 5432
	if got := resourceFlowIDs(source, source.Peers[0], sample, peerDNSRecords(*source)); !slices.Contains(got, subnetID) || slices.Contains(got, serviceID) {
		t.Fatal("subnet destination inherited service proof", got)
	}
	if got := resourceFlowPeerIndex(source, sample.key.remote); got != 0 {
		t.Fatal("signed subnet owner lost", got)
	}
	service := source.Network.Services[0]
	first := resourceFlowSample{key: resourceFlowKey{remote: netip.MustParseAddr("100.64.0.2")}}
	if !resourceServiceTargetsConfirmed(service, peerDNSRecords(*source), []resourceFlowSample{first}) {
		t.Fatal("exact service target did not confirm")
	}
	source.Peers[0].AllowedIPs = append(source.Peers[0].AllowedIPs, "fd00::2/128")
	if resourceServiceTargetsConfirmed(service, peerDNSRecords(*source), []resourceFlowSample{first}) {
		t.Fatal("IPv4 proof concealed missing IPv6 service path")
	}
	second := resourceFlowSample{key: resourceFlowKey{remote: netip.MustParseAddr("fd00::2")}}
	if !resourceServiceTargetsConfirmed(service, peerDNSRecords(*source), []resourceFlowSample{first, second}) {
		t.Fatal("both signed service families were not accepted")
	}
	source.Peers = append(source.Peers, api.Peer{ID: "other", AllowedIPs: []string{"10.2.0.42/32"}})
	if got := resourceFlowPeerIndex(source, sample.key.remote); got != 1 {
		t.Fatal("more specific peer did not own flow", got)
	}
	source.Peers = append(source.Peers, api.Peer{ID: "collision", AllowedIPs: []string{"10.2.0.42/32"}})
	if got := resourceFlowPeerIndex(source, sample.key.remote); got >= 0 {
		t.Fatal("ambiguous peer owned flow", got)
	}
}

func TestResourceFlowIsCollectedOnlyAcrossAllowedTUNDirections(t *testing.T) {
	opts, _ := signedServiceDNSFixture(t)
	filter := newApplicationPacketFilter()
	filter.update(opts.NetworkMap)
	observer := &resourceFlowObserver{}
	request := resourceTestTCP("100.64.0.1", "100.64.0.2", 40000, 5432, 0x02)
	response := resourceTestTCP("100.64.0.2", "100.64.0.1", 5432, 40000, 0x12)
	underlying := &applicationBatchTUN{packets: [][]byte{request}}
	wrapper := &applicationTUN{Device: underlying, filter: filter, resourceFlows: observer}
	bufs, sizes := [][]byte{make([]byte, 128)}, make([]int, 1)
	if n, err := wrapper.Read(bufs, sizes, 16); err != nil || n != 1 {
		t.Fatal("outbound packet did not pass all filters", n, err)
	}
	if len(observer.samples(time.Now())) != 0 {
		t.Fatal("outbound request alone claimed reachability")
	}
	wrapper.Device = &resourceFailTUN{}
	if _, err := wrapper.Write([][]byte{response}, 0); err == nil || len(observer.samples(time.Now())) != 0 {
		t.Fatal("failed OS delivery claimed reachability", err)
	}
	wrapper.Device = underlying
	if n, err := wrapper.Write([][]byte{response}, 0); err != nil || n != 1 || len(underlying.written) != 1 {
		t.Fatal("reply was not delivered to OS", n, err)
	}
	if len(observer.samples(time.Now())) != 1 {
		t.Fatal("delivered reply did not confirm application flow")
	}
	observer.reset()
	denied := &resourcePacketFilter{}
	if err := denied.suspend([]resourceDenyRule{{prefix: netip.MustParsePrefix("100.64.0.2/32"), protocol: 6, port: 5432}}, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	denied.commit()
	wrapper.resources = denied
	observer.observe(request, false, time.Now())
	if n, err := wrapper.Write([][]byte{response}, 0); err != nil || n != 1 {
		t.Fatal("denied reply handling failed", n, err)
	}
	if len(underlying.written) != 1 || len(observer.samples(time.Now())) != 0 {
		t.Fatal("denied reply seeded positive evidence")
	}
}
