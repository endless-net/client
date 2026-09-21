package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func nativeExitPreferenceFixture(t *testing.T) (*NativeExitRuntime, *nativeExitExecutor, *ClientRPCMutations) {
	t.Helper()
	n, cfg, _ := nativeExitResumeFixture(t)
	m := newRPCStoreTest(t)
	cfg.RPCState.Revision, cfg.RPCState.DigestKey = 1, make([]byte, 32)
	cfg.RPCState.Operations = map[string]clientRPCOperationRecord{}
	cfg = clonePersistentConfig(cfg)
	if err := m.store.Update(func(c *Config) error { *c = cfg; return nil }); err != nil {
		t.Fatal(err)
	}
	// Keep background path discovery out of deterministic native readback.
	n.engine.pathCancel = func() {}
	if _, err := n.resumeSaved(t.Context(), m.store.Read()); err != nil {
		t.Fatal(err)
	}
	lock := new(sync.Mutex)
	executor, err := newNativeExitExecutorWithGuard(n.engine, lock, n.createGuard)
	if err != nil {
		t.Fatal(err)
	}
	handle := &NativeExitRuntime{engine: n.engine, store: m.store, lock: lock, executor: executor}
	if err := m.store.Update(func(c *Config) error {
		profile := c.RPCState.Profiles[c.RPCState.ActiveProfileID]
		c.RPCState.NetworkPreferenceChange = &clientRPCNetworkPreferenceChange{
			OperationID: "preferences", ProfileID: profile.ID, OwnerID: c.LocalOwnerID, ControlOrigin: profile.ControlOrigin,
			NodeID: c.NodeID, NetworkID: c.NetworkID, MapHash: c.CachedMap.MapSignature.PayloadHash,
			Previous: cloneNetworkPreferences(c.NetworkPreferences), Requested: &ClientNetworkPreferences{AcceptDNS: proto.Bool(false)},
			PreviousIntent: c.ConnectionIntent, Changed: true,
		}
		raw, marshalErr := proto.Marshal(&ipc.Operation{Id: "preferences", ProfileId: profile.ID, Kind: ipc.OperationKind_OPERATION_KIND_SET_PREFERENCES, State: ipc.OperationState_OPERATION_STATE_RUNNING})
		if marshalErr != nil {
			return marshalErr
		}
		c.RPCState.Operations["preferences"] = clientRPCOperationRecord{Owner: c.LocalOwnerID, Operation: raw}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return handle, n, m
}

func TestNativeExitPreferenceWorkerCommitsActualGuardedRuntime(t *testing.T) {
	r, n, m := nativeExitPreferenceFixture(t)
	called := false
	driver := ClientRPCProfileDriver{Lock: r.lock, Start: func(ctx context.Context, cfg Config) error {
		called = true
		if m.store.Read().NetworkPreferences != nil {
			t.Fatal("candidate committed before effects")
		}
		return r.ApplyPreferenceCandidateLocked(ctx, r.lock, cfg)
	}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		t.Fatal("successful candidate stopped")
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_UNKNOWN, nil
	}}
	if err := m.ReconcileNetworkPreferences(t.Context(), driver); err != nil {
		t.Fatal(err)
	}
	current := m.store.Read()
	if !called || current.RPCState.NetworkPreferenceChange != nil || current.NetworkPreferences == nil || current.NetworkPreferences.AcceptDNS == nil || *current.NetworkPreferences.AcceptDNS || !n.engine.configured {
		t.Fatal("worker did not commit native candidate")
	}
	op := new(ipc.Operation)
	if err := proto.Unmarshal(current.RPCState.Operations["preferences"].Operation, op); err != nil || op.State != ipc.OperationState_OPERATION_STATE_SUCCEEDED {
		t.Fatal("operation not completed", err)
	}
	r.lock.Lock()
	defer r.lock.Unlock()
	if err := n.maintain(t.Context(), current); err != nil {
		t.Fatal("committed candidate not maintainable", err)
	}
}

func TestNativeExitPreferenceRejectsUnboundCandidates(t *testing.T) {
	for _, scenario := range []string{"candidate", "pending", "foreign_owner", "wrong_kind", "barrier", "disconnected", "foreign_runtime", "lock"} {
		t.Run(scenario, func(t *testing.T) {
			r, n, m := nativeExitPreferenceFixture(t)
			candidate, err := nativeExitPreferenceCandidate(m.store.Read(), r.executor.InterfaceName)
			if err != nil {
				t.Fatal(err)
			}
			lock := r.lock
			switch scenario {
			case "candidate":
				candidate.NetworkPreferences.AcceptDNS = proto.Bool(true)
			case "foreign_runtime":
				n.engine.runtimeIdentity.OwnerID = "foreign"
			case "lock":
				lock = new(sync.Mutex)
			default:
				if err := m.store.Update(func(c *Config) error {
					switch scenario {
					case "pending", "foreign_owner", "wrong_kind":
						record := c.RPCState.Operations["preferences"]
						op := new(ipc.Operation)
						if err := proto.Unmarshal(record.Operation, op); err != nil {
							return err
						}
						switch scenario {
						case "pending":
							op.State = ipc.OperationState_OPERATION_STATE_PENDING
						case "wrong_kind":
							op.Kind = ipc.OperationKind_OPERATION_KIND_SET_RESOURCE_ENABLED
						default:
							record.Owner = "foreign"
						}
						raw, err := proto.Marshal(op)
						if err != nil {
							return err
						}
						record.Operation = raw
						c.RPCState.Operations["preferences"] = record
					case "barrier":
						c.RPCState.DisconnectOperationID = "disconnect"
					case "disconnected":
						c.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			device := n.engine.device
			before := clonePersistentConfig(m.store.Read())
			r.lock.Lock()
			err = r.ApplyPreferenceCandidateLocked(t.Context(), lock, candidate)
			r.lock.Unlock()
			if err == nil || device != n.engine.device || !reflect.DeepEqual(before, clonePersistentConfig(m.store.Read())) {
				t.Fatal("invalid candidate changed runtime/store", err)
			}
		})
	}
}

func TestNativeExitPreferenceLateFailureContainsAndKeepsJournal(t *testing.T) {
	for _, scenario := range []string{"cancel", "changed", "native_failure"} {
		t.Run(scenario, func(t *testing.T) {
			r, n, m := nativeExitPreferenceFixture(t)
			candidate, err := nativeExitPreferenceCandidate(m.store.Read(), r.executor.InterfaceName)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			guard := n.engine.exitGuard
			run := guard.run
			fired := false
			guard.run = func(ctx context.Context, input, command string, args ...string) ([]byte, error) {
				output, err := run(ctx, input, command, args...)
				if !fired && command == "nft" {
					fired = true
					switch scenario {
					case "cancel":
						cancel()
					case "changed":
						if updateErr := m.store.Update(func(c *Config) error {
							c.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
							return nil
						}); updateErr != nil {
							t.Fatal(updateErr)
						}
					case "native_failure":
						return nil, errors.New("injected native failure")
					}
				}
				return output, err
			}
			r.lock.Lock()
			err = r.ApplyPreferenceCandidateLocked(ctx, r.lock, candidate)
			r.lock.Unlock()
			if err == nil || !fired || m.store.Read().RPCState.NetworkPreferenceChange == nil || m.store.Read().NetworkPreferences != nil {
				t.Fatal("failed candidate committed", err)
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if err := guard.ObserveContained(t.Context()); err != nil {
				t.Fatal("failure left guard open", err)
			}
		})
	}
}
