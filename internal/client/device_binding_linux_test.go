//go:build linux

package client

import "testing"

func TestResolveLinuxInstallationStateDir(t *testing.T) {
	tests := []struct {
		name          string
		configuredDir string
		effectiveUID  int
		wantDir       string
		wantOwned     bool
		wantErr       bool
	}{
		{name: "root machine state", effectiveUID: 0, wantDir: "/var/lib/endlessnet", wantOwned: true},
		{name: "ordinary user fallback", effectiveUID: 1000},
		{name: "explicit service state", configuredDir: "/var/lib/endlessnet/../endlessnet", effectiveUID: 1000, wantDir: "/var/lib/endlessnet", wantOwned: true},
		{name: "relative override rejected", configuredDir: "var/lib/endlessnet", effectiveUID: 0, wantOwned: true, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotDir, gotOwned, err := resolveLinuxInstallationStateDir(tc.configuredDir, tc.effectiveUID)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, tc.wantErr)
			}
			if gotDir != tc.wantDir || gotOwned != tc.wantOwned {
				t.Fatalf("result = (%q, %v), want (%q, %v)", gotDir, gotOwned, tc.wantDir, tc.wantOwned)
			}
		})
	}
}
