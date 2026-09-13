package clientrepo_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Runtime cutover is tracked separately. Migrated system/recovery scenarios and
// their harness must never regain a dependency on the retired HTTP IPC DTO package.
func TestSystemScenariosAndHarnessRejectRetiredIPCImports(t *testing.T) {
	for _, root := range []string{"tests", "internal/testclient", "cmd/endlessnet-client/recovery_test.go"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, declaration := range file.Imports {
				importPath, err := strconv.Unquote(declaration.Path.Value)
				if err != nil {
					return err
				}
				const retired = "github.com/endless-net/client/ipc"
				if importPath == retired || strings.HasPrefix(importPath, retired+"/") {
					t.Errorf("%s imports retired HTTP IPC; use the native clientipc contract directly", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
