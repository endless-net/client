package main

import "testing"

func TestDiagnosticsSessionAuthorityKeysAreSensitive(t *testing.T) {
	for _, key := range []string{"bearer", "Bearer", "renewal_authorization", "renewalAuthorization", "user_session", "UserSession"} {
		if !sensitiveDiagnosticsKey(key) {
			t.Errorf("session authority key is not redacted: %s", key)
		}
	}
	if sensitiveDiagnosticsKey("token_present") {
		t.Fatal("presence flag is not secret material")
	}
}
