package connectivity

import (
	"crypto/ed25519"
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"time"

	coordinator "github.com/endless-net/coordinator/api/enrollment"
	relay "github.com/endless-net/relay/api/wire"
	signing "github.com/endless-net/signing/api/contract"
)

func relayMap(value coordinator.NetworkMapSnapshot, trust signing.ServerKeyResponse, now time.Time) error {
	if len(value.Relays) > 8 {
		return ErrMap
	}
	if len(value.Relays) == 0 {
		if value.RelayCredential != nil {
			return ErrMap
		}
		return nil
	}
	if value.Node.Status == "revoked" || value.RelayCredential == nil {
		return ErrMap
	}
	seen := map[string]bool{}
	for _, edge := range value.Relays {
		host, port, err := net.SplitHostPort(edge.Addr)
		n, portErr := strconv.ParseUint(port, 10, 16)
		if !identifier(edge.ID) || seen[edge.ID] || edge.Protocol != relay.EndpointProtocolTLS || edge.Priority < 0 || err != nil || portErr != nil || n == 0 || strconv.FormatUint(n, 10) != port || host == "" || host != strings.ToLower(host) || strings.ContainsAny(host, " /?#%@\\\x00\r\n\t") || net.JoinHostPort(host, port) != edge.Addr {
			return ErrMap
		}
		seen[edge.ID] = true
	}
	credential := value.RelayCredential
	if credential.Algorithm != relay.CredentialAlgorithm || credential.NetworkID != value.Network.ID || credential.NodeID != value.Node.ID || !now.Before(credential.ExpiresAt) || credential.ExpiresAt.After(value.MapSignature.ExpiresAt) {
		return ErrMap
	}
	key, err := signing.ResolveBootstrapKey(trust.RelayTrustBundle, credential.KeyID, now)
	if err != nil || key.NotBefore != nil && value.MapSignature.IssuedAt.Before(*key.NotBefore) || key.NotAfter != nil && credential.ExpiresAt.After(*key.NotAfter) {
		return ErrTrust
	}
	public, err := base64.RawURLEncoding.Strict().DecodeString(key.PublicKey)
	if err != nil {
		return ErrTrust
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(credential.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || base64.RawURLEncoding.EncodeToString(signature) != credential.Signature {
		return ErrMap
	}
	payload, err := relay.CredentialSigningPayload(credential.KeyID, credential.NetworkID, credential.NodeID, credential.ExpiresAt)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(public), payload, signature) {
		return ErrMap
	}
	return nil
}
