package clientrepo_test

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

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
		if path == "github.com/endless-net/client/ipc" || strings.HasPrefix(path, "github.com/endless-net/client/ipc/") {
			return true, nil
		}
	}
	return false, nil
}

func TestRetiredIPCImportDetectionIncludesAliasesAndBuildTags(t *testing.T) {
	for name, source := range map[string]string{
		"retired-root":          "package fixture\nimport \"github.com/endless-net/client/ipc\"",
		"other-retired-package": "package fixture\nimport \"github.com/endless-net/client/ipc/compat\"",
		"ordinary":              "package fixture\nimport \"github.com/endless-net/client/ipc/v2\"",
		"aliased":               "package fixture\nimport old \"github.com/endless-net/client/ipc/v2\"",
		"blank":                 "package fixture\nimport _ \"github.com/endless-net/client/ipc/v2\"",
		"dot":                   "package fixture\nimport . \"github.com/endless-net/client/ipc/v2\"",
		"windows":               "//go:build windows\n\npackage fixture\nimport \"github.com/endless-net/client/ipc/v2\"",
		"sub-package":           "package fixture\nimport \"github.com/endless-net/client/ipc/v2/compat\"",
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
