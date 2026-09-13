package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRPCBundleRejectsLinkedFileAndParent(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "parent"}[parent], func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "target")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(target, "bundles.state")
			store, err := openClientRPCBundleStore(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.put("owner", "profile", []byte("archive")); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(root, "alias")
			linked := path
			if parent {
				linked = target
			}
			if err := os.Symlink(linked, alias); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if parent {
				alias = filepath.Join(alias, "bundles.state")
			}
			if opened, err := openClientRPCBundleStore(alias, nil); err == nil || opened != nil {
				t.Fatal("linked path accepted")
			}
			restored, err := openClientRPCBundleStore(path, nil)
			if err != nil || len(restored.items) != 1 {
				t.Fatal("link target changed", err)
			}
		})
	}
}
