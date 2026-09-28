// Command verify-contract-results requires the selected isolated reports per platform.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/endless-net/client/internal/contractshard"
)

var platforms = []string{"ubuntu-22.04", "ubuntu-24.04", "ubuntu-22.04-arm", "ubuntu-24.04-arm", "windows-2022", "windows-2025", "macos-15", "macos-15-intel"}

var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

type event struct {
	Action  string
	Package string
	Test    string
}

type outcome struct {
	runs, passes int
	active       bool
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: verify-contract-results REPORT_DIRECTORY SOURCE_SHA REPETITIONS(1|3)")
		os.Exit(1)
	}
	repetitions, _ := strconv.Atoi(os.Args[3])
	n, err := verifyReports(os.Args[1], os.Args[2], repetitions)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Verified %d common scenarios x %d repetitions x %d platforms: %d PASS outcomes, no skips.\n", n, repetitions, len(platforms), n*repetitions*len(platforms))
}

func declaredTests(data []byte) ([]string, error) {
	return contractshard.ParseInventory(data)
}

func verifyReports(dir, sha string, repetitions int) (int, error) {
	if repetitions != 1 && repetitions != 3 {
		return 0, errors.New("repetitions must be 1 or 3")
	}
	if !commitSHA.MatchString(sha) {
		return 0, errors.New("expected source must be a full commit SHA")
	}
	var common []string
	var failures []error
	for _, platformName := range platforms {
		for repetition := 1; repetition <= repetitions; repetition++ {
			platform := fmt.Sprintf("%s-%d", platformName, repetition)
			var complete []string
			selectedRoots := make(map[string]bool)
			selectedShards := 0
			for shard := 1; shard <= contractshard.Count; shard++ {
				names, selected, err := verifyShardReport(dir, platformName, platform, sha, shard)
				if err != nil {
					failures = append(failures, fmt.Errorf("%s-%d: %w", platform, shard, err))
				}
				if names != nil {
					if complete == nil {
						complete = names
					} else if !slices.Equal(complete, names) {
						failures = append(failures, fmt.Errorf("%s-%d: compiled scenario inventory differs across shards", platform, shard))
					}
				}
				if selected != nil {
					selectedShards++
				}
				for _, name := range selected {
					if selectedRoots[name] {
						failures = append(failures, fmt.Errorf("%s: duplicate scenario selection %s", platform, name))
					}
					selectedRoots[name] = true
				}
			}
			if complete != nil {
				if selectedShards == contractshard.Count && len(selectedRoots) != len(complete) {
					failures = append(failures, fmt.Errorf("%s: incomplete scenario selection", platform))
				}
				if common == nil {
					common = complete
				} else if !slices.Equal(common, complete) {
					failures = append(failures, fmt.Errorf("%s: compiled scenario inventory differs across platforms", platform))
				}
			}
		}
	}
	if len(failures) != 0 {
		return 0, errors.Join(failures...)
	}
	return len(common), nil
}

func verifyShardReport(dir, platformName, platform, sha string, shardIndex int) ([]string, []string, error) {
	identity := fmt.Sprintf("%s-%d", platform, shardIndex)
	root := filepath.Join(dir, "client-contracts-"+identity)
	shard, err := os.ReadFile(filepath.Join(root, "shard.txt"))
	if err != nil || strings.TrimSpace(string(shard)) != identity {
		return nil, nil, errors.New("missing or mismatched shard identity")
	}
	source, err := os.ReadFile(filepath.Join(root, "source.txt"))
	if err != nil || strings.TrimSpace(string(source)) != sha {
		return nil, nil, errors.New("missing or mismatched source identity")
	}
	inventory, err := os.ReadFile(filepath.Join(root, "expected-tests.txt"))
	if err != nil {
		return nil, nil, errors.New("missing compiled test inventory")
	}
	names, err := declaredTests(inventory)
	if err != nil {
		return nil, nil, err
	}
	if err := contractshard.RequireFlowRoots(names); err != nil {
		return names, nil, err
	}
	selectedData, err := os.ReadFile(filepath.Join(root, "selected-tests.txt"))
	if err != nil {
		return names, nil, errors.New("missing selected test inventory")
	}
	selected, err := declaredTests(selectedData)
	if err != nil {
		return names, nil, err
	}
	if !slices.Equal(selected, contractshard.Split(names)[shardIndex-1]) {
		return names, selected, errors.New("selected scenarios do not match deterministic shard assignment")
	}
	file, err := os.Open(filepath.Join(root, "results.jsonl"))
	if err != nil {
		return names, selected, errors.New("missing execution report")
	}
	err = verifyEvents(file, selected, requiredPlatformSubtests(platformName, selected)...)
	if closeErr := file.Close(); closeErr != nil {
		err = errors.Join(err, errors.New("report close failed"))
	}
	return names, selected, err
}

func requiredPlatformSubtests(platform string, names []string) []string {
	var required []string
	if slices.Contains(names, "TestControlPlaneCLIIPCUnaryTimeout") {
		required = append(required, "TestControlPlaneCLIIPCUnaryTimeout/before-headers", "TestControlPlaneCLIIPCUnaryTimeout/partial-body")
	}
	for _, root := range []string{
		"TestControlPlaneNativeApplicationRoute", "TestControlPlaneNativeExitRoute",
		"TestControlPlaneNativeMachineSharing", "TestControlPlaneRoutedResource",
		"TestControlPlaneNativeServiceCatalog", "TestControlPlaneNativeMTUPreference",
		"TestControlPlaneNativeCachedMapExpiry",
		"TestControlPlaneJoinTokenRotation",
		"TestControlPlaneJoinTokenExpiryRecovery",
		"TestControlPlaneSessionExpiryRecovery",
	} {
		if slices.Contains(names, root) {
			required = append(required, root+"/ipv4", root+"/ipv6")
		}
	}
	for _, family := range []string{"ipv4", "ipv6"} {
		for _, protocol := range []string{"tcp", "udp"} {
			if slices.Contains(names, "TestControlPlaneNativeLogoutTraffic") {
				for _, cleanup := range []string{"logout", "local-forget"} {
					required = append(required, "TestControlPlaneNativeLogoutTraffic/"+cleanup+"/"+family+"/"+protocol)
				}
			}
		}
	}
	if strings.HasPrefix(platform, "ubuntu-") && slices.Contains(names, "TestControlPlaneSubnetRouter") {
		required = append(required, "TestControlPlaneSubnetRouter/snat", "TestControlPlaneSubnetRouter/preserve-source")
	}
	if slices.Contains(names, "TestControlPlaneDNSWireRecovery") {
		for _, upstream := range []string{"udp4", "udp6"} {
			for _, listener := range []string{"127.0.0.1", "::1"} {
				for _, answer := range []string{"complete-udp", "truncated-udp"} {
					required = append(required, "TestControlPlaneDNSWireRecovery/"+upstream+"/"+listener+"/"+answer)
				}
			}
		}
	}
	if slices.Contains(names, "TestControlPlaneTLSTrustBoundary") {
		for _, validity := range []string{"expired", "not-yet-valid", "enrolled-expired", "enrolled-not-yet-valid"} {
			required = append(required, "TestControlPlaneTLSTrustBoundary/"+validity)
		}
	}
	return required
}

func verifyEvents(reader io.Reader, names []string, requiredSubtests ...string) error {
	states := make(map[string]*outcome, len(names))
	for _, name := range names {
		states[name] = &outcome{}
	}
	for _, name := range requiredSubtests {
		states[name] = &outcome{}
	}
	packagePass := false
	decoder := json.NewDecoder(reader)
	for {
		var e event
		if err := decoder.Decode(&e); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return errors.New("invalid JSONL execution report")
		}
		if e.Package != "client/contracts" || e.Action == "" || packagePass {
			return errors.New("invalid report package or events after completion")
		}
		if e.Action == "fail" || e.Action == "skip" {
			return errors.New("report contains a failed or skipped test")
		}
		if e.Test == "" {
			if e.Action == "pass" {
				packagePass = true
			}
			continue
		}
		root, _, child := strings.Cut(e.Test, "/")
		state, ok := states[root]
		if !ok {
			return errors.New("report contains an undeclared test")
		}
		if child {
			if !state.active {
				return errors.New("subtest event outside an active root scenario")
			}
			state, ok = states[e.Test]
			if !ok {
				state = &outcome{}
				states[e.Test] = state
			}
		}
		switch e.Action {
		case "run":
			if state.active {
				return errors.New("overlapping root scenario executions")
			}
			state.runs++
			state.active = true
		case "pass":
			if !state.active {
				return errors.New("pass without a matching scenario start")
			}
			state.passes++
			state.active = false
		}
	}
	if !packagePass {
		return errors.New("execution report has no successful package completion")
	}
	for name, state := range states {
		if state.active || state.runs != 1 || state.passes != 1 {
			return fmt.Errorf("%s: expected exactly one complete successful execution in this repetition", name)
		}
	}
	return nil
}
