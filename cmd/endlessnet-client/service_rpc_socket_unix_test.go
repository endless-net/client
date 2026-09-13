//go:build linux || darwin

package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentRPCSocketRecovery(t *testing.T) {
	for _, scenario := range []string{"absent", "stale", "live", "file", "symlink", "writable directory"} {
		t.Run(scenario, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "en-recover-")
			if err != nil {
				t.Fatal(err)
			}
			endpoint := filepath.Join(dir, "rpc.sock")
			t.Cleanup(func() {
				_ = os.Remove(endpoint)
				_ = os.Remove(dir)
			})
			switch scenario {
			case "stale", "live", "writable directory":
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: endpoint, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				listener.SetUnlinkOnClose(false)
				t.Cleanup(func() { _ = listener.Close() })
				if scenario != "live" {
					if err := listener.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "writable directory" {
					if err := os.Chmod(dir, 0777); err != nil {
						t.Fatal(err)
					}
				}
			case "file":
				if err := os.WriteFile(endpoint, []byte("preserve caller data"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("missing-target", endpoint); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.Lstat(endpoint)
			err = recoverAgentRPCSocket(endpoint)
			wantSuccess := scenario == "absent" || scenario == "stale"
			if (err == nil) != wantSuccess {
				t.Fatalf("unexpected recovery outcome: %v", err)
			}
			after, statErr := os.Lstat(endpoint)
			if wantSuccess {
				if !os.IsNotExist(statErr) {
					t.Fatal("recovered path still exists")
				}
				listener, err := net.Listen("unix", endpoint)
				if err != nil {
					t.Fatal("recovered endpoint cannot be rebound", err)
				}
				_ = listener.Close()
			} else if statErr != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("rejected recovery changed caller path")
			}
		})
	}
	if err := recoverAgentRPCSocket("relative.sock"); err == nil {
		t.Fatal("relative path accepted")
	}
	if err := recoverAgentRPCSocket(""); err != nil {
		t.Fatal("disabled IPC rejected", err)
	}
}
