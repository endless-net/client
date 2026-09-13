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

// Parse imports on every platform, including OS-tagged installer/traffic tests.
// This guards the migrated harness only; legacy component DTOs elsewhere still
// need removal and this source boundary is not runtime acceptance evidence.
func TestRuntimeHarnessDoesNotImportRetiredIPC(t *testing.T) {
	for _, root := range []string{"tests", "internal/testclient"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".go" {
				return nil
			}
			retired, err := importsRetiredIPC(path, nil)
			if err != nil {
				return err
			}
			if retired {
				t.Errorf("%s imports retired IPC instead of the native contract", filepath.ToSlash(path))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func importsRetiredIPC(name string, source any) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ImportsOnly)
	if err != nil {
		return false, err
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return false, err
		}
		if path == "github.com/endless-net/client/ipc/v2" || strings.HasPrefix(path, "github.com/endless-net/client/ipc/v2/") {
			return true, nil
		}
	}
	return false, nil
}

func TestRetiredIPCImportDetectionIncludesAliasesAndBuildTags(t *testing.T) {
	for name, source := range map[string]string{
		"ordinary":    "package fixture\nimport \"github.com/endless-net/client/ipc/v2\"",
		"aliased":     "package fixture\nimport old \"github.com/endless-net/client/ipc/v2\"",
		"blank":       "package fixture\nimport _ \"github.com/endless-net/client/ipc/v2\"",
		"dot":         "package fixture\nimport . \"github.com/endless-net/client/ipc/v2\"",
		"windows":     "//go:build windows\n\npackage fixture\nimport \"github.com/endless-net/client/ipc/v2\"",
		"sub-package": "package fixture\nimport \"github.com/endless-net/client/ipc/v2/compat\"",
	} {
		t.Run(name, func(t *testing.T) {
			if retired, err := importsRetiredIPC("fixture.go", source); err != nil || !retired {
				t.Fatal("retired import escaped the boundary", err)
			}
		})
	}
	for _, source := range []string{
		"package fixture\nimport ipc \"github.com/endless-net/client/clientipc/v0\"",
		"package fixture\n// import \"github.com/endless-net/client/ipc/v2\"",
	} {
		if retired, err := importsRetiredIPC("fixture.go", source); err != nil || retired {
			t.Fatal("native import or comment rejected", err)
		}
	}
	if _, err := importsRetiredIPC("fixture.go", "not Go source"); err == nil {
		t.Fatal("malformed source silently accepted")
	}
}
