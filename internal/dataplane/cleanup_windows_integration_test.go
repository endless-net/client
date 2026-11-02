//go:build windows && integration

package dataplane

import (
	"testing"
	"time"

	"github.com/endless-net/client/internal/state"
	"github.com/google/uuid"
)

// Actual production cleanup of a prepared journal whose create was never
// dispatched. Native operations are GetIfTable2Ex only: no DLL load, Wintun,
// route/address mutation, WFP engine, installed state or service access.
func TestWindowsPreparedJournalRecoveryReadOnly(t *testing.T) {
	session, err := uuid.NewRandom()
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PreparedWindowsAdapter(session.String())
	if err != nil {
		t.Fatal(err)
	}
	store, _, original := protectedCleanupStore(t, &state.ResourceJournal{ID: prepared.ID, Generation: 1, Adapter: &prepared})
	cleanup, err := NewWindowsCleanup(store, WindowsCleanupConfig{MaxInventoryRows: 256, MaxInterfaces: 256, MaxProtected: 8, TransactionWait: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx := cleanupContext(t)
	if err := cleanup.Run(ctx, cleanupProfile, prepared.ID, 1); err != nil {
		t.Fatal(err)
	}
	value, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p := value.Profiles[cleanupProfile]
	if p.Connection.Journal != nil || p.Connection.Generation != original.Connection.Generation || p.Connection.Intent != original.Connection.Intent || p.Secrets != original.Secrets {
		t.Fatal("native read-only recovery changed registration/intent")
	}
}
