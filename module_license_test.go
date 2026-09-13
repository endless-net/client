package clientrepo_test

import (
	"os"
	"strings"
	"testing"
)

// The independently consumed Go module must carry the repository license in
// its own archive. License scanners cannot rely on a parent outside that module.
func TestNativeIPCModuleCarriesRepositoryLicense(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	root := read("LICENSE")
	if root == "" || read("clientipc/LICENSE") != root {
		t.Fatal("native IPC module must contain the unchanged repository license")
	}
}
