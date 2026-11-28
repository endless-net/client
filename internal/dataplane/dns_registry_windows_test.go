package dataplane

import (
	"errors"
	"fmt"
	"net/netip"
	"testing"

	"github.com/endless-net/client/internal/state"
	"github.com/google/uuid"
	"golang.org/x/sys/windows/registry"
)

func TestWindowsCrashReconcilesDNSRuleAgainstRegistry(t *testing.T) {
	// DNS Client never reads this location; the test exercises native registry
	// transactions without modifying the computer's DNS policy.
	base := `Software\EndlessNetTest-` + uuid.NewString()
	root, _, err := registry.CreateKey(registry.CURRENT_USER, base, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = root.Close()
		if err := registry.DeleteKey(registry.CURRENT_USER, base); err != nil {
			t.Error(err)
		}
	})
	c, store, _, _ := cleanupFixture(t)
	c.dnsRules = windowsDNSRegistry{registry.CURRENT_USER, base}
	record, values, err := newDNSRule(netip.MustParseAddr("127.17.0.53"), []string{".dev.test", "dev.test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := cleanupContext(t)
	j := store.value.Profiles[cleanupProfile].Connection.Journal
	j.DNSRules = []state.DNSRuleRecord{record}
	if err := c.dnsRules.install(ctx, record, values); err != nil {
		t.Fatal(err)
	}
	key := "{" + record.Key + "}"
	t.Cleanup(func() { _ = registry.DeleteKey(root, key) })
	if err := c.dnsRules.install(ctx, record, values); !errors.Is(err, ErrIPConflict) {
		t.Fatal("duplicate create", err)
	}
	k, err := registry.OpenKey(root, key, registry.READ|registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	// A new process has only the prepared receipt, just as after a crash
	// between the registry commit and acknowledgement in the protected store.
	if err := k.SetStringValue("Comment", "administrator"); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(ctx, cleanupProfile, policySession, 1); !errors.Is(err, ErrIPConflict) {
		t.Fatal("foreign value", err)
	}
	j = store.value.Profiles[cleanupProfile].Connection.Journal
	if len(j.DNSRules) != 1 || j.DNSRules[0].Phase != state.EffectRemoving {
		t.Fatal("dropped uncertain removal")
	}
	if text, _, err := k.GetStringValue("Comment"); err != nil || text != "administrator" {
		t.Fatal("foreign change lost", err)
	}
	if err := k.DeleteValue("Comment"); err != nil {
		t.Fatal(err)
	}
	if err := k.SetStringValue("GenericDNSServers", "127.17.0.54"); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(ctx, cleanupProfile, policySession, 1); !errors.Is(err, ErrIPConflict) {
		t.Fatal("foreign server", err)
	}
	if err := k.SetStringValue("GenericDNSServers", "127.17.0.53"); err != nil {
		t.Fatal(err)
	}
	_ = k.Close()
	if err := c.Run(ctx, cleanupProfile, policySession, 1); err != nil {
		t.Fatal(err)
	}
	if store.value.Profiles[cleanupProfile].Connection.Journal != nil {
		t.Fatal("cleanup not retired")
	}
	if _, err := registry.OpenKey(root, key, registry.READ); !errors.Is(err, registry.ErrNotExist) {
		t.Fatal("rule remains", err)
	}
	record.Phase = state.EffectRemoving
	if err := c.dnsRules.remove(ctx, record); err != nil {
		t.Fatal("already absent", err)
	}
}

func TestWindowsDNSRuleLostJournalAcknowledgement(t *testing.T) {
	for _, after := range []int{1, 2} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			s, store, slot, m, _, _, _, _, _ := wiredSessionFixture(t)
			if err := startSessionFixture(t, s, m); err != nil {
				t.Fatal(err)
			}
			base := `Software\EndlessNetTest-` + uuid.NewString()
			root, _, err := registry.CreateKey(registry.CURRENT_USER, base, registry.ALL_ACCESS)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = root.Close()
				if err := registry.DeleteKey(registry.CURRENT_USER, base); err != nil {
					t.Error(err)
				}
			})
			s.cleanup.dnsRules = windowsDNSRegistry{registry.CURRENT_USER, base}
			slot.loseACKAt = slot.writes + after
			err = s.replaceDNSRule(t.Context(), m, netip.MustParseAddr("127.17.0.53"), []string{".dev.test", "dev.test"})
			if !errors.Is(err, state.ErrUncertain) {
				t.Fatal("uncertain write accepted", err)
			}
			slot.loseACKAt = 0
			v, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			rules := v.Profiles[cleanupProfile].Connection.Journal.DNSRules
			if len(rules) != 1 {
				t.Fatal(rules)
			}
			key := "{" + rules[0].Key + "}"
			t.Cleanup(func() { _ = registry.DeleteKey(root, key) })
			k, err := registry.OpenKey(root, key, registry.READ)
			if after == 1 {
				if !errors.Is(err, registry.ErrNotExist) || rules[0].Phase != state.EffectPrepared {
					t.Fatal("mutation preceded receipt", err, rules)
				}
			} else {
				if err != nil || rules[0].Phase != state.EffectApplied {
					t.Fatal("missing native receipt", err, rules)
				}
				_ = k.Close()
			}
			if err := s.Stop(cleanupContext(t)); err != nil {
				t.Fatal(err)
			}
			if _, err := registry.OpenKey(root, key, registry.READ); !errors.Is(err, registry.ErrNotExist) {
				t.Fatal("cleanup left rule", err)
			}
		})
	}
}

func TestWindowsDNSRuleReplacement(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprintf("adminEdit=%t", conflict), func(t *testing.T) {
			s, _, _, m, _, _, _, _, _ := wiredSessionFixture(t)
			if err := startSessionFixture(t, s, m); err != nil {
				t.Fatal(err)
			}
			base := `Software\EndlessNetTest-` + uuid.NewString()
			root, _, err := registry.CreateKey(registry.CURRENT_USER, base, registry.ALL_ACCESS)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				names, _ := root.ReadSubKeyNames(-1)
				for _, name := range names {
					_ = registry.DeleteKey(root, name)
				}
				_ = root.Close()
				if err := registry.DeleteKey(registry.CURRENT_USER, base); err != nil {
					t.Error(err)
				}
			})
			s.cleanup.dnsRules = windowsDNSRegistry{registry.CURRENT_USER, base}
			onlyRule := func() string {
				t.Helper()
				names, err := root.ReadSubKeyNames(-1)
				if err != nil || len(names) != 1 {
					t.Fatal(names, err)
				}
				return names[0]
			}
			namespace := []string{"dev.test", ".dev.test"}
			if err := s.replaceDNSRule(t.Context(), m, netip.MustParseAddr("127.17.1.53"), namespace); err != nil {
				t.Fatal(err)
			}
			old := onlyRule()
			if conflict {
				k, err := registry.OpenKey(root, old, registry.SET_VALUE)
				if err != nil {
					t.Fatal(err)
				}
				err = k.SetStringValue("Comment", "admin")
				_ = k.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			err = s.replaceDNSRule(t.Context(), m, netip.MustParseAddr("127.17.2.53"), namespace)
			if conflict {
				if !errors.Is(err, ErrIPConflict) || onlyRule() != old {
					t.Fatal("replacement did not roll back", err)
				}
				return
			}
			if err != nil || onlyRule() == old {
				t.Fatal("replacement missing", err)
			}
			if err := s.replaceDNSRule(t.Context(), m, netip.Addr{}, nil); err != nil {
				t.Fatal(err)
			}
			names, err := root.ReadSubKeyNames(-1)
			if err != nil || len(names) != 0 {
				t.Fatal("withdrawal", names, err)
			}
		})
	}
}
