package clientapp

import (
	"maps"
	"runtime"
	"testing"

	contract "github.com/endless-net/coordinator/api/enrollment"
)

func TestProcessProbeReportsOnlyRequestedConclusivePaths(t *testing.T) {
	base := "/opt/agent/"
	if runtime.GOOS == "windows" {
		base = `C:\Agent\`
	}
	a, b, c := base+"a", base+"b", base+"c"
	plan := &contract.ProcessProbePlan{Platform: runtime.GOOS, ExecutablePaths: []string{a, b, c}}
	scan := func(visit func(string, string)) bool {
		visit("a", a)
		visit("b", "")
		visit("other", base+"other")
		return true
	}
	if got := measureProcesses(plan, scan); !maps.Equal(got, map[string]bool{a: true, c: false}) {
		t.Fatal(got)
	}
	got := measureProcesses(plan, func(visit func(string, string)) bool { visit("a", a); return false })
	if !maps.Equal(got, map[string]bool{a: true}) {
		t.Fatal("partial snapshot", got)
	}
}
