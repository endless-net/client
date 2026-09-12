package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordedTest2JSONSubtests(t *testing.T) {
	// Action/package/test fields from the Ubuntu 22.04 artifact of CI
	// 34625686577, source 5ad2709. Output/timestamps and unrelated tests are
	// omitted; the first repetition and final package event are retained.
	data, err := os.ReadFile("testdata/single-subtests.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyEvents(bytes.NewReader(data), []string{"TestControlPlaneBrowserEnrollment"}); err != nil {
		t.Fatal(err)
	}
}

func goodReport(names []string, repetitions int) []byte {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	for range repetitions {
		for _, name := range names {
			_ = encoder.Encode(event{Action: "run", Package: "client/contracts", Test: name})
			_ = encoder.Encode(event{Action: "run", Package: "client/contracts", Test: name + "/case"})
			_ = encoder.Encode(event{Action: "pass", Package: "client/contracts", Test: name + "/case"})
			_ = encoder.Encode(event{Action: "pass", Package: "client/contracts", Test: name})
		}
	}
	_ = encoder.Encode(event{Action: "pass", Package: "client/contracts"})
	return out.Bytes()
}

func TestRejectIncompleteOrUnequalExecution(t *testing.T) {
	names := []string{"TestControlPlaneAlpha", "TestControlPlaneBeta"}
	good := string(goodReport(names, 1))
	if err := verifyEvents(strings.NewReader(good), names); err != nil {
		t.Fatal(err)
	}
	for name, report := range map[string]string{
		"missing repetition":  string(goodReport(names, 0)),
		"extra repetition":    string(goodReport(names, 2)),
		"three in one runner": string(goodReport(names, 3)),
		"missing scenario":    string(goodReport(names[:1], 1)),
		"undeclared scenario": strings.ReplaceAll(good, "TestControlPlaneBeta", "TestControlPlaneOther"),
		"wrong package":       strings.ReplaceAll(good, "client/contracts", "another/package"),
		"unfinished child":    strings.Replace(good, `"Action":"pass","Package":"client/contracts","Test":"TestControlPlaneAlpha/case"`, `"Action":"output","Package":"client/contracts","Test":"TestControlPlaneAlpha/case"`, 1),
		"unstarted child":     strings.Replace(good, `"Action":"run","Package":"client/contracts","Test":"TestControlPlaneAlpha/case"`, `"Action":"output","Package":"client/contracts","Test":"TestControlPlaneAlpha/case"`, 1),
		"skipped child":       strings.Replace(good, `"Action":"pass","Package":"client/contracts","Test":"TestControlPlaneAlpha/case"`, `"Action":"skip","Package":"client/contracts","Test":"TestControlPlaneAlpha/case"`, 1),
		"failed child":        strings.Replace(good, `"Action":"pass","Package":"client/contracts","Test":"TestControlPlaneAlpha/case"`, `"Action":"fail","Package":"client/contracts","Test":"TestControlPlaneAlpha/case"`, 1),
		"missing start":       strings.Replace(good, `"Action":"run","Package":"client/contracts","Test":"TestControlPlaneAlpha"`, `"Action":"output","Package":"client/contracts","Test":"TestControlPlaneAlpha"`, 1),
		"truncated":           good[:strings.LastIndex(strings.TrimSpace(good), "\n")+1],
		"malformed":           good[:len(good)/2],
		"empty":               "",
		"duplicate report":    good + good,
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifyEvents(strings.NewReader(report), names); err == nil {
				t.Fatal("incomplete or invalid execution was accepted")
			}
		})
	}
}

func TestRequireBothLinuxSubnetRouterModes(t *testing.T) {
	names := []string{"TestControlPlaneSubnetRouter"}
	for _, platform := range platforms {
		t.Run(platform, func(t *testing.T) {
			required := requiredPlatformSubtests(platform, names)
			for _, children := range [][]string{nil, {"snat"}, {"preserve-source"}, {"snat", "preserve-source"}} {
				var report bytes.Buffer
				encoder := json.NewEncoder(&report)
				write := func(action, test string) {
					_ = encoder.Encode(event{Action: action, Test: test, Package: "client/contracts"})
				}
				write("run", names[0])
				for _, child := range children {
					write("run", names[0]+"/"+child)
					write("pass", names[0]+"/"+child)
				}
				write("pass", names[0])
				write("pass", "")
				err := verifyEvents(&report, names, required...)
				wantFailure := strings.HasPrefix(platform, "ubuntu-") && len(children) != 2
				if (err != nil) != wantFailure {
					t.Fatalf("children=%v: error=%v, want failure=%t", children, err, wantFailure)
				}
			}
		})
	}
}

func TestRequireEveryDNSWireVariant(t *testing.T) {
	leaves := []string{
		"udp4/127.0.0.1/complete-udp", "udp4/127.0.0.1/truncated-udp",
		"udp4/::1/complete-udp", "udp4/::1/truncated-udp",
		"udp6/127.0.0.1/complete-udp", "udp6/127.0.0.1/truncated-udp",
		"udp6/::1/complete-udp", "udp6/::1/truncated-udp",
	}
	checkRequiredLeaves(t, "TestControlPlaneDNSWireRecovery", leaves)
}

func TestRequireEveryTLSValidityVariant(t *testing.T) {
	checkRequiredLeaves(t, "TestControlPlaneTLSTrustBoundary", []string{"expired", "not-yet-valid", "enrolled-expired", "enrolled-not-yet-valid"})
}

func checkRequiredLeaves(t *testing.T, root string, leaves []string) {
	t.Helper()
	names := []string{root}
	for _, platform := range platforms {
		required := requiredPlatformSubtests(platform, names)
		for missing := -1; missing < len(leaves); missing++ {
			var report bytes.Buffer
			encoder := json.NewEncoder(&report)
			write := func(action, test string) {
				_ = encoder.Encode(event{Action: action, Test: test, Package: "client/contracts"})
			}
			write("run", names[0])
			for index, leaf := range leaves {
				if index == missing {
					continue
				}
				write("run", names[0]+"/"+leaf)
				write("pass", names[0]+"/"+leaf)
			}
			write("pass", names[0])
			write("pass", "")
			err := verifyEvents(&report, names, required...)
			if (err != nil) != (missing >= 0) {
				t.Fatalf("platform=%s missing=%d: %v", platform, missing, err)
			}
		}
	}
}

func TestRequireNativeAddressAndProtocolVariants(t *testing.T) {
	for _, root := range []string{
		"TestControlPlaneNativeApplicationRoute", "TestControlPlaneNativeExitRoute",
		"TestControlPlaneNativeMachineSharing", "TestControlPlaneRoutedResource",
		"TestControlPlaneNativeServiceCatalog", "TestControlPlaneNativeMTUPreference",
		"TestControlPlaneNativeCachedMapExpiry",
	} {
		t.Run(root, func(t *testing.T) { checkRequiredLeaves(t, root, []string{"ipv4", "ipv6"}) })
	}
	checkRequiredLeaves(t, "TestControlPlaneNativeFlowConsent", []string{"ipv4/tcp", "ipv4/udp", "ipv6/tcp", "ipv6/udp"})
	checkRequiredLeaves(t, "TestControlPlaneNativeLogoutTraffic", []string{
		"logout/ipv4/tcp", "logout/ipv4/udp", "logout/ipv6/tcp", "logout/ipv6/udp",
		"local-forget/ipv4/tcp", "local-forget/ipv4/udp", "local-forget/ipv6/tcp", "local-forget/ipv6/udp",
	})
}

func TestRequireThreeIsolatedReportsOnEightPlatforms(t *testing.T) {
	const sha = "0123456789012345678901234567890123456789"
	write := func(path string, value []byte) {
		t.Helper()
		if err := os.WriteFile(path, value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, mutation := range []string{"none", "multiple-platform-errors", "missing-platform", "missing-linux-arm", "missing-repetition", "wrong-shard", "missing-shard", "missing-report", "missing-inventory", "source", "inventory", "empty-inventory", "duplicate-inventory", "repeated-in-one-shard"} {
		t.Run(mutation, func(t *testing.T) {
			dir := t.TempDir()
			for _, platform := range platforms {
				for repetition := 1; repetition <= 3; repetition++ {
					shard := fmt.Sprintf("%s-%d", platform, repetition)
					root := filepath.Join(dir, "client-contracts-"+shard)
					if platform == "ubuntu-24.04-arm" && mutation == "missing-linux-arm" {
						continue
					}
					if platform == "windows-2025" && mutation == "missing-platform" {
						continue
					}
					if platform == "windows-2025" && repetition == 2 && mutation == "missing-repetition" {
						continue
					}
					if err := os.MkdirAll(root, 0o700); err != nil {
						t.Fatal(err)
					}
					write(filepath.Join(root, "source.txt"), []byte(sha+"\n"))
					write(filepath.Join(root, "shard.txt"), []byte(shard+"\n"))
					write(filepath.Join(root, "expected-tests.txt"), []byte("TestControlPlaneAlpha\n"))
					write(filepath.Join(root, "results.jsonl"), goodReport([]string{"TestControlPlaneAlpha"}, 1))
					if mutation == "multiple-platform-errors" {
						switch shard {
						case "ubuntu-22.04-1":
							write(filepath.Join(root, "source.txt"), []byte(strings.Repeat("f", 40)))
						case "windows-2025-2":
							write(filepath.Join(root, "results.jsonl"), bytes.Replace(goodReport([]string{"TestControlPlaneAlpha"}, 1), []byte(`"pass"`), []byte(`"fail"`), 1))
						case "macos-15-intel-3":
							if err := os.Remove(filepath.Join(root, "results.jsonl")); err != nil {
								t.Fatal(err)
							}
						}
					}
					if platform != "windows-2025" || repetition != 2 {
						continue
					}
					switch mutation {
					case "missing-report", "missing-inventory", "missing-shard":
						file := "results.jsonl"
						if mutation == "missing-inventory" {
							file = "expected-tests.txt"
						}
						if mutation == "missing-shard" {
							file = "shard.txt"
						}
						if err := os.Remove(filepath.Join(root, file)); err != nil {
							t.Fatal(err)
						}
					case "source":
						write(filepath.Join(root, "source.txt"), []byte(strings.Repeat("f", 40)))
					case "inventory":
						write(filepath.Join(root, "expected-tests.txt"), []byte("TestControlPlaneBeta\n"))
					case "empty-inventory":
						write(filepath.Join(root, "expected-tests.txt"), nil)
					case "duplicate-inventory":
						write(filepath.Join(root, "expected-tests.txt"), []byte("TestControlPlaneAlpha\nTestControlPlaneAlpha\n"))
					case "wrong-shard":
						write(filepath.Join(root, "shard.txt"), []byte("windows-2025-1\n"))
					case "repeated-in-one-shard":
						write(filepath.Join(root, "results.jsonl"), goodReport([]string{"TestControlPlaneAlpha"}, 3))
					}
				}
			}
			n, err := verifyReports(dir, sha)
			if mutation == "none" {
				if err != nil || n != 1 {
					t.Fatalf("valid reports: count=%d err=%v", n, err)
				}
			} else if err == nil {
				t.Fatal("invalid platform reports were accepted")
			}
			if mutation == "multiple-platform-errors" {
				for _, expected := range []string{
					"ubuntu-22.04-1: missing or mismatched source identity",
					"windows-2025-2: report contains a failed or skipped test",
					"macos-15-intel-3: missing execution report",
				} {
					if !strings.Contains(err.Error(), expected) {
						t.Fatalf("aggregate omitted %q: %v", expected, err)
					}
				}
				if n != 0 || len(strings.Split(err.Error(), "\n")) != 3 {
					t.Fatalf("unexpected aggregate result: count=%d error=%v", n, err)
				}
			}
		})
	}
}
