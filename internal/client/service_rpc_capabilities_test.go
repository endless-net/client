package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

func TestRPCConnectionCapabilityTracksWorkerAndRebootstrap(t *testing.T) {
	m := newRPCStoreTest(t)
	s := NewClientRPCService(m, nil)
	peer := local.Peer{Identity: "observer"}
	read := func(want bool) *ipc.RuntimeInfo {
		t.Helper()
		snapshot, err := m.snapshotAs(peer, s.build)
		if err != nil {
			t.Fatal(err)
		}
		info := snapshot.Runtime
		if (len(info.Capabilities) == 1) != want || len(info.Capabilities) > 1 {
			t.Fatal("capability does not follow actual worker readiness")
		}
		if want {
			capability := info.Capabilities[0]
			if capability.Capability != ipc.Capability_CAPABILITY_CONNECTION || capability.Platform != s.build.Platform || capability.Restriction.Availability != ipc.Availability_AVAILABILITY_AVAILABLE {
				t.Fatal("connection readiness advertised another capability or platform")
			}
		}
		m.mu.Lock()
		runtimeInfo := m.runtimeInfoLocked(peer, s.build, m.store.Read())
		m.mu.Unlock()
		if !proto.Equal(runtimeInfo, info) {
			t.Fatal("runtime bootstrap and stream disagree on capabilities")
		}
		return info
	}
	read(false)
	old, err := m.subscribe(peer, s.build, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(old)
	if _, err := old.next(t.Context()); err != nil {
		t.Fatal(err)
	}
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
	}, Start: func(context.Context, Config) error { return nil }}
	ctx, cancel := context.WithCancel(t.Context())
	done, err := s.StartProfileWorker(ctx, driver)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { cancel(); <-done }()
	s.profileMu.Lock()
	firstWorker := s.profileWorker
	s.profileMu.Unlock()
	waitCtx, stopWait := context.WithTimeout(t.Context(), 5*time.Second)
	defer stopWait()
	_, err = old.next(waitCtx)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	info := read(true)
	info.Capabilities[0].Restriction.Availability = ipc.Availability_AVAILABILITY_UNSUPPORTED
	read(true)
	fresh, err := m.subscribe(peer, s.build, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(fresh)
	opening, err := fresh.next(waitCtx)
	if err != nil || opening.Sequence != 1 || len(opening.GetSnapshot().GetRuntime().GetCapabilities()) != 1 {
		t.Fatal("new stream did not open with ready capability", err)
	}
	cancel()
	_, err = fresh.next(waitCtx)
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("worker did not stop on cancellation", err)
	}
	read(false)
	ctx2, cancel2 := context.WithCancel(t.Context())
	done2, err := s.StartProfileWorker(ctx2, driver)
	if err != nil {
		cancel2()
		t.Fatal(err)
	}
	defer func() { cancel2(); <-done2 }()
	// A delayed shutdown callback must not clear a replacement worker.
	m.setConnectionWorker(firstWorker, false)
	read(true)
	restarted, err := NewClientRPCMutations(m.store)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := restarted.snapshotAs(peer, s.build)
	if err != nil || len(snapshot.Runtime.Capabilities) != 0 {
		t.Fatal("readiness survived a new runtime without a worker", err)
	}
}

func TestRPCInvalidDriverDoesNotAdvertiseConnection(t *testing.T) {
	m := newRPCStoreTest(t)
	s := NewClientRPCService(m, nil)
	if _, err := s.StartProfileWorker(t.Context(), ClientRPCProfileDriver{}); err == nil {
		t.Fatal("incomplete connection driver accepted")
	}
	snapshot, err := m.snapshotAs(local.Peer{Identity: "observer"}, s.build)
	if err != nil || len(snapshot.Runtime.Capabilities) != 0 {
		t.Fatal("failed worker startup advertised connection", err)
	}
}
