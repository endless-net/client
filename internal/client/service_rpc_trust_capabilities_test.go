package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	clientapi "github.com/endless-net/client-api/clientapi/v1"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCTrustCapabilityTracksExecutorLifetime(t *testing.T) {
	m := newRPCStoreTest(t)
	s := NewClientRPCService(m, nil)
	peer := local.Peer{Identity: "observer"}
	assertReady := func(ready bool) {
		t.Helper()
		snapshot, err := m.snapshotAs(peer, s.build)
		if err != nil {
			t.Fatal(err)
		}
		caps := snapshot.Runtime.Capabilities
		if !ready {
			if len(caps) != 0 {
				t.Fatal("inactive trust executor advertised readiness")
			}
			return
		}
		if len(caps) != 1 || caps[0].Capability != ipc.Capability_CAPABILITY_IDENTITY_RECOVERY || caps[0].Restriction.Availability != ipc.Availability_AVAILABILITY_AVAILABLE {
			t.Fatal("running trust executor missing readiness")
		}
	}
	driver := ClientRPCProfileDriver{Lock: &sync.Mutex{}, Stop: func(context.Context) (ipc.ConnectionContinuity, error) {
		return ipc.ConnectionContinuity_CONNECTION_CONTINUITY_NOT_APPLICABLE, nil
	}}
	assertReady(false)
	if _, err := s.StartTrustWorker(t.Context(), driver); err == nil {
		t.Fatal("missing providers accepted")
	}
	assertReady(false)
	s.ServerIdentityProvider = func(context.Context, Config) (clientapi.SigningTrustBundle, error) {
		t.Error("readiness performed identity discovery")
		return clientapi.SigningTrustBundle{}, nil
	}
	if _, err := s.StartTrustWorker(t.Context(), driver); err == nil {
		t.Fatal("missing renewal provider accepted")
	}
	assertReady(false)
	s.TrustRecoveryProvider = func(context.Context, Config) (ClientRPCTrustRecoveryResult, error) {
		t.Error("readiness performed credential renewal")
		return ClientRPCTrustRecoveryResult{}, nil
	}
	var previous *clientRPCProfileWorker
	for i := 0; i < 2; i++ {
		old, err := m.subscribe(peer, s.build, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer m.unsubscribe(old)
		wait, stop := context.WithTimeout(t.Context(), 5*time.Second)
		defer stop()
		if _, err := old.next(wait); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done, err := s.StartTrustWorker(ctx, driver)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		defer cancel()
		if previous != nil {
			m.setWorkerCapabilities(previous, false, ipc.Capability_CAPABILITY_IDENTITY_RECOVERY)
		}
		assertReady(true)
		_, err = old.next(wait)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		s.trustMu.Lock()
		previous = s.trustWorker
		s.trustMu.Unlock()
		fresh, err := m.subscribe(peer, s.build, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer m.unsubscribe(fresh)
		if _, err := fresh.next(wait); err != nil {
			t.Fatal(err)
		}
		cancel()
		_, err = fresh.next(wait)
		assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_STALE_STATE)
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-wait.Done():
			t.Fatal("trust executor did not stop")
		}
		assertReady(false)
	}
}
