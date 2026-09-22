package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestResourceNativeObservationRunsOutsideMutationLockAndRechecksScope(t *testing.T) {
	for _, scenario := range []string{"unchanged", "unauthorized", "owner", "map", "expired", "cancelled", "worker", "busy"} {
		t.Run(scenario, func(t *testing.T) {
			m, owner, profile := rpcPreferenceFixture(t)
			s := NewClientRPCService(m, nil)
			lock := &sync.Mutex{}
			s.ResourceObservationLock = lock
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			s.ResourceHostProvider = func(ctx context.Context, cfg Config) (*ResourceHostObservation, error) {
				calls++
				if lock.TryLock() {
					lock.Unlock()
					t.Fatal("native read did not own the shared effect lock")
				}
				if !m.mu.TryLock() {
					t.Fatal("native read blocked RPC mutations")
				}
				m.mu.Unlock()
				if _, bounded := ctx.Deadline(); !bounded || cfg.CachedMap == nil {
					t.Fatal("unbounded or unbound native read")
				}
				switch scenario {
				case "owner", "map":
					if err := m.store.Update(func(current *Config) error {
						if scenario == "owner" {
							current.LocalOwnerID = "replacement-owner"
						} else {
							current.MapRevision++
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				case "cancelled":
					cancel()
				case "expired":
					m.now = func() time.Time { return cfg.CachedMap.MapSignature.ExpiresAt }
				case "worker":
					s.profileMu.Lock()
					s.profileWorker = &clientRPCProfileWorker{ctx: ctx}
					s.profileMu.Unlock()
				}
				return nil, errors.New("private native diagnostic")
			}
			if scenario == "busy" {
				lock.Lock()
			}
			if scenario == "unauthorized" {
				owner.Identity = "foreign-owner"
			}
			rows, err := s.resourcesAs(ctx, owner, &ipc.ListResourcesRequest{Profile: profile})
			if scenario == "busy" {
				lock.Unlock()
			}
			switch scenario {
			case "owner", "unauthorized":
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
			case "map", "worker":
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
			case "expired":
				assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_UNAVAILABLE)
			case "cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatal("late cancellation ignored", err)
				}
			default:
				if err != nil || rows == nil {
					t.Fatal("unavailable native evidence hid valid catalog", err)
				}
				for _, row := range rows.Resources {
					if row.Availability.Availability == ipc.Availability_AVAILABILITY_AVAILABLE {
						t.Fatal("missing native evidence advertised availability")
					}
				}
			}
			skipped := scenario == "busy" || scenario == "unauthorized"
			if skipped && calls != 0 || !skipped && calls != 1 {
				t.Fatal("native callback count", calls)
			}
			if !lock.TryLock() {
				t.Fatal("observation leaked effect lock")
			}
			lock.Unlock()
		})
	}
}

func TestResourceObservationOwnsProviderLifetimeThroughPublication(t *testing.T) {
	for _, scenario := range []string{"success", "partial_error", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			m := newRPCStoreTest(t)
			s := NewClientRPCService(m, nil)
			lock := &sync.Mutex{}
			s.ResourceObservationLock = lock
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var providerCtx context.Context
			closed := 0
			s.ResourceHostProvider = func(ctx context.Context, _ Config) (*ResourceHostObservation, error) {
				providerCtx = ctx
				proof := &ResourceHostObservation{close: func() error {
					closed++
					if ctx.Err() == nil {
						t.Error("provider not cancelled before close")
					}
					if lock.TryLock() {
						lock.Unlock()
						t.Error("effect lock released before observer cleanup")
					}
					return nil
				}}
				if scenario == "partial_error" {
					return proof, errors.New("partial collection")
				}
				if scenario == "cancel" {
					cancel()
				}
				return proof, nil
			}
			proof, release := s.observeResourceHosts(ctx, m.store.Read())
			if (proof != nil) != (scenario == "success") || providerCtx == nil {
				t.Fatal("unexpected receipt")
			}
			if scenario != "cancel" && providerCtx.Err() != nil {
				t.Fatal("provider lifetime ended before publication")
			}
			if closed != 0 {
				t.Fatal("observer closed before publication")
			}
			release()
			release()
			if closed != 1 || providerCtx.Err() == nil || !lock.TryLock() {
				t.Fatal("cleanup not exactly once or lock leaked", closed)
			}
			lock.Unlock()
		})
	}
}

func TestResourceHostProjectionAndEventsRequireFreshEvidence(t *testing.T) {
	cfg, engine, template := resourceHostProjectionFixture(t)
	m := newRPCStoreTest(t)
	cfg.RPCState.Revision = 1
	cfg.RPCState.DigestKey = make([]byte, 32)
	cfg.RPCState.Operations = map[string]clientRPCOperationRecord{}
	if err := m.store.Update(func(next *Config) error { *next = clonePersistentConfig(cfg); return nil }); err != nil {
		t.Fatal(err)
	}
	s := NewClientRPCService(m, nil)
	s.ResourceObservationLock = &sync.Mutex{}
	s.ResourceEnforcementProvider = engine.TryResourceEnforcement
	id := rpcResourceID(ipc.ResourceKind_RESOURCE_KIND_HOST, cfg.CachedMap.Peers[0].ID)
	confirmed := false
	// Inject only the trusted observer result. Native command/path proof is
	// covered separately; this verifies service projection and event semantics.
	s.ResourceHostProvider = func(ctx context.Context, current Config) (*ResourceHostObservation, error) {
		proof := *template
		proof.configuration = resourceObservationConfig(current)
		engine.mu.Lock()
		proof.paths = resourceObservationPaths(engine)
		engine.mu.Unlock()
		// A previously positive route batch loses authority when its topology
		// stream changes; the host list itself remains affirmative.
		proof.hosts = map[string]bool{id: true}
		stream := &exitLANTestStream{changed: make(chan struct{})}
		proof.lifetime = &exitLANSourceLifetime{ctx: ctx, stream: stream}
		proof.close = sync.OnceValue(stream.Close)
		if !confirmed {
			_ = stream.Close()
		}
		return &proof, nil
	}
	owner := local.Peer{Identity: cfg.LocalOwnerID}
	request := &ipc.ListResourcesRequest{Profile: &ipc.ProfileRef{ProfileId: cfg.RPCState.ActiveProfileID}}
	if err := s.publishResourceClock(t.Context()); err != nil {
		t.Fatal(err)
	}
	sub, err := m.subscribe(owner, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := sub.next(ctx); err != nil {
		t.Fatal(err)
	}
	for _, value := range []bool{true, false, true} {
		confirmed = value
		rows, err := s.resourcesAs(ctx, owner, request)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows.Resources {
			if row.Id == id {
				found = true
				if (row.Availability.Availability == ipc.Availability_AVAILABILITY_AVAILABLE) != confirmed {
					t.Fatal("HOST availability ignored fresh proof", row)
				}
			} else if row.Availability.Availability == ipc.Availability_AVAILABILITY_AVAILABLE {
				t.Fatal("HOST proof widened to another resource kind")
			}
		}
		if !found {
			t.Fatal("fixture did not expose HOST")
		}
		before := m.Metadata().Revision
		if err := s.publishResourceClock(ctx); err != nil {
			t.Fatal(err)
		}
		if m.Metadata().Revision != before+1 {
			t.Fatal("host path transition did not advance revision")
		}
		for {
			event, err := sub.next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if inv := event.GetInvalidated(); inv != nil {
				if inv.Domain != ipc.Domain_DOMAIN_RESOURCES || inv.ProfileId != request.Profile.ProfileId {
					t.Fatal("wrong resource invalidation", event)
				}
				break
			}
		}
		if err := s.publishResourceClock(ctx); err != nil {
			t.Fatal(err)
		}
		if m.Metadata().Revision != before+1 {
			t.Fatal("unchanged proof caused duplicate invalidation")
		}
	}
	// Even an affirmative observer cannot override an applied local denial.
	if err := m.store.Update(func(current *Config) error { current.ResourcePreferences = map[string]bool{id: false}; return nil }); err != nil {
		t.Fatal(err)
	}
	s.ResourceEnforcementProvider = func(Config, time.Time) bool { return true }
	rows, err := s.resourcesAs(ctx, owner, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows.Resources {
		if row.Id == id && (row.Enabled.Effective || row.Availability.ReasonKey != "resource_packet_restriction_applied") {
			t.Fatal("positive proof overrode denial", row)
		}
	}
}

func TestResourceClockDiscardsLateNativeScope(t *testing.T) {
	m, _, _ := rpcPreferenceFixture(t)
	s := NewClientRPCService(m, nil)
	s.ResourceObservationLock = &sync.Mutex{}
	s.ResourceHostProvider = func(context.Context, Config) (*ResourceHostObservation, error) {
		if !m.mu.TryLock() {
			t.Fatal("clock held mutation lock during native read")
		}
		m.mu.Unlock()
		if err := m.store.Update(func(cfg *Config) error { cfg.MapRevision++; return nil }); err != nil {
			t.Fatal(err)
		}
		return nil, nil
	}
	before := m.Metadata().Revision
	if err := s.publishResourceClock(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.observedResourceEnforcement != nil || m.Metadata().Revision != before {
		t.Fatal("stale native result was published")
	}
}
