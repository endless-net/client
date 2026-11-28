package clientapp

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	contract "github.com/endless-net/coordinator/api/enrollment"
)

func TestProcessPostureReadsExecutableLinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "123")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "exe")
	if err := os.Symlink("/opt/agent", link); err != nil {
		t.Fatal(err)
	}
	plan := &contract.ProcessProbePlan{Platform: "linux", ExecutablePaths: []string{"/opt/agent", "/opt/other"}}
	read := func() map[string]bool {
		return measureProcesses(plan, func(visit func(string, string)) bool { return scanProcDirectory(root, visit) })
	}
	if got := read(); !maps.Equal(got, map[string]bool{"/opt/agent": true, "/opt/other": false}) {
		t.Fatal(got)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if got := read(); !maps.Equal(got, map[string]bool{"/opt/agent": false, "/opt/other": false}) {
		t.Fatal(got)
	}
	if err := os.WriteFile(link, []byte("unreadable link"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := read(); len(got) != 0 {
		t.Fatal("inconclusive scan", got)
	}
}
