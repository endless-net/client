package testclient

import (
	"context"
	"crypto/sha1" // Certificate-store lookup identifier, not a signature algorithm.
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/endless-net/client/internal/testcontrol"
)

// TrustControlTLS installs only the fixture's ephemeral public CA on disposable
// native CI runners. Windows/macOS use OS trust stores; Linux uses the driver's
// process-scoped SSL_CERT_FILE. Cleanup removes this exact certificate.
func (n *Node) TrustControlTLS(s *testcontrol.Server) {
	n.t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		n.t.Fatal("native trust-store setup is restricted to disposable GitHub runners")
	}
	public := s.TLSCertificatePEM()
	block, _ := pem.Decode(public)
	if block == nil {
		n.t.Fatal("control fixture has no public TLS certificate")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		n.t.Fatal("invalid public test certificate")
	}
	path := filepath.Join(filepath.Dir(n.Config), "test-tls-ca.pem")
	if err := os.WriteFile(path, public, 0o600); err != nil {
		n.t.Fatal("could not write public test CA")
	}
	digest := sha1.Sum(block.Bytes)
	thumbprint := hex.EncodeToString(digest[:])
	var install []string
	var remove [][]string
	switch runtime.GOOS {
	case "windows":
		install = []string{"certutil", "-user", "-addstore", "Root", path}
		remove = [][]string{{"certutil", "-user", "-delstore", "Root", thumbprint}}
	case "darwin":
		install = []string{"security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", "/Library/Keychains/System.keychain", path}
		remove = [][]string{
			{"security", "remove-trusted-cert", "-d", path},
			{"security", "delete-certificate", "-Z", thumbprint, "/Library/Keychains/System.keychain"},
		}
	case "linux":
		return // New already supplied SSL_CERT_FILE to the child process.
	default:
		n.t.Fatal("native TLS trust setup is unsupported on this platform")
	}
	run := func(args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, args[0], args[1:]...).Run()
	}
	n.t.Cleanup(func() {
		for _, args := range remove {
			if err := run(args); err != nil {
				n.t.Error("could not remove the exact ephemeral test CA or trust entry")
			}
		}
	})
	if err := run(install); err != nil {
		n.t.Fatal("could not install ephemeral control-plane test CA")
	}
}
