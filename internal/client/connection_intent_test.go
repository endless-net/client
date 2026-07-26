package client

import (
	"path/filepath"
	"testing"
	"time"
)

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
