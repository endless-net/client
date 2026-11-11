//go:build windows

package clientapp

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func configEnvironment() map[string]string {
	values := map[string]string{
		"NATIVE_CONSENT_PREFIX": "/api/v1/auth/native-login/",
		"APPROVAL_BASE":         "https://approval.invalid/devices",
		"INSTALLATION_ID":       "5c2e681d-fcd7-4d72-a85b-d7ac21858992", "DEFAULT_PROFILE_ID": "5e510773-e1cf-440f-9224-d0cdf454a185",
		"STATE_DIRECTORY": `C:\fixture-only\missing-client-state`, "SERVICE_SID": "S-1-5-18", "ALLOWED_SIDS": "S-1-5-18,S-1-5-32-544",
		"CONTROL_ORIGIN": "https://control.invalid", "PROFILE_DISPLAY_NAME": "Fixture",
		"CONNECTION_ENDPOINT": "192.0.2.1:51820", "CONNECTION_LISTEN_PORT": "51820", "CONNECTION_MTU": "1420", "CONNECTION_ROUTE_METRIC": "7",
		"CONNECTION_MAX_PEERS": "16", "CONNECTION_MAX_RULES": "32", "CONNECTION_MAX_PROTECTED": "32", "CONNECTION_MAX_INVENTORY_ROWS": "1024", "CONNECTION_MAX_INTERFACES": "256",
		"CONNECTION_MAX_QUEUED_MAPS": "8", "CONNECTION_MAX_MAP_BYTES": "65536", "CONNECTION_MAX_STREAM_RECORDS": "128", "CONNECTION_MAX_CALLS": "2", "CONNECTION_MAX_STREAMS": "1",
		"CONNECTION_STREAM_DURATION": "60s", "CONNECTION_READ_IDLE_TIMEOUT": "15s", "CONNECTION_ENDPOINT_TTL": "120s", "CONNECTION_RENEWAL_INTERVAL": "30s", "CONNECTION_RETRY_INTERVAL": "1s", "CONNECTION_PROBE_INTERVAL": "1s",
		"CONNECTION_POLL_INTERVAL": "100ms", "CONNECTION_CLEANUP_TIMEOUT": "1s", "CONNECTION_DAD_POLL_INTERVAL": "10ms", "CONNECTION_TRANSACTION_WAIT": "100ms", "CONNECTION_APPLY_TIMEOUT": "20s",
		"CONNECTION_RELAY_DIAL_TIMEOUT": "3s", "CONNECTION_RELAY_IO_TIMEOUT": "5s", "CONNECTION_RELAY_HEARTBEAT_INTERVAL": "1s",
		"START_TIMEOUT": "3s", "STOP_TIMEOUT": "3s", "STATUS_TIMEOUT": "100ms", "CHECKPOINT_INTERVAL": "100ms",
		"HANDSHAKE_TIMEOUT": "1s", "DRAIN_TIMEOUT": "1s", "STATE_LOCK_RETRY": "10ms", "CALL_TIMEOUT": "30s", "RETRY_INTERVAL": "1s", "WRITE_BUDGET": "1s",
		"MAX_STATE_PLAIN_BYTES": "65536", "MAX_STATE_CIPHER_BYTES": "131072", "MAX_MESSAGE_BYTES": "65536", "MAX_CONNECTIONS": "8", "MAX_CONCURRENT_RPCS": "16", "MAX_PROFILES": "4",
		"MAX_DISPLAY_NAME_BYTES": "128", "MAX_HOSTNAME_BYTES": "128", "MAX_PENDING": "4", "MAX_COMMIT_RETRIES": "3",
		"MAX_BOOTSTRAP_BYTES": "65536", "MAX_REQUEST_BYTES": "65536", "MAX_RESPONSE_BYTES": "65536", "MAX_CREDENTIAL_BYTES": "16384",
		"MAX_QUERY_CALLS": "16", "MAX_ACCOUNTS": "100", "MAX_TARGETS": "500", "UPSTREAM_PAGE_SIZE": "100", "LOG_LEVEL": "info", "LOG_FORMAT": "json", "LOG_QUEUE_SIZE": "16", "MAX_LOG_BYTES": "2048", "SAMPLE_RATIO": "1", "TELEMETRY_EXPORT": "disabled",
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result["ENDLESSNET_CLIENT_"+key] = value
	}
	return result
}

func TestWindowsConfigUsesOnlyExplicitEnvironmentWithoutIO(t *testing.T) {
	config, err := ParseWindows(configEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if config.CallTimeout != 30*time.Second || config.MaxTargets != 500 || config.ServiceSID != "S-1-5-18" {
		t.Fatal("configuration changed")
	}
	if _, err := ParseWindows(nil); err != ErrConfig {
		t.Fatal(err)
	}
	if _, err := ParseWindows(map[string]string{}); err != ErrConfig {
		t.Fatal(err)
	}
	// The path intentionally does not need to exist; parsing must neither open
	// state nor initialize it or query the installed service.
	for key := range configEnvironment() {
		t.Run("missing_"+key, func(t *testing.T) {
			environment := configEnvironment()
			delete(environment, key)
			t.Setenv(key, configEnvironment()[key])
			got, err := ParseWindows(environment)
			if err != ErrConfig || !reflect.DeepEqual(got, Config{}) {
				t.Fatal("ambient/default fallback or partial result", err)
			}
		})
	}
}

func TestWindowsConfigRejectsInvalidBoundariesWithoutDisclosingValues(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"APPROVAL_BASE", "http://approval.invalid"}, {"APPROVAL_BASE", "https://user:synthetic-secret@approval.invalid"},
		{"APPROVAL_BASE", "https://APPROVAL.invalid"}, {"APPROVAL_BASE", "https://approval.invalid/?secret=synthetic-secret"}, {"APPROVAL_BASE", "https://approval.invalid/#secret"},
		{"INSTALLATION_ID", "00000000-0000-0000-0000-000000000000"}, {"DEFAULT_PROFILE_ID", "bad"},
		{"SERVICE_SID", "SY"}, {"ALLOWED_SIDS", "S-1-5-18,S-1-5-18"}, {"ALLOWED_SIDS", "S-1-5-18,invalid"},
		{"CONTROL_ORIGIN", "http://control.invalid"}, {"CONTROL_ORIGIN", "https://user:synthetic-secret@control.invalid"},
		{"CONTROL_ORIGIN", "https://CONTROL.invalid"}, {"CONTROL_ORIGIN", "https://control.invalid/"}, {"CONTROL_ORIGIN", "https://control.invalid?"},
		{"CONTROL_ORIGIN", "https://control.invalid#"}, {"CONTROL_ORIGIN", "https://control.invalid:0"}, {"CONTROL_ORIGIN", "https://control.invalid:65536"},
		{"STATE_DIRECTORY", `C:\`}, {"STATE_DIRECTORY", `C:relative`}, {"STATE_DIRECTORY", `\\server\share\client`},
		{"STATE_DIRECTORY", `\\?\C:\client`}, {"STATE_DIRECTORY", `C:\client\..\state`}, {"STATE_DIRECTORY", `C:\client\state:secret`},
		{"STATE_DIRECTORY", `C:\client\NUL`}, {"STATE_DIRECTORY", `C:\client\state.`}, {"STATE_DIRECTORY", `C:\client\state `},
		{"MAX_STATE_PLAIN_BYTES", "0"}, {"MAX_STATE_CIPHER_BYTES", "65536"}, {"MAX_CONNECTIONS", "0"}, {"MAX_CONCURRENT_RPCS", "0"},
		{"MAX_PENDING", "3"}, {"MAX_QUERY_CALLS", "-1"}, {"MAX_MESSAGE_BYTES", "999999999999999999999999"},
		{"UPSTREAM_PAGE_SIZE", "501"}, {"UPSTREAM_PAGE_SIZE", "0"}, {"PROFILE_DISPLAY_NAME", " "}, {"PROFILE_DISPLAY_NAME", "invalid\nname"},
		{"MAX_DISPLAY_NAME_BYTES", "1"}, {"LOG_LEVEL", "trace"}, {"CALL_TIMEOUT", "31s"}, {"CALL_TIMEOUT", "synthetic-secret"},
		{"WRITE_BUDGET", "3s"}, {"DRAIN_TIMEOUT", "3s"}, {"STATE_LOCK_RETRY", "2s"}, {"HANDSHAKE_TIMEOUT", "4s"},
		{"STOP_TIMEOUT", "0"}, {"START_TIMEOUT", "-1s"}, {"STATUS_TIMEOUT", "4s"}, {"CHECKPOINT_INTERVAL", "4s"},
		{"CONNECTION_ENDPOINT", "localhost:51820"}, {"CONNECTION_ENDPOINT", "[::1]:51820"}, {"CONNECTION_ENDPOINT", "127.0.0.1:51820"}, {"CONNECTION_ENDPOINT", "192.0.2.1:0"},
		{"CONNECTION_LISTEN_PORT", "0"}, {"CONNECTION_MTU", "575"}, {"CONNECTION_MAX_PEERS", "0"}, {"CONNECTION_MAX_RULES", "0"}, {"CONNECTION_MAX_PROTECTED", "0"},
		{"CONNECTION_MAX_MAP_BYTES", "1048577"}, {"CONNECTION_MAX_STREAM_RECORDS", "0"}, {"CONNECTION_MAX_QUEUED_MAPS", "0"}, {"CONNECTION_MAX_CALLS", "0"}, {"CONNECTION_MAX_STREAMS", "0"},
		{"CONNECTION_ENDPOINT_TTL", "301s"}, {"CONNECTION_RENEWAL_INTERVAL", "120s"}, {"CONNECTION_CLEANUP_TIMEOUT", "3s"}, {"CONNECTION_POLL_INTERVAL", "0"},
		{"CONNECTION_APPLY_TIMEOUT", "31s"}, {"CONNECTION_TRANSACTION_WAIT", "100us"}, {"CONNECTION_READ_IDLE_TIMEOUT", "61s"},
		{"CONNECTION_RELAY_DIAL_TIMEOUT", "31s"}, {"CONNECTION_RELAY_IO_TIMEOUT", "1s"}, {"CONNECTION_RELAY_HEARTBEAT_INTERVAL", "100us"}, {"CONNECTION_MTU", "65535"},
	} {
		t.Run(tc.key+"_"+tc.value, func(t *testing.T) {
			environment := configEnvironment()
			environment["ENDLESSNET_CLIENT_"+tc.key] = tc.value
			got, err := ParseWindows(environment)
			if err != ErrConfig || !reflect.DeepEqual(got, Config{}) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatal("invalid config admitted or disclosed", err)
			}
		})
	}
}
