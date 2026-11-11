package connectivity

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	relay "github.com/endless-net/relay/api/wire"
	signing "github.com/endless-net/signing/api/contract"
)

func TestMapRelayCredentialRequiresCurrentPurposeSubjectAndOriginalInterval(t *testing.T) {
	for _, name := range []string{"valid", "missing", "bad-signature", "wrong-purpose", "foreign-network", "foreign-node", "longer-than-map", "expired", "foreign-protocol", "duplicate-edge", "edge-without-credential", "credential-without-edge", "nine-edges", "ambiguous-port"} {
		t.Run(name, func(t *testing.T) {
			f := newMapFixture(t)
			expires := f.now.Add(30 * time.Second)
			var trust signing.ServerKeyResponse
			if json.Unmarshal(f.trust.Bundle, &trust) != nil {
				t.Fatal("invalid fixture trust")
			}
			bundle := trust.RelayTrustBundle
			payload, err := relay.CredentialSigningPayload(bundle.ActiveKeyID, f.value.Network.ID, f.value.Node.ID, expires)
			if err != nil {
				t.Fatal(err)
			}
			f.value.Relays = []relay.Endpoint{{ID: "edge", Addr: "relay.example:443", Protocol: relay.EndpointProtocolTLS, Priority: 0}}
			f.value.RelayCredential = &relay.Credential{Algorithm: relay.CredentialAlgorithm, KeyID: bundle.ActiveKeyID, NetworkID: f.value.Network.ID, NodeID: f.value.Node.ID, ExpiresAt: expires,
				Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{63}, 32)), payload))}
			switch name {
			case "missing", "edge-without-credential":
				f.value.RelayCredential = nil
			case "bad-signature":
				f.value.RelayCredential.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, 64))
			case "wrong-purpose":
				f.value.RelayCredential.KeyID = trust.TrustBundle.ActiveKeyID
			case "foreign-network":
				f.value.RelayCredential.NetworkID = "foreign"
			case "foreign-node":
				f.value.RelayCredential.NodeID = "foreign"
			case "longer-than-map":
				f.value.RelayCredential.ExpiresAt = f.now.Add(2 * time.Minute)
			case "expired":
				f.value.RelayCredential.ExpiresAt = f.now
			case "foreign-protocol":
				f.value.Relays[0].Protocol = "plaintext"
			case "duplicate-edge":
				f.value.Relays = append(f.value.Relays, f.value.Relays[0])
			case "credential-without-edge":
				f.value.Relays = nil
			case "nine-edges":
				for i := 0; i < 8; i++ {
					edge := f.value.Relays[0]
					edge.ID = strings.Repeat("x", i+1)
					f.value.Relays = append(f.value.Relays, edge)
				}
			case "ambiguous-port":
				f.value.Relays[0].Addr = "relay.example:0443"
			}
			verifier, err := NewVerifier(f.profile, f.config)
			if err != nil {
				t.Fatal(err)
			}
			raw := f.sign(t, nil, f.now.Add(-time.Second), f.now.Add(time.Minute))
			value, err := verifier.Verify(t.Context(), raw, nil)
			if name == "valid" {
				if err != nil {
					t.Fatal("valid Relay map rejected", err)
				}
				snapshot, err := value.Snapshot()
				if err != nil || snapshot.RelayCredential == nil || len(snapshot.Relays) != 1 {
					t.Fatal("Relay projection lost")
				}
				return
			}
			if err == nil {
				t.Fatal("invalid Relay input accepted despite a valid map signature", name)
			}
		})
	}
}
