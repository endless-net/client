package client

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestExitSavedResumeRechecksConcurrentAdmissionBeforeUnlocking(t *testing.T) {
	for _, scenario := range []string{"stable", "disconnect", "cancel_and_disconnect", "native_error", "already_disconnected"} {
		t.Run(scenario, func(t *testing.T) {
			_, cfg, _ := nativeExitResumeFixture(t)
			if scenario == "already_disconnected" {
				cfg.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
			}
			m := newRPCStoreTest(t)
			if err := m.store.Update(func(current *Config) error { *current = clonePersistentConfig(cfg); return nil }); err != nil {
				t.Fatal(err)
			}
			lock := &sync.Mutex{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			resumes, checks := 0, 0
			executor := clientRPCExitExecutor{InterfaceName: cfg.RPCState.ExitProtection.InterfaceName, Lock: lock,
				ResumeSaved: func(_ context.Context, input Config) (*ipc.ExitNodeStatus, error) {
					resumes++
					if lock.TryLock() {
						lock.Unlock()
						t.Fatal("resume ran without effect lock")
					}
					if scenario == "disconnect" || scenario == "cancel_and_disconnect" {
						if err := m.store.Update(func(current *Config) error {
							current.ConnectionIntent.DesiredState = ConnectionIntentDesiredDisconnected
							return nil
						}); err != nil {
							t.Fatal(err)
						}
					}
					if scenario == "cancel_and_disconnect" {
						cancel()
					}
					if scenario == "native_error" {
						return nil, errors.New("private native output")
					}
					return nativeExitSelectedStatus(input.RPCState.ActiveProfileID, input.ExitSelection), nil
				},
				Maintain: func(recovery context.Context, input Config) error {
					checks++
					if input.ConnectionIntent.DesiredState != ConnectionIntentDesiredDisconnected {
						t.Fatal("resume recovery used stale connected intent")
					}
					if recovery.Err() != nil {
						t.Fatal("cancelled resume prevented context containment")
					}
					deadline, ok := recovery.Deadline()
					if !ok || time.Until(deadline) > 5*time.Second {
						t.Fatal("resume recovery is unbounded")
					}
					if lock.TryLock() {
						lock.Unlock()
						t.Fatal("changed context escaped before maintenance")
					}
					return nil
				},
			}
			err := m.reconcileSavedExit(ctx, executor)
			if scenario == "cancel_and_disconnect" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
			} else if err != nil {
				t.Fatal("private native error escaped worker", err)
			}
			wantResume := 1
			if scenario == "already_disconnected" {
				wantResume = 0
			}
			wantChecks := 0
			if scenario == "disconnect" || scenario == "cancel_and_disconnect" {
				wantChecks = 1
			}
			if resumes != wantResume || checks != wantChecks {
				t.Fatal("incorrect resume or recheck", resumes, checks)
			}
			after := m.store.Read()
			if !reflect.DeepEqual(after.ExitSelection, cfg.ExitSelection) || after.RPCState.ExitChange != nil {
				t.Fatal("resume manufactured or replaced durable selection")
			}
		})
	}
}
