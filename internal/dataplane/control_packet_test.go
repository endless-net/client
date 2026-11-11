package dataplane

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"testing"

	"github.com/endless-net/client/api/transport"
)

func challengeControl(t *testing.T, f *candidateFixture) transport.Control {
	t.Helper()
	a := f.assignment(t)
	return transport.Control{Version: 1, Kind: transport.Challenge, AccountID: a.plan.AccountID, NetworkID: a.plan.NetworkID, SourceNodeID: a.plan.LocalNodeID, DestinationNodeID: a.plan.PeerNodeID, SourcePublicKey: a.plan.LocalPublicKey, DestinationPublicKey: a.plan.PeerPublicKey, SourceGeneration: a.plan.LocalIntentGeneration, DestinationGeneration: a.plan.PeerAdvertisement.IntentGeneration, NetworkRevision: a.plan.Revision.Network, ProfileDigest: a.offer.Digest, CandidateID: "112233445566778899aabbccddeeff00", Nonce: bytes.Repeat([]byte{9}, 32)}
}

func TestControlPacketStrictPublishedCodecAndReservedBoundary(t *testing.T) {
	f := newCandidateFixture(t)
	value := challengeControl(t, f)
	source, destination, err := f.assignment(t).addresses()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := controlPacket(source, destination, value)
	if err != nil {
		t.Fatal(err)
	}
	result, claimed, err := decodeControlPacket(raw, source, destination)
	if err != nil || !claimed || !transport.MatchesReply(value, replyControl(result, transport.Echo)) {
		t.Fatal("published DTO binding lost", err)
	}
	for _, change := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"fragment", func(p []byte) { binary.BigEndian.PutUint16(p[6:8], 0x2000) }},
		{"reserved-ip-flag", func(p []byte) { binary.BigEndian.PutUint16(p[6:8], 0x8000) }},
		{"header-length", func(p []byte) { p[0] = 0x44 }},
		{"ip-length", func(p []byte) { p[3]-- }},
		{"ip-checksum", func(p []byte) { p[10] ^= 1 }},
		{"udp-length", func(p []byte) { p[25]-- }},
		{"udp-checksum", func(p []byte) { p[27] ^= 1 }},
		{"udp-zero-checksum", func(p []byte) { p[26] = 0; p[27] = 0 }},
		{"source-port", func(p []byte) { p[21] = 1 }},
		{"payload", func(p []byte) { p[len(p)-1] ^= 1 }},
	} {
		t.Run(change.name, func(t *testing.T) {
			packet := bytes.Clone(raw)
			change.mutate(packet)
			if _, claimed, err := decodeControlPacket(packet, source, destination); !claimed || err == nil {
				t.Fatal("malformed control escaped reserved boundary", claimed, err)
			}
		})
	}
	if _, claimed, err := decodeControlPacket(raw, netip.MustParseAddr("100.70.0.3"), destination); !claimed || err == nil {
		t.Fatal("foreign decrypted source accepted")
	}
	application := cipherIPv4(source.String(), destination.String(), 1)
	if _, claimed, err := decodeControlPacket(application, source, destination); claimed || err != nil {
		t.Fatal("ordinary application traffic consumed", err)
	}
}
