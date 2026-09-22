package client

import (
	"context"

	api "github.com/endless-net/client-api/clientapi/v1"
)

// A one-shot change source. Loss, cancellation and closure must invalidate it;
// reconnecting must create a new stream and a new topology snapshot.
type exitLANChangeStream interface {
	Changed() <-chan struct{}
	Err() error
	Close() error
}

type exitLANSourceLifetime struct {
	ctx    context.Context
	stream exitLANChangeStream
}

func (l *exitLANSourceLifetime) current() bool {
	if l == nil || l.ctx == nil || l.ctx.Err() != nil || l.stream == nil || l.stream.Changed() == nil {
		return false
	}
	if l.stream.Err() != nil || l.ctx.Err() != nil {
		return false
	}
	select {
	case <-l.stream.Changed():
		return false
	default:
		return true
	}
}

// Subscribe before the first snapshot. This rejects reported changes even when
// both snapshots look identical (delete/recreate, or route away and back).
// Notification delivery is asynchronous: this is not packet-time instance
// binding or Wi-Fi association evidence, and cannot authorize a LAN exception.
func captureWatchedExitLANSource(ctx context.Context, own string, family api.ExitFamilyMode, open func(context.Context) (exitLANChangeStream, error), capture func(context.Context, string, api.ExitFamilyMode) (*exitLANSource, error)) (*exitLANSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !exitLANSourceFamilyValid(family) || !exitLANInterfaceName(own) || own == "lo" || open == nil || capture == nil {
		return nil, errExitLANSource
	}
	stream, err := open(ctx)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return nil, err
	}
	lifetime := &exitLANSourceLifetime{ctx: ctx, stream: stream}
	keep := false
	defer func() {
		if !keep && stream != nil {
			_ = stream.Close()
		}
	}()
	if !lifetime.current() {
		return nil, errExitLANSource
	}
	source, err := capture(ctx, own, family)
	if err != nil {
		return nil, err
	}
	if source == nil || source.OwnInterface != own || source.Family != family || !lifetime.current() {
		return nil, errExitLANSource
	}
	source.lifetime = lifetime
	keep = true
	return source, nil
}

// The snapshot owner must close its stream when abandoning the preparation.
// Copies deliberately share the same irreversible lifetime.
func (s *exitLANSource) close() error {
	if s == nil || s.lifetime == nil || s.lifetime.stream == nil {
		return nil
	}
	return s.lifetime.stream.Close()
}
