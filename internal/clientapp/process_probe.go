package clientapp

import (
	"path/filepath"
	"runtime"
	"strings"

	contract "github.com/endless-net/coordinator/api/enrollment"
)

// A complete enumeration proves absence. An unreadable process leaves only
// the paths it might represent unknown. Positive observations still stand.
func measureProcesses(plan *contract.ProcessProbePlan, scan func(func(name, path string)) bool) map[string]bool {
	if plan == nil || plan.Platform != runtime.GOOS || !plan.Valid(true) {
		return nil
	}
	equal := func(a, b string) bool { return a == b }
	if runtime.GOOS == "windows" {
		equal = strings.EqualFold
	}
	found := map[string]bool{}
	unknown := map[string]bool{}
	complete := scan(func(name, path string) {
		for _, want := range plan.ExecutablePaths {
			if path != "" {
				if equal(path, want) {
					found[want] = true
				}
				continue
			}
			if name == "" || equal(name, filepath.Base(want)) {
				unknown[want] = true
			}
		}
	})
	if complete {
		for _, want := range plan.ExecutablePaths {
			if !found[want] && !unknown[want] {
				found[want] = false
			}
		}
	}
	return found
}
