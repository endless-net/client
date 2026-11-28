package clientapp

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/endless-net/client/internal/control"
	contract "github.com/endless-net/coordinator/api/enrollment"
)

func TestClientSignsOnlyChallengedRegistration(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	leaf := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now, NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "identity.pem")
	if err = os.WriteFile(file, append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})...), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDLESSNET_CLIENT_POSTURE_IDENTITY_TEST", file)
	req := contract.UpdateNodeEndpointRequest{Generation: 8, Status: "online", TTL: "1m", Endpoint: "192.0.2.12:80", Posture: &contract.NodePostureMetadata{Platform: "linux", ObservedAt: now}}
	receipt := contract.UpdateNodeEndpointResponse{NetworkID: "network", NodeID: "node", Online: true, Generation: 7}
	attachCertificateProof(&req, &receipt, "test")
	if req.Posture.Certificate != nil {
		t.Fatal("signed without challenge")
	}
	receipt.CertificateChallenge = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	attachCertificateProof(&req, &receipt, "missing")
	if req.Posture.Certificate != nil {
		t.Fatal("used another registration's identity")
	}
	attachCertificateProof(&req, &receipt, "test")
	wire := control.EndpointRequest("node", req).GetPosture().GetCertificate()
	if wire == nil || len(wire.ChainDer) != 1 || wire.Challenge != receipt.CertificateChallenge {
		t.Fatal("missing certificate on wire")
	}
	message, err := contract.CertificatePosturePayload("network", "node", req)
	cert, parseErr := x509.ParseCertificate(wire.ChainDer[0])
	if err != nil || parseErr != nil || !contract.VerifyCertificatePostureSignature(cert, message, wire.Signature) {
		t.Fatal("wire signature invalid", err, parseErr)
	}
}
