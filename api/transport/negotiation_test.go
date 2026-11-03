package transport_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/endless-net/client/api/transport"
)

func challenge() transport.Control {
	return transport.Control{Version: 1, Kind: transport.Challenge, AccountID: "account", NetworkID: "network", SourceNodeID: "a", DestinationNodeID: "b", SourcePublicKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), DestinationPublicKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)), SourceGeneration: 4, DestinationGeneration: 9, NetworkRevision: 9007199254740993, ProfileDigest: strings.Repeat("a", 64), CandidateID: strings.Repeat("1", 32), Nonce: bytes.Repeat([]byte{3}, 32)}
}

func response(request transport.Control, kind string) transport.Control {
	request.Kind = kind
	request.SourceNodeID, request.DestinationNodeID = request.DestinationNodeID, request.SourceNodeID
	request.SourcePublicKey, request.DestinationPublicKey = request.DestinationPublicKey, request.SourcePublicKey
	request.SourceGeneration, request.DestinationGeneration = request.DestinationGeneration, request.SourceGeneration
	request.Nonce = bytes.Clone(request.Nonce)
	return request
}

func TestReflectedReplyBinding(t *testing.T) {
	for kind, replyKind := range map[string]string{transport.Challenge: transport.Echo, transport.Prepare: transport.Prepared, transport.Commit: transport.Committed} {
		t.Run(kind, func(t *testing.T) {
			request := challenge()
			request.Kind = kind
			reply := response(request, replyKind)
			if !transport.MatchesReply(request, reply) {
				t.Fatal("exact reflected reply rejected")
			}
			for name, change := range map[string]func(*transport.Control){
				"scope":   func(c *transport.Control) { c.AccountID += "foreign" },
				"network": func(c *transport.Control) { c.NetworkID += "foreign" },
				"node":    func(c *transport.Control) { c.SourceNodeID += "other" },
				"identity": func(c *transport.Control) {
					c.SourcePublicKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32))
				},
				"generation": func(c *transport.Control) { c.DestinationGeneration++ },
				"revision":   func(c *transport.Control) { c.NetworkRevision++ },
				"profile":    func(c *transport.Control) { c.ProfileDigest = strings.Repeat("b", 64) },
				"candidate":  func(c *transport.Control) { c.CandidateID = strings.Repeat("2", 32) },
				"nonce":      func(c *transport.Control) { c.Nonce[0]++ },
				"kind":       func(c *transport.Control) { c.Kind = transport.Challenge },
			} {
				t.Run(name, func(t *testing.T) {
					changed := response(request, replyKind)
					change(&changed)
					if transport.MatchesReply(request, changed) {
						t.Fatal("reply crossed original binding")
					}
				})
			}
			raw, err := transport.MarshalControl(reply)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := transport.DecodeControl(raw)
			if err != nil || !transport.MatchesReply(request, decoded) || decoded.NetworkRevision != request.NetworkRevision {
				t.Fatal("reply wire changed binding or uint64", err)
			}
		})
	}
}

func TestUnsupportedAndMalformedControls(t *testing.T) {
	for name, change := range map[string]func(*transport.Control){
		"version":            func(c *transport.Control) { c.Version = 2 },
		"unknown kind":       func(c *transport.Control) { c.Kind = "execute" },
		"zero generation":    func(c *transport.Control) { c.SourceGeneration = 0 },
		"zero revision":      func(c *transport.Control) { c.NetworkRevision = 0 },
		"same node":          func(c *transport.Control) { c.SourceNodeID = c.DestinationNodeID },
		"same key":           func(c *transport.Control) { c.SourcePublicKey = c.DestinationPublicKey },
		"bad public key":     func(c *transport.Control) { c.SourcePublicKey = "untrusted" },
		"uppercase digest":   func(c *transport.Control) { c.ProfileDigest = strings.ToUpper(c.ProfileDigest) },
		"empty candidate":    func(c *transport.Control) { c.CandidateID = strings.Repeat("0", 32) },
		"bad candidate":      func(c *transport.Control) { c.CandidateID = strings.Repeat("1", 31) },
		"zero nonce":         func(c *transport.Control) { clear(c.Nonce) },
		"short nonce":        func(c *transport.Control) { c.Nonce = c.Nonce[:31] },
		"wrong proposer":     func(c *transport.Control) { c.Kind, c.SourceNodeID, c.DestinationNodeID = transport.Prepare, "b", "a" },
		"wrong acknowledger": func(c *transport.Control) { c.Kind = transport.Prepared },
	} {
		t.Run(name, func(t *testing.T) {
			c := challenge()
			change(&c)
			if _, err := transport.MarshalControl(c); err == nil {
				t.Fatal("invalid control accepted")
			}
		})
	}
	raw, err := transport.MarshalControl(challenge())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{string(raw) + `{}`, strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1), strings.Replace(string(raw), `"kind":"challenge"`, `"Kind":"challenge"`, 1), strings.Replace(string(raw), `"nonce":`, `"unknown":`, 1), strings.Repeat(" ", transport.MaxDatagramBytes+1)} {
		if _, err := transport.DecodeControl([]byte(input)); err == nil {
			t.Fatal("malformed wire control accepted")
		}
	}
}
