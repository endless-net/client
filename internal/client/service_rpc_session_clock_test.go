package client

import (
	"context"
	"testing"
	"time"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
	"github.com/endless-net/client/clientipc/local"
	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRPCSessionClockPublishesOnlyTransitions(t *testing.T) {
	m, owner, _ := rpcConnectFixture(t)
	now := time.Now().UTC()
	base := now
	m.now = func() time.Time { return now }
	if err := m.store.Update(func(cfg *Config) error {
		cfg.Token = "user-token"
		cfg.ConnectionIntent = &ConnectionIntent{DesiredState: ConnectionIntentDesiredConnected}
		cfg.UserSession = &StoredUserSession{ControlOrigin: "https://control.test", TokenBinding: sessionTokenBinding(cfg.Token), Response: &backend.GetSessionResponse{Session: &backend.UserSession{SessionId: "session", UserId: "user", State: backend.UserSessionState_USER_SESSION_STATE_ACTIVE, WarningAt: timestamppb.New(base.Add(time.Minute)), ExpiresAt: timestamppb.New(base.Add(2 * time.Minute))}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sub, err := m.subscribe(owner, &ipc.BuildIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	observer, err := m.subscribe(local.Peer{Identity: "uid:2000"}, &ipc.BuildIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(observer)
	if _, err := sub.next(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.next(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i, want := range []ipc.SessionState{ipc.SessionState_SESSION_STATE_EXPIRING, ipc.SessionState_SESSION_STATE_EXPIRED} {
		before := m.Metadata().Revision
		now = base.Add(time.Duration(i+1) * time.Minute)
		if err := m.publishSessionClock(); err != nil {
			t.Fatal(err)
		}
		event, err := sub.next(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if event.GetStatusChanged().GetSession().GetState() != want || event.Metadata.Revision != before+1 {
			t.Fatal("missing clock transition")
		}
		invalidated, err := sub.next(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if invalidated.GetInvalidated().GetDomain() != ipc.Domain_DOMAIN_SESSION {
			t.Fatal("session domain not invalidated")
		}
		public, err := observer.next(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if public.GetStatusChanged().Session != nil || len(observer.queue) != 0 {
			t.Fatal("observer received session state or invalidation")
		}
		if err := m.publishSessionClock(); err != nil {
			t.Fatal(err)
		}
		if len(sub.queue) != 0 || m.Metadata().Revision != before+1 {
			t.Fatal("unchanged clock emitted duplicate events")
		}
		if m.store.Read().ConnectionIntent.DesiredState != ConnectionIntentDesiredConnected {
			t.Fatal("session expiry changed tunnel intent")
		}
	}
}

func TestRPCSessionClockStopsOnCancellation(t *testing.T) {
	m := newRPCStoreTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := m.startSessionClock(ctx)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("session clock did not stop")
	}
}
