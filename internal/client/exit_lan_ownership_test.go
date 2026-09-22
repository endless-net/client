package client

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	api "github.com/endless-net/client-api/clientapi/v1"
)

func exitLANOwnershipFixture(mode api.ExitFamilyMode) *exitLANOwnership {
	o := &exitLANOwnership{Scope: "0123456789abcdef01234567", BootID: "12345678-9abc-4def-8123-456789abcdef", NamespaceDevice: 4, NamespaceInode: 5, Family: mode, MapID: 7, ProgramID: 7}
	families, _ := exitLANBPFFamilies(mode)
	for i, family := range families {
		o.Links = append(o.Links, exitLANOwnedLink{ID: uint32(i + 7), ProgramID: 7, Family: family, Hook: 4, Priority: -100})
	}
	return o
}

func TestExitLANOwnershipCanonicalFamilyAndJSON(t *testing.T) {
	for _, mode := range []api.ExitFamilyMode{api.ExitFamilyIPv4Only, api.ExitFamilyIPv6Only, api.ExitFamilyDualStack} {
		o := exitLANOwnershipFixture(mode)
		if err := validateExitLANOwnership(o); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(o)
		if err != nil {
			t.Fatal(err)
		}
		var decoded exitLANOwnership
		if err := json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(o, &decoded) {
			t.Fatal("ownership round trip changed identity", err)
		}
		if err := validateExitLANOwnership(&decoded); err != nil {
			t.Fatal(err)
		}
		// Map, program and link IDs occupy separate kernel type namespaces.
		if o.MapID != o.ProgramID || o.ProgramID != o.Links[0].ID {
			t.Fatal("fixture must exercise equal cross-type IDs")
		}
	}
}

func TestExitLANOwnershipRejectsMalformedIdentity(t *testing.T) {
	cases := map[string]func(*exitLANOwnership){
		"scope_short":       func(o *exitLANOwnership) { o.Scope = "0123" },
		"scope_path":        func(o *exitLANOwnership) { o.Scope = strings.Repeat("/", 24) },
		"scope_upper":       func(o *exitLANOwnership) { o.Scope = strings.Repeat("A", 24) },
		"boot_zero":         func(o *exitLANOwnership) { o.BootID = "00000000-0000-0000-0000-000000000000" },
		"boot_upper":        func(o *exitLANOwnership) { o.BootID = strings.ToUpper(o.BootID) },
		"boot_hyphen":       func(o *exitLANOwnership) { o.BootID = strings.ReplaceAll(o.BootID, "-", "0") },
		"boot_short":        func(o *exitLANOwnership) { o.BootID = o.BootID[:35] },
		"boot_nonhex":       func(o *exitLANOwnership) { o.BootID = "z" + o.BootID[1:] },
		"device":            func(o *exitLANOwnership) { o.NamespaceDevice = 0 },
		"inode":             func(o *exitLANOwnership) { o.NamespaceInode = 0 },
		"map":               func(o *exitLANOwnership) { o.MapID = 0 },
		"program":           func(o *exitLANOwnership) { o.ProgramID = 0 },
		"family":            func(o *exitLANOwnership) { o.Family = "unknown" },
		"missing":           func(o *exitLANOwnership) { o.Links = o.Links[:1] },
		"extra":             func(o *exitLANOwnership) { o.Links = append(o.Links, o.Links[0]) },
		"order":             func(o *exitLANOwnership) { o.Links[0], o.Links[1] = o.Links[1], o.Links[0] },
		"zero_id":           func(o *exitLANOwnership) { o.Links[0].ID = 0 },
		"duplicate_id":      func(o *exitLANOwnership) { o.Links[1].ID = o.Links[0].ID },
		"other_program":     func(o *exitLANOwnership) { o.Links[1].ProgramID++ },
		"other_family":      func(o *exitLANOwnership) { o.Links[1].Family = 2 },
		"hook_range":        func(o *exitLANOwnership) { o.Links[0].Hook = 5 },
		"hook_mismatch":     func(o *exitLANOwnership) { o.Links[1].Hook = 3 },
		"priority_min":      func(o *exitLANOwnership) { o.Links[0].Priority = math.MinInt32 },
		"priority_max":      func(o *exitLANOwnership) { o.Links[0].Priority = math.MaxInt32 },
		"priority_mismatch": func(o *exitLANOwnership) { o.Links[1].Priority++ },
	}
	if validateExitLANOwnership(nil) == nil {
		t.Fatal("nil accepted")
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			o := exitLANOwnershipFixture(api.ExitFamilyDualStack)
			mutate(o)
			if validateExitLANOwnership(o) == nil {
				t.Fatal("invalid ownership accepted")
			}
		})
	}
}

func TestExitLANOwnershipCloneIsIndependent(t *testing.T) {
	if cloneExitLANOwnership(nil) != nil {
		t.Fatal("nil clone")
	}
	o := exitLANOwnershipFixture(api.ExitFamilyDualStack)
	clone := cloneExitLANOwnership(o)
	if clone == o || !reflect.DeepEqual(clone, o) {
		t.Fatal("clone changed identity")
	}
	clone.Links[0].ID++
	clone.Scope = "changed"
	if clone.Links[0].ID == o.Links[0].ID || clone.Scope == o.Scope {
		t.Fatal("clone aliases original")
	}
	for _, links := range [][]exitLANOwnedLink{nil, {}} {
		o.Links = links
		if !reflect.DeepEqual(cloneExitLANOwnership(o), o) {
			t.Fatal("clone changed nil/empty slice")
		}
	}
}
