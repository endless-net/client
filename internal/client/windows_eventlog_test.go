package client

import (
	"strings"
	"testing"
)

func TestRedactServiceLogMessage(t *testing.T) {
	raw := strings.Join([]string{
		"agent sync failed",
		"PrivateKey = secret-private-key",
		`{"node_credential":"secret-node-credential"}`,
		"--join_token enr_secret_underscore",
		"--join-token enr_secret_hyphen",
		"-EnrollToken enr_secret_powershell",
		"Authorization: Bearer session-secret",
		`{"access_token":"oidc-access-secret","refresh_token":"oidc-refresh-secret"}`,
		"ordinary status line",
	}, "\n")
	redacted := RedactServiceLogMessage(raw)
	for _, secret := range []string{
		"secret-private-key",
		"secret-node-credential",
		"enr_secret_underscore",
		"enr_secret_hyphen",
		"enr_secret_powershell",
		"session-secret",
		"oidc-access-secret",
		"oidc-refresh-secret",
	} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted service log leaked %q: %s", secret, redacted)
		}
	}
	for _, want := range []string{"[redacted private key line]", "[redacted node credential line]", "[redacted token line]", "ordinary status line"} {
		if !strings.Contains(redacted, want) {
			t.Fatalf("redacted service log missing %q: %s", want, redacted)
		}
	}
}
