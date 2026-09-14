package client

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRuntimeStartPreservesExplicitIntentAndDefaultsDisconnected(t *testing.T) {
	for _, desired := range []string{"absent", ConnectionIntentDesiredConnected, ConnectionIntentDesiredDisconnected, "", "invalid"} {
		t.Run(desired, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "client.json")
			config, err := OpenConfigStore(path)
			if err != nil {
				t.Fatal(err)
			}
			if desired != "absent" {
				if err := config.Update(func(cfg *Config) error {
					cfg.ConnectionIntent = &ConnectionIntent{DesiredState: desired, Reason: "explicit_user_choice", UpdatedAt: "2026-09-14T00:00:00Z"}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before := config.Read()
			store := NewConnectionIntentStore(config)
			now := time.Date(2026, 9, 14, 1, 0, 0, 0, time.UTC)
			store.now = func() time.Time { return now }
			err = store.InitializeRuntimeIntent()
			if desired == "" || desired == "invalid" {
				if err == nil || !reflect.DeepEqual(before, config.Read()) {
					t.Fatal("invalid saved intent was accepted or replaced", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if desired == "absent" {
				before.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredDisconnected, Reason: "runtime_start_no_saved_intent", UpdatedAt: now.Format(time.RFC3339)}
			}
			if !reflect.DeepEqual(before.ConnectionIntent, config.Read().ConnectionIntent) {
				t.Fatal("startup did not preserve the expected intent")
			}
			reopened, err := OpenConfigStore(path)
			if err != nil {
				t.Fatal(err)
			}
			restarted := NewConnectionIntentStore(reopened)
			if err := restarted.InitializeRuntimeIntent(); err != nil {
				t.Fatal(err)
			}
			intent, disconnected, err := restarted.Disconnected()
			if err != nil || !reflect.DeepEqual(&intent, before.ConnectionIntent) || disconnected != (desired != ConnectionIntentDesiredConnected) {
				t.Fatal("restart changed the agent's network-loop decision", err, disconnected)
			}
		})
	}
}

func TestRuntimeStartReadsIntentCommittedAfterStoreWasOpened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.json")
	config, err := OpenConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := OpenConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewConnectionIntentStore(writer).SetDisconnected("user_disconnect"); err != nil {
		t.Fatal(err)
	}
	store := NewConnectionIntentStore(config)
	if err := store.InitializeRuntimeIntent(); err != nil {
		t.Fatal(err)
	}
	intent, disconnected, err := store.Disconnected()
	if err != nil || !disconnected || intent.Reason != "user_disconnect" || !reflect.DeepEqual(config.Read().ConnectionIntent, writer.Read().ConnectionIntent) {
		t.Fatal("startup used a stale store snapshot or replaced newer intent", err)
	}
}

func TestConnectionIntentStorePersistsDisconnectedIntentInClientState(t *testing.T) {
	config, err := OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	store := NewConnectionIntentStore(config)
	store.now = func() time.Time { return now }

	if err := store.SetDisconnected("user_disconnect"); err != nil {
		t.Fatal(err)
	}
	intent, disconnected, err := store.Disconnected()
	if err != nil {
		t.Fatal(err)
	}
	if !disconnected || intent.DesiredState != ConnectionIntentDesiredDisconnected || intent.Reason != "user_disconnect" || intent.UpdatedAt != now.Format(time.RFC3339) {
		t.Fatalf("intent=%#v disconnected=%v, want persisted disconnected intent", intent, disconnected)
	}
	persisted := config.Read().ConnectionIntent
	if persisted == nil || *persisted != intent {
		t.Fatalf("persisted intent = %#v, want %#v", persisted, intent)
	}
}

func TestConnectionIntentStoreClearRemovesIntentFromClientState(t *testing.T) {
	config, err := OpenConfigStore(filepath.Join(t.TempDir(), "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	store := NewConnectionIntentStore(config)
	if err := store.SetDisconnected("test"); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	_, disconnected, err := store.Disconnected()
	if err != nil {
		t.Fatal(err)
	}
	if disconnected || config.Read().ConnectionIntent != nil {
		t.Fatal("cleared intent still reports disconnected")
	}
}

func TestConnectionIntentStoreRequiresConfigStore(t *testing.T) {
	store := NewConnectionIntentStore(nil)
	if err := store.InitializeRuntimeIntent(); err == nil {
		t.Fatal("InitializeRuntimeIntent accepted a nil config store")
	}
	if _, _, err := store.Disconnected(); err == nil {
		t.Fatal("Disconnected accepted a nil config store")
	}
	if err := store.SetDisconnected("test"); err == nil {
		t.Fatal("SetDisconnected accepted a nil config store")
	}
	if err := store.Clear(); err == nil {
		t.Fatal("Clear accepted a nil config store")
	}
}
