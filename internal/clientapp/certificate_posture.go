package clientapp

import (
	"crypto"
	"crypto/tls"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	contract "github.com/endless-net/coordinator/api/enrollment"
)

// The client operator chooses an identity for one registration. Only the
// public chain and possession signature leave this process.
func attachCertificateProof(request *contract.UpdateNodeEndpointRequest, receipt *contract.UpdateNodeEndpointResponse, attempt string) {
	if receipt == nil || !receipt.Online || receipt.Generation == 0 || receipt.Generation >= request.Generation || !contract.ValidCertificateChallenge(receipt.CertificateChallenge) || request.Posture == nil || attempt == "" {
		return
	}
	path := os.Getenv("ENDLESSNET_CLIENT_POSTURE_IDENTITY_" + strings.ToUpper(strings.ReplaceAll(attempt, "-", "")))
	if !filepath.IsAbs(path) {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 || info.Size() <= 0 || info.Size() > 256<<10 {
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, 256<<10+1))
	defer clear(data)
	if err != nil || len(data) > 256<<10 {
		return
	}
	pair, err := tls.X509KeyPair(data, data)
	if err != nil {
		return
	}
	key, ok := pair.PrivateKey.(crypto.Signer)
	if !ok {
		return
	}
	proof := &contract.CertificatePossession{Challenge: receipt.CertificateChallenge, ChainDER: pair.Certificate}
	request.Posture.Certificate = proof
	payload, err := contract.CertificatePosturePayload(receipt.NetworkID, receipt.NodeID, *request)
	if err == nil {
		proof.Signature, err = contract.SignCertificatePosture(key, payload)
	}
	if err != nil {
		request.Posture.Certificate = nil
	}
}
