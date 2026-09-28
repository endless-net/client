package contractshard

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestSplitCoversInventoryAndIncludesNewRoots(t *testing.T) {
	names, err := ParseInventory([]byte(strings.Join(append(slices.Clone(RequiredFlowRoots),
		"TestControlPlaneAlpha", "TestControlPlaneNewScenario"), "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireFlowRoots(names); err != nil {
		t.Fatal(err)
	}
	groups := Split(names)
	var combined []string
	for _, group := range groups {
		if len(group) == 0 {
			t.Fatal("empty shard")
		}
		combined = append(combined, group...)
		pattern := regexp.MustCompile(Pattern(group))
		for _, name := range group {
			if !pattern.MatchString(name) {
				t.Fatalf("selection pattern omitted %s", name)
			}
		}
	}
	slices.Sort(combined)
	if !slices.Equal(combined, names) {
		t.Fatal("assignment is incomplete or unstable")
	}
	again := Split(slices.Clone(names))
	for i := range groups {
		if !slices.Equal(groups[i], again[i]) {
			t.Fatal("assignment is unstable")
		}
	}
	if !slices.Contains(combined, "TestControlPlaneNewScenario") {
		t.Fatal("new compiled root was omitted")
	}
}

func TestInventoryAndFlowRequirements(t *testing.T) {
	for _, invalid := range []string{"", "TestOther\n", "TestControlPlaneAlpha\nTestControlPlaneAlpha\n"} {
		if _, err := ParseInventory([]byte(invalid)); err == nil {
			t.Fatalf("accepted invalid inventory %q", invalid)
		}
	}
	if err := RequireFlowRoots([]string{"TestControlPlaneAlpha"}); err == nil {
		t.Fatal("accepted missing flow variants")
	}
}
