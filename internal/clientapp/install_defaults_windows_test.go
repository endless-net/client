//go:build windows

package clientapp

import (
	"encoding/json"
	"os"
	"testing"
)

func TestInstallationDefaultsNeedOnlyMachineFactsAndServer(t *testing.T) {
	data, err := os.ReadFile("../../packaging/windows/install-defaults.json")
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	fixture := configEnvironment()
	for _, key := range []string{"APPROVAL_BASE", "INSTALLATION_ID", "DEFAULT_PROFILE_ID", "STATE_DIRECTORY", "SERVICE_SID", "ALLOWED_SIDS", "CONTROL_ORIGIN", "CONNECTION_ENDPOINT"} {
		name := "ENDLESSNET_CLIENT_" + key
		if _, present := values[name]; present {
			t.Fatalf("machine/server fact in defaults: %s", key)
		}
		values[name] = fixture[name]
	}
	if _, err := ParseWindows(values); err != nil {
		t.Fatal("installation defaults do not satisfy the producer runtime")
	}
}
