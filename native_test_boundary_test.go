package clientrepo_test

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// Runtime cutover is tracked separately. Migrated system/recovery scenarios and
// their harness must never regain a dependency on the retired HTTP IPC DTO package.
func TestSystemScenariosAndHarnessRejectRetiredIPCImports(t *testing.T) {
	for _, root := range []string{"tests", "internal/testclient", "cmd/endlessnet-client"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" {
				return nil
			}
			// Other command files still contain migration residue. Protect every
			// native adapter (including future files), plus migrated recovery tests.
			if root == "cmd/endlessnet-client" && !strings.HasPrefix(entry.Name(), "service_rpc") && entry.Name() != "recovery_test.go" {
				return nil
			}
			retired, err := importsRetiredIPC(path, nil)
			if err != nil {
				return err
			}
			if retired {
				t.Errorf("%s imports retired HTTP IPC; use the native clientipc contract directly", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
