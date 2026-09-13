package client

import (
	"context"
	"errors"
	"testing"

	ipc "github.com/endless-net/client/clientipc/v0"
)

func TestRPCEventSendRechecksUnpublishedOwnerChange(t *testing.T) {
	m, peer, _ := rpcConnectFixture(t)
	sub, err := m.subscribe(peer, &ipc.BuildIdentity{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	event, err := sub.next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = "replacement"; return nil }); err != nil {
		t.Fatal(err)
	}
	sent := false
	err = m.sendEvent(t.Context(), sub, event, func(*ipc.WatchEventsResponse) error { sent = true; return nil })
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	if sent {
		t.Fatal("private event sent after unpublished owner change")
	}
	if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = peer.Identity; return nil }); err != nil {
		t.Fatal(err)
	}
	// A closed sequence cannot resume merely because ownership changes back.
	err = m.sendEvent(t.Context(), sub, event, func(*ipc.WatchEventsResponse) error { sent = true; return nil })
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	if sent {
		t.Fatal("revoked sequence resumed")
	}
}

func TestRPCEventSendCancellationAndTransportFailure(t *testing.T) {
	for _, mode := range []string{"before", "during", "transport", "success"} {
		t.Run(mode, func(t *testing.T) {
			m, peer, _ := rpcConnectFixture(t)
			sub, err := m.subscribe(peer, &ipc.BuildIdentity{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.unsubscribe(sub)
			event, err := sub.next(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "before" {
				cancel()
			}
			transportErr := errors.New("transport failed")
			calls := 0
			err = m.sendEvent(ctx, sub, event, func(got *ipc.WatchEventsResponse) error {
				calls++
				if got != event {
					t.Fatal("wrong event sent")
				}
				// Acquiring both locks in the callback proves they are not held
				// across slow transport I/O (TryLock avoids a hanging regression).
				if !m.mu.TryLock() {
					t.Fatal("mutation lock held across send")
				}
				m.mu.Unlock()
				if !sub.mu.TryLock() {
					t.Fatal("subscriber lock held across send")
				}
				active := sub.sending
				sub.mu.Unlock()
				if !active {
					t.Fatal("active send not visible to revocation")
				}
				if mode == "during" {
					cancel()
				}
				if mode == "transport" {
					return transportErr
				}
				return nil
			})
			if mode == "before" && calls != 0 || mode != "before" && calls != 1 {
				t.Fatal("incorrect send count", calls)
			}
			switch mode {
			case "before", "during":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case "transport":
				if !errors.Is(err, transportErr) {
					t.Fatal(err)
				}
			case "success":
				if err != nil {
					t.Fatal(err)
				}
			}
			if sub.sending {
				t.Fatal("send marker leaked")
			}
		})
	}
}

func TestRPCEventRevocationDuringSendOverridesTransportResult(t *testing.T) {
	m, peer, _ := rpcConnectFixture(t)
	aborted := false
	sub, err := m.subscribe(peer, &ipc.BuildIdentity{}, func() { aborted = true })
	if err != nil {
		t.Fatal(err)
	}
	defer m.unsubscribe(sub)
	event, err := sub.next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = m.sendEvent(t.Context(), sub, event, func(*ipc.WatchEventsResponse) error {
		if !m.mu.TryLock() {
			t.Fatal("send prevents revocation")
		}
		defer m.mu.Unlock()
		if err := m.store.Update(func(cfg *Config) error { cfg.LocalOwnerID = "replacement"; return nil }); err != nil {
			t.Fatal(err)
		}
		m.publishMutationLocked(nil)
		return errors.New("aborted transport")
	})
	assertRPCFailure(t, err, ipc.ErrorCode_ERROR_CODE_OWNER_REQUIRED)
	if !aborted || sub.sending {
		t.Fatal("send revocation did not abort and clear active marker")
	}
}
