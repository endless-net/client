// Command verify-contract-results requires three isolated reports per platform.
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
	"strings"
)

var platforms = []string{"ubuntu-22.04", "ubuntu-24.04", "ubuntu-22.04-arm", "ubuntu-24.04-arm", "windows-2022", "windows-2025", "macos-15", "macos-15-intel"}

var testName = regexp.MustCompile(`^TestControlPlane[A-Za-z0-9_]+$`)
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
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: verify-contract-results REPORT_DIRECTORY SOURCE_SHA")
		os.Exit(1)
	}
	n, err := verifyReports(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Verified %d common scenarios x 3 repetitions x %d platforms: %d PASS outcomes, no skips.\n", n, len(platforms), n*3*len(platforms))
}

func declaredTests(data []byte) ([]string, error) {
	names := strings.Fields(string(data))
	if len(names) == 0 {
		return nil, errors.New("empty compiled test inventory")
	}
	slices.Sort(names)
	for i, name := range names {
		if !testName.MatchString(name) || i > 0 && names[i-1] == name {
			return nil, errors.New("invalid or duplicate compiled test name")
		}
	}
	return names, nil
}

func verifyReports(dir, sha string) (int, error) {
	if !commitSHA.MatchString(sha) {
		return 0, errors.New("expected source must be a full commit SHA")
	}
	var common []string
	var failures []error
	for _, platformName := range platforms {
		for repetition := 1; repetition <= 3; repetition++ {
			platform := fmt.Sprintf("%s-%d", platformName, repetition)
			names, err := verifyPlatformReport(dir, platformName, platform, sha)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", platform, err))
			}
			if names == nil {
				continue
			}
			if common == nil {
				common = names
			} else if !slices.Equal(common, names) {
				failures = append(failures, fmt.Errorf("%s: compiled scenario inventory differs across platforms", platform))
			}
		}
	}
	if len(failures) != 0 {
		return 0, errors.Join(failures...)
	}
	return len(common), nil
}

func verifyPlatformReport(dir, platformName, platform, sha string) ([]string, error) {
	root := filepath.Join(dir, "client-contracts-"+platform)
	shard, err := os.ReadFile(filepath.Join(root, "shard.txt"))
	if err != nil || strings.TrimSpace(string(shard)) != platform {
		return nil, errors.New("missing or mismatched repetition identity")
	}
	source, err := os.ReadFile(filepath.Join(root, "source.txt"))
	if err != nil || strings.TrimSpace(string(source)) != sha {
		return nil, errors.New("missing or mismatched source identity")
	}
	inventory, err := os.ReadFile(filepath.Join(root, "expected-tests.txt"))
	if err != nil {
		return nil, errors.New("missing compiled test inventory")
	}
	names, err := declaredTests(inventory)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(root, "results.jsonl"))
	if err != nil {
		return names, errors.New("missing execution report")
	}
	err = verifyEvents(file, names, requiredPlatformSubtests(platformName, names)...)
	if closeErr := file.Close(); closeErr != nil {
		err = errors.Join(err, errors.New("report close failed"))
	}
	return names, err
}

func requiredPlatformSubtests(platform string, names []string) []string {
	var required []string
	for _, root := range []string{
		"TestControlPlaneNativeApplicationRoute", "TestControlPlaneNativeExitRoute",
		"TestControlPlaneNativeMachineSharing", "TestControlPlaneRoutedResource",
		"TestControlPlaneNativeServiceCatalog", "TestControlPlaneNativeMTUPreference",
		"TestControlPlaneNativeCachedMapExpiry",
		"TestControlPlaneJoinTokenRotation",
		"TestControlPlaneSessionExpiryRecovery",
	} {
		if slices.Contains(names, root) {
			required = append(required, root+"/ipv4", root+"/ipv6")
		}
	}
	for _, family := range []string{"ipv4", "ipv6"} {
		for _, protocol := range []string{"tcp", "udp"} {
			if slices.Contains(names, "TestControlPlaneNativeFlowConsent") {
				required = append(required, "TestControlPlaneNativeFlowConsent/"+family+"/"+protocol)
			}
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
