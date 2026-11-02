package connectivity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequestedFullSnapshotKeepsDurableAntiRollbackFloor(t *testing.T) {
	f := newMapFixture(t)
	f.value.Revision.Network = 2
	f.value.Network.Revision = 2
	v, err := NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	first, err := v.Verify(context.Background(), f.sign(t, nil, f.now, f.now.Add(30*time.Second)), nil)
	if err != nil {
		t.Fatal(err)
	}
	floor := first.Cursor()
	f.profile.Connection.MapCursor = &floor
	v, err = NewVerifier(f.profile, f.config)
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	raw := f.sign(t, nil, f.now, f.now.Add(30*time.Second))
	if _, err := v.Verify(context.Background(), raw, nil); !errors.Is(err, ErrResync) {
		t.Fatal("ordinary stream silently accepted From=0 after cursor", err)
	}
	full, err := v.VerifySnapshot(context.Background(), raw, nil)
	if err != nil || full.Cursor().Revision.Network != 2 {
		t.Fatal("requested full snapshot could not retain floor", err)
	}
	f.value.Revision.Network = 1
	f.value.Network.Revision = 1
	f.now = f.now.Add(time.Second)
	if _, err := v.VerifySnapshot(context.Background(), f.sign(t, nil, f.now, f.now.Add(30*time.Second)), nil); !errors.Is(err, ErrStale) {
		t.Fatal("full snapshot bypassed rollback floor", err)
	}
}
