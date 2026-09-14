package client

import (
	"context"
	"reflect"
	"time"

	ipc "github.com/endless-net/client/clientipc/v0"
	"google.golang.org/protobuf/proto"
)

// Re-evaluate session deadlines independently of network observations. This
// never renews a session, changes tunnel intent or treats credentials as clocks.
func (m *ClientRPCMutations) publishSessionClock() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg := m.store.Read()
	changed := false
	for sub := range m.subscribers {
		sub.mu.Lock()
		closed := sub.closed
		sub.mu.Unlock()
		if closed {
			continue
		}
		snapshot, err := m.snapshotLocked(sub.peer, sub.build, cfg)
		if err != nil {
			return err
		}
		if !proto.Equal(snapshot.Status.GetSession(), sub.sessionProjection) {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	if err := m.store.Update(func(next *Config) error {
		if !reflect.DeepEqual(clonePersistentConfig(cfg), clonePersistentConfig(*next)) {
			return errRPCNoChange
		}
		if next.RPCState == nil {
			return errRPCNoChange
		}
		next.RPCState.Revision++
		return nil
	}); err != nil {
		if err == errRPCNoChange {
			return nil
		}
		return err
	}
	m.publishMutationLocked(nil, ipc.Domain_DOMAIN_SESSION)
	return nil
}

func cloneSessionProjection(session *ipc.Session) *ipc.Session {
	if session == nil {
		return nil
	}
	return proto.Clone(session).(*ipc.Session)
}

func (m *ClientRPCMutations) startSessionClock(ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-ticker.C:
				if err := m.publishSessionClock(); err != nil {
					done <- err
					return
				}
			}
		}
	}()
	return done
}
