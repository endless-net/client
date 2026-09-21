package client

import (
	"context"
	"crypto/ed25519"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	api "github.com/endless-net/client-api/clientapi/v1"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func resourceReconciliationFixture(t *testing.T) (Config, api.RegisterNodeResponse, ed25519.PrivateKey, string) {
	t.Helper()
	opts, key := signedServiceDNSFixture(t)
	id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, "db\x00tcp\x005432")
	cfg := Config{NodeID: opts.NetworkMap.Node.ID, NetworkID: opts.NetworkMap.Network.ID, CachedMap: &opts.NetworkMap, MapSigningTrust: opts.SigningTrust,
		MapRevision: opts.NetworkMap.Network.Revision, MapGlobalRevision: opts.NetworkMap.Revision.Global, ResourcePreferences: map[string]bool{id: false}}
	next := *clonePersistentConfig(cfg).CachedMap
	next.Network.Services[0].Ports[0].Port = 5433
	next.Network.Revision++
	next.Revision.Network = next.Network.Revision
	resignApplicationMap(t, &next, key)
	return cfg, next, key, id
}

func installResourceReconciliationMap(t *testing.T, cfg *Config, next api.RegisterNodeResponse) {
	t.Helper()
	if err := ReconcileResourcePreferencesForMap(cfg, next, time.Now()); err != nil {
		t.Fatal(err)
	}
	cfg.CachedMap = &next
	cfg.MapRevision, cfg.MapGlobalRevision, cfg.MapHash = next.Network.Revision, next.Revision.Global, next.MapSignature.PayloadHash
}

func TestResourceChoicesRetireAndReturnWithPolicyAfterRestart(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(strconv.FormatBool(locked), func(t *testing.T) {
			cfg, next, key, id := resourceReconciliationFixture(t)
			original := clonePersistentConfig(cfg)
			oldRules, err := compileResourceDenials(cfg, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			filter := &resourcePacketFilter{}
			if err := filter.suspend(oldRules, cfg.CachedMap.MapSignature.ExpiresAt); err != nil {
				t.Fatal(err)
			}
			filter.commit()
			installResourceReconciliationMap(t, &cfg, next)
			if cfg.ResourcePreferences != nil || cfg.ResourcePreferencesRetired[id] || len(cfg.ResourcePreferencesRetired) != 1 {
				t.Fatal("choice was lost rather than retired")
			}
			if len(original.ResourcePreferences) != 1 {
				t.Fatal("reconciliation mutated source alias")
			}
			rules, err := compileResourceDenials(cfg, time.Now())
			if err != nil {
				t.Fatal("new port map remains blocked by old choice", err)
			}
			if err := filter.suspend(rules, next.MapSignature.ExpiresAt); err != nil {
				t.Fatal(err)
			}
			packet := applicationTCPPacket("100.64.0.1", "100.64.0.2", 50000, 5432)
			if filter.allows(packet, false, time.Now()) {
				t.Fatal("old denial released before map apply commit")
			}
			filter.withdraw()
			if filter.allows(packet, false, time.Now()) {
				t.Fatal("failed map apply released old scope")
			}
			m, _, _ := rpcPreferenceFixture(t)
			if err := m.store.Update(func(current *Config) error {
				current.CachedMap, current.MapSigningTrust = cfg.CachedMap, cfg.MapSigningTrust
				current.MapRevision, current.MapGlobalRevision, current.MapHash = cfg.MapRevision, cfg.MapGlobalRevision, cfg.MapHash
				current.ResourcePreferences, current.ResourcePreferencesRetired = cfg.ResourcePreferences, cfg.ResourcePreferencesRetired
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			cfg = reopenRPCStoreFromDisk(t, m.store).Read()
			returning := *clonePersistentConfig(cfg).CachedMap
			returning.Network.Services[0].Ports[0].Port = 5432
			returning.Network.Revision++
			returning.Revision.Network = returning.Network.Revision
			returning.Network.ClientPolicy = &api.ClientPolicy{Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceService, ID: "db", Source: api.ClientPolicyDevice, PolicyID: "service", Enabled: true, Locked: locked}}}
			resignApplicationMap(t, &returning, key)
			installResourceReconciliationMap(t, &cfg, returning)
			setting, err := resolveResourcePreference(cfg, id, time.Now())
			if err != nil || setting.Requested == nil || *setting.Requested || setting.Enabled != locked || len(cfg.ResourcePreferencesRetired) != 0 {
				t.Fatal("returned ID lost durable intent or current policy", setting, err)
			}
		})
	}
}

func TestResourceChoiceMapReconciliationRejectsAtomically(t *testing.T) {
	for _, scenario := range []string{"tampered_next", "unsigned_next", "foreign_node", "foreign_network", "foreign_previous", "rollback", "same_revision", "malformed_active", "malformed_retired"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, next, key, _ := resourceReconciliationFixture(t)
			switch scenario {
			case "tampered_next":
				next.Network.Name = "tampered"
			case "unsigned_next":
				next.MapSignature = nil
			case "foreign_node":
				cfg.NodeID = "other"
			case "foreign_network":
				cfg.NetworkID = "other"
			case "foreign_previous":
				cfg.CachedMap.Node.ID = "other"
			case "rollback":
				cfg.MapRevision = next.Network.Revision + 1
			case "same_revision":
				next.Network.Revision = cfg.MapRevision
				next.Revision.Network = cfg.MapRevision
				resignApplicationMap(t, &next, key)
			case "malformed_active":
				cfg.ResourcePreferences["garbage"] = false
			case "malformed_retired":
				cfg.ResourcePreferencesRetired = map[string]bool{"garbage": false}
			}
			before := clonePersistentConfig(cfg)
			if err := ReconcileResourcePreferencesForMap(&cfg, next, time.Now()); err == nil {
				t.Fatal("invalid transition accepted")
			}
			if !reflect.DeepEqual(before, cfg) {
				t.Fatal("rejected transition mutated choices/map")
			}
		})
	}
}

func TestResourceChoiceReconciliationAllowsExpiredPreviousMap(t *testing.T) {
	cfg, next, key, _ := resourceReconciliationFixture(t)
	var err error
	cfg.CachedMap.MapSignature, err = api.SignNetworkMapAt(key, *cfg.CachedMap, time.Now().Add(-2*time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	installResourceReconciliationMap(t, &cfg, next)
	if _, err := compileResourceDenials(cfg, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestResourceChoiceRetirementContainsPendingOperationAfterRestart(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	cfg, next, _, id := resourceReconciliationFixture(t)
	if err := m.store.Update(func(current *Config) error {
		current.CachedMap, current.MapSigningTrust = cfg.CachedMap, cfg.MapSigningTrust
		current.ResourcePreferences = cfg.ResourcePreferences
		current.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	op, err := m.setResourceEnabledAs(owner, &ipc.SetResourceEnabledRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ResourceId: id, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	journal := m.store.Read().RPCState.NetworkPreferenceChange
	if err := m.store.Update(func(current *Config) error { installResourceReconciliationMap(t, current, next); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(journal, m.store.Read().RPCState.NetworkPreferenceChange) {
		t.Fatal("map transition overwrote accepted operation")
	}
	m, err = NewClientRPCMutations(reopenRPCStoreFromDisk(t, m.store))
	if err != nil {
		t.Fatal(err)
	}
	stops := 0
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Start: func(context.Context, Config) error { return errors.New("stale candidate must not start") }, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		stops++
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_INTERRUPTED, nil
	}}
	if err := m.ReconcileNetworkPreferences(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	result, err := m.operationAs(owner, &ipc.GetOperationRequest{Lookup: &ipc.GetOperationRequest_OperationId{OperationId: op.Id}})
	if err != nil {
		t.Fatal(err)
	}
	if stops != 1 || result.State != ipc.OperationState_OPERATION_STATE_FAILED || result.GetFailure().GetCode() != ipc.ErrorCode_ERROR_CODE_STALE_STATE || len(m.store.Read().ResourcePreferencesRetired) != 1 {
		t.Fatal("pending operation did not contain or lost retired choice", result)
	}
}

func TestResourceRetirementLimitDoesNotBlockMapRefresh(t *testing.T) {
	m, owner, profile := rpcPreferenceFixture(t)
	cfg, next, _, _ := resourceReconciliationFixture(t)
	cfg.ResourcePreferencesRetired = make(map[string]bool)
	for i := 0; i < resourcePreferenceLimit-1; i++ {
		cfg.ResourcePreferencesRetired[rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, "retired-"+strconv.Itoa(i))] = false
	}
	installResourceReconciliationMap(t, &cfg, next)
	if len(cfg.ResourcePreferencesRetired) != resourcePreferenceLimit {
		t.Fatal("map update evicted a remembered choice")
	}
	if err := m.store.Update(func(current *Config) error {
		current.CachedMap, current.MapSigningTrust = cfg.CachedMap, cfg.MapSigningTrust
		current.MapRevision, current.MapGlobalRevision, current.MapHash = cfg.MapRevision, cfg.MapGlobalRevision, cfg.MapHash
		current.ResourcePreferences, current.ResourcePreferencesRetired = cfg.ResourcePreferences, cfg.ResourcePreferencesRetired
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := m.store.Read()
	id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_SERVICE, "db\x00tcp\x005433")
	_, err := m.setResourceEnabledAs(owner, &ipc.SetResourceEnabledRequest{Mutation: rpcCreateRequest(t, m).Mutation, Profile: profile, ResourceId: id, Enabled: false})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_LIMIT_EXCEEDED)
	if !reflect.DeepEqual(before, m.store.Read()) {
		t.Fatal("limit rejection mutated state")
	}
	if err := ReconcileResourcePreferencesForMap(&cfg, next, time.Now()); err != nil {
		t.Fatal("full choice budget blocked unchanged map refresh", err)
	}
}

func TestResourceRetirementCopiesAndIdentityCleanup(t *testing.T) {
	cfg, next, _, id := resourceReconciliationFixture(t)
	installResourceReconciliationMap(t, &cfg, next)
	for name, copyFields := range map[string]func(*Config, Config){"enrollment": copyEnrollmentFields, "recovery": copyRPCRecoveryFields, "startup": ApplyStartupPolicyMap} {
		t.Run(name, func(t *testing.T) {
			var target Config
			copyFields(&target, cfg)
			if len(target.ResourcePreferencesRetired) != 1 {
				t.Fatal("copy lost retired intent")
			}
			target.ResourcePreferencesRetired[id] = true
			if cfg.ResourcePreferencesRetired[id] {
				t.Fatal("copy aliased retired map")
			}
		})
	}
	changed := clonePersistentConfig(cfg)
	changed.ResourcePreferencesRetired[id] = true
	if StartupPolicyContextEqual(cfg, changed) || reflect.DeepEqual(enrollmentFields(cfg), enrollmentFields(changed)) {
		t.Fatal("late authority response could overwrite changed resource intent")
	}
	clearNodeBoundState(&changed)
	if changed.ResourcePreferences != nil || changed.ResourcePreferencesRetired != nil {
		t.Fatal("retired choices survived node identity cleanup")
	}
}

func TestResourceAlreadyOrphanedChoiceRepairsAndReturnsUnderLockedPolicy(t *testing.T) {
	cfg, current, key, id := resourceReconciliationFixture(t)
	// Reproduce a cache saved before reconciliation existed: 5432 is still an
	// active preference although the current authenticated catalog has 5433.
	cfg.CachedMap = &current
	cfg.MapRevision, cfg.MapGlobalRevision, cfg.MapHash = current.Network.Revision, current.Revision.Global, current.MapSignature.PayloadHash
	cfg.ResourcePreferences[id] = true
	if _, err := compileResourceDenials(cfg, time.Now()); err == nil {
		t.Fatal("compiler silently ignored orphan before reconciliation")
	}
	next := *clonePersistentConfig(cfg).CachedMap
	next.Network.Revision++
	next.Revision.Network = next.Network.Revision
	resignApplicationMap(t, &next, key)
	installResourceReconciliationMap(t, &cfg, next)
	if len(cfg.ResourcePreferences) != 0 || !cfg.ResourcePreferencesRetired[id] {
		t.Fatal("orphan was not explicitly retired")
	}
	if _, err := compileResourceDenials(cfg, time.Now()); err != nil {
		t.Fatal("repaired catalog cannot apply", err)
	}
	m, _, _ := rpcPreferenceFixture(t)
	if err := m.store.Update(func(current *Config) error {
		current.CachedMap, current.MapSigningTrust = cfg.CachedMap, cfg.MapSigningTrust
		current.MapRevision, current.MapGlobalRevision, current.MapHash = cfg.MapRevision, cfg.MapGlobalRevision, cfg.MapHash
		current.ResourcePreferences, current.ResourcePreferencesRetired = cfg.ResourcePreferences, cfg.ResourcePreferencesRetired
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg = reopenRPCStoreFromDisk(t, m.store).Read()
	returning := *clonePersistentConfig(cfg).CachedMap
	returning.Network.Services[0].Ports[0].Port = 5432
	returning.Network.Revision++
	returning.Revision.Network = returning.Network.Revision
	returning.Network.ClientPolicy = &api.ClientPolicy{Resources: []api.ManagedResourceSetting{{Kind: api.ManagedResourceService, ID: "db", Source: api.ClientPolicyDevice, PolicyID: "deny-service", Enabled: false, Locked: true}}}
	resignApplicationMap(t, &returning, key)
	installResourceReconciliationMap(t, &cfg, returning)
	setting, err := resolveResourcePreference(cfg, id, time.Now())
	if err != nil || setting.Requested == nil || !*setting.Requested || setting.Enabled {
		t.Fatal("orphan intent bypassed returned locked policy", setting, err)
	}
	rules, err := compileResourceDenials(cfg, time.Now())
	if err != nil || len(rules) == 0 {
		t.Fatal("returned resource omitted managed denial", err)
	}
}

func TestResourceReconciliationUsesAdoptedTrustForNextMap(t *testing.T) {
	cfg, next, oldKey, id := resourceReconciliationFixture(t)
	var err error
	cfg.CachedMap.MapSignature, err = api.SignNetworkMapAt(oldKey, *cfg.CachedMap, time.Now().Add(-2*time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	adopted, newKey := signedServiceDNSFixture(t)
	cfg.MapSigningTrust = adopted.SigningTrust
	if api.VerifyNetworkMapSignatureWithTrustBundle(*cfg.CachedMap, *cfg.MapSigningTrust) == nil {
		t.Fatal("fixture old key unexpectedly trusted")
	}
	before := clonePersistentConfig(cfg)
	if err := ReconcileResourcePreferencesForMap(&cfg, next, time.Now()); err == nil {
		t.Fatal("next map signed by replaced key accepted")
	}
	if !reflect.DeepEqual(before, cfg) {
		t.Fatal("rejected next map changed choices")
	}
	resignApplicationMap(t, &next, newKey)
	installResourceReconciliationMap(t, &cfg, next)
	if len(cfg.ResourcePreferences) != 0 || len(cfg.ResourcePreferencesRetired) != 1 || cfg.ResourcePreferencesRetired[id] {
		t.Fatal("trust rotation blocked retirement")
	}
	if _, err := compileResourceDenials(cfg, time.Now()); err != nil {
		t.Fatal("adopted-trust map cannot apply", err)
	}
}
