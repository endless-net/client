//go:build windows

package dataplane

import "testing"

func TestNativeObservationNeverWaitsForSDKOrConfusesListenerWithPath(t *testing.T) {
	s, _, _, m, _, _, _, _, _ := wiredSessionFixture(t)
	if err := startSessionFixture(t, s, m); err != nil {
		t.Fatal(err)
	}
	v := s.ConnectionLocal()
	if !v.Known || !v.Applied || !v.Permission.NewFlows || v.MapRevision != m.Cursor().Revision.Network || v.DirectPaths != 0 {
		t.Fatal("listener inferred path or measured apply lost", v)
	}
	s.mu.Lock()
	busy := s.ConnectionLocal()
	s.mu.Unlock()
	if busy.Known || busy.Applied {
		t.Fatal("busy native transition claimed current application")
	}
	s.BlockSource()
	v = s.ConnectionLocal()
	if !v.Known || !v.Applied || v.Permission.NewFlows || v.Permission.ExistingFlows {
		t.Fatal("blocked source retained current authority", v)
	}
	if err := s.Stop(cleanupContext(t)); err != nil {
		t.Fatal(err)
	}
	if v := s.ConnectionLocal(); v.Applied || v.Permission.ExistingFlows {
		t.Fatal("closed native owner stayed applied", v)
	}
}
