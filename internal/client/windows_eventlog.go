package client

import "strings"

const DefaultWindowsEventLogSource = "EndlessNet Client"

func RedactServiceLogMessage(message string) string {
	lines := strings.Split(message, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		switch {
		case serviceLogLineContainsPrivateKey(lower):
			lines[i] = "[redacted private key line]"
		case serviceLogLineContainsNodeCredential(lower):
			lines[i] = "[redacted node credential line]"
		case serviceLogLineContainsToken(lower):
			lines[i] = "[redacted token line]"
		}
	}
	return strings.Join(lines, "\n")
}

func serviceLogLineContainsPrivateKey(lower string) bool {
	return strings.Contains(lower, "privatekey") ||
		strings.Contains(lower, "private_key") ||
		strings.Contains(lower, "private-key") ||
		strings.Contains(lower, "identity_private_key")
}

func serviceLogLineContainsNodeCredential(lower string) bool {
	return strings.Contains(lower, "node_credential") ||
		strings.Contains(lower, "node-credential") ||
		strings.Contains(lower, "node credential") ||
		strings.Contains(lower, "x-endlessnet-node-credential")
}

func serviceLogLineContainsToken(lower string) bool {
	for _, marker := range []string{
		"join_token",
		"join-token",
		"jointoken",
		"enroll_token",
		"enroll-token",
		"enrolltoken",
		"session token",
		"session_token",
		"session-token",
		"authorization: bearer",
		"access_token",
		"access-token",
		"refresh_token",
		"refresh-token",
		"id_token",
		"id-token",
		"oidc token",
		"oidc_token",
		"oidc-token",
		"token=",
		"token:",
		"enr_",
		"enj_",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
