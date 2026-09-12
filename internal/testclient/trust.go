package testclient

import (
	"context"
	"crypto/sha1" // Certificate-store lookup identifier, not a signature algorithm.
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/endless-net/client/internal/testcontrol"
)

// TrustControlTLS installs only the fixture's ephemeral public CA on disposable
// native CI runners. Windows/macOS use OS trust stores; Linux uses the driver's
// process-scoped SSL_CERT_FILE. Cleanup removes this exact certificate.
// On macOS the certificate-hash trust entry lives until hosted VM disposal;
// removing the last entry requires interactive authorization even as root.
func (n *Node) TrustControlTLS(s *testcontrol.Server) {
	n.t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" {
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
		// The elevated disposable runner uses machine trust, avoiding the
		// interactive confirmation required by the current-user root store.
		install = []string{"certutil", "-addstore", "Root", path}
		remove = [][]string{{"certutil", "-delstore", "Root", thumbprint}}
	case "darwin":
		install = []string{"security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", "/Library/Keychains/System.keychain", path}
		remove = [][]string{
			{"security", "delete-certificate", "-Z", thumbprint, "/Library/Keychains/System.keychain"},
		}
	case "linux":
		return // New already supplied SSL_CERT_FILE to the child process.
	default:
		n.t.Fatal("native TLS trust setup is unsupported on this platform")
	}
	n.t.Cleanup(func() {
		for index, args := range remove {
			if err := runTrustCommand(args); err != nil {
				n.t.Errorf("could not remove the exact ephemeral test CA (step %d): %v", index+1, err)
			}
		}
	})
	if err := runTrustCommand(install); err != nil {
		n.t.Fatalf("could not install ephemeral control-plane test CA: %v", err)
	}
}

func runTrustCommand(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.WaitDelay = 2 * time.Second
	output, err := cmd.Output()
	if err == nil {
		return nil
	}
	code := -1
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
		output = append(output, exit.Stderr...)
	}
	reason := "unclassified"
	for _, phrase := range []string{"unknown format", "invalid certificate", "unsupported algorithm", "user interaction is not allowed", "authorization was denied", "permission denied", "could not be found", "unable to read"} {
		if strings.Contains(strings.ToLower(string(output)), phrase) {
			reason = phrase
			break
		}
	}
	return fmt.Errorf("exit=%d reason=%s deadline=%t", code, reason, ctx.Err() != nil)
}
