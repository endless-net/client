package client

import (
	"testing"

	"github.com/endless-net/client/clientipc/local"
)

// Artifact generation runs on every host; each target must use the endpoint
// owned by the native transport, independently of the generator's host OS.
func TestServiceDefaultsMatchNativeRPCEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"systemd", DefaultSystemdServiceOptions().IPCSocketPath, local.DefaultUnixSocket},
		{"launchd", DefaultLaunchdServiceOptions().IPCSocketPath, local.DefaultDarwinSocket},
		{"windows", DefaultWindowsServiceOptions().IPCPipe, local.DefaultWindowsPipe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("service endpoint = %q, native transport endpoint = %q", tc.got, tc.want)
			}
		})
	}
}
