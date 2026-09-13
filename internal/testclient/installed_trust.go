package testclient

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/endless-net/client/internal/testcontrol"
)

// TrustInstalledControlTLS extends the existing disposable-runner trust fixture
// to services that do not inherit the test CLI's SSL_CERT_FILE environment.
// It must never be invoked on a developer machine or a persistent runner.
func (n *Node) TrustInstalledControlTLS(s *testcontrol.Server) {
	n.t.Helper()
	if err := installedTrustRunner(os.Getenv("GITHUB_ACTIONS"), os.Getenv("RUNNER_ENVIRONMENT")); err != nil {
		n.t.Fatal(err)
	}
	if runtime.GOOS != "linux" {
		n.TrustControlTLS(s)
		return
	}
	public := s.TLSCertificatePEM()
	block, rest := pem.Decode(public)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		n.t.Fatal("installed control fixture requires one public TLS certificate")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		n.t.Fatal("invalid installed control public test certificate")
	}
	// Exclusive creation ensures cleanup never removes a pre-existing root.
	path := filepath.Join("/usr/local/share/ca-certificates", "endlessnet-installed-test-"+rand.Text()+".crt")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		n.t.Fatal("could not create disposable installed-service trust fixture")
	}
	n.t.Cleanup(func() {
		if err := os.Remove(path); err != nil {
			n.t.Error("could not remove the exact installed-service test CA")
			return
		}
		if err := runTrustCommand([]string{"update-ca-certificates"}); err != nil {
			n.t.Errorf("could not refresh trust after test CA removal: %v", err)
		}
	})
	_, writeErr := file.Write(public)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		n.t.Fatal("could not write installed-service public test CA")
	}
	if err := runTrustCommand([]string{"update-ca-certificates"}); err != nil {
		n.t.Fatalf("could not activate installed-service test CA: %v", err)
	}
}

func installedTrustRunner(actions, environment string) error {
	if actions != "true" || environment != "github-hosted" {
		return errors.New("installed-service trust fixture requires a disposable GitHub-hosted runner")
	}
	return nil
}
