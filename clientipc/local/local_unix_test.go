//go:build linux || darwin

package local

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestUnixListenNeverReplacesCallerPath(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "stale-socket"} {
		t.Run(kind, func(t *testing.T) {
			// Darwin's socket path limit is shorter than named t.TempDir paths.
			dir, err := os.MkdirTemp("/tmp", "en-path-")
			if err != nil {
				t.Fatal(err)
			}
			endpoint, target := filepath.Join(dir, "rpc.sock"), filepath.Join(dir, "target")
			t.Cleanup(func() {
				for _, path := range []string{endpoint, target, dir} {
					if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
						t.Error(err)
					}
				}
			})
			const contents = "caller-owned synthetic data"
			switch kind {
			case "file":
				err = os.WriteFile(endpoint, []byte(contents), 0600)
			case "symlink":
				if err = os.WriteFile(target, []byte(contents), 0600); err == nil {
					err = os.Symlink(target, endpoint)
				}
			case "stale-socket":
				var listener *net.UnixListener
				listener, err = net.ListenUnix("unix", &net.UnixAddr{Name: endpoint, Net: "unix"})
				if err == nil {
					listener.SetUnlinkOnClose(false)
					err = listener.Close()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			if listener, err := Listen(endpoint); err == nil {
				_ = listener.Close()
				t.Fatal("native listener replaced a caller-owned path")
			}
			after, err := os.Lstat(endpoint)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("rejected listen changed the original path")
			}
			if kind != "stale-socket" {
				data, err := os.ReadFile(endpoint)
				if err != nil || string(data) != contents {
					t.Fatal("rejected listen changed caller data")
				}
			}
			if kind == "symlink" {
				link, err := os.Readlink(endpoint)
				if err != nil || link != target {
					t.Fatal("rejected listen changed the symlink target")
				}
			}
		})
	}
}
