package publicationproxy

import (
	"context"
	"net/http"
	"sync"
	"time"
)

type requestEpoch struct {
	ctx     context.Context
	cancel  context.CancelFunc
	active  int
	drained chan struct{}
	fenced  bool
}

// A single control loop installs epochs. The gate tracks requests on existing
// HTTP/2 connections as well as newly accepted connections.
type routeGate struct {
	next       http.Handler
	mu         sync.Mutex
	generation *requestEpoch
}

func (g *routeGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	epoch := g.generation
	if epoch == nil || epoch.fenced || epoch.ctx.Err() != nil {
		g.mu.Unlock()
		logPublicationRequest(r.Context(), nil, "access_denied", "", 0)
		http.NotFound(w, r)
		return
	}
	epoch.active++
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		epoch.active--
		if epoch.fenced && epoch.active == 0 {
			close(epoch.drained)
		}
	}()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	finished := make(chan struct{})
	stop := context.AfterFunc(epoch.ctx, func() {
		defer close(finished)
		cancel()
		// Release a blocked upload or slow reader before acknowledging withdrawal.
		rc := http.NewResponseController(w)
		now := time.Now()
		_ = rc.SetReadDeadline(now)
		_ = rc.SetWriteDeadline(now)
	})
	defer func() {
		if !stop() {
			<-finished
		}
	}()
	g.next.ServeHTTP(w, r.WithContext(ctx))
}

// A timeout leaves the old epoch fenced. The next call still waits for its
// requests to return before installing or acknowledging any new generation.
func (g *routeGate) replace(ctx context.Context, install func() error) error {
	g.mu.Lock()
	old := g.generation
	if old != nil && !old.fenced {
		old.fenced = true
		old.cancel()
		if old.active == 0 {
			close(old.drained)
		}
	}
	g.mu.Unlock()
	if old != nil {
		select {
		case <-old.drained:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if install == nil {
		return nil
	}
	if err := install(); err != nil {
		return err
	}
	c, cancel := context.WithCancel(ctx)
	g.mu.Lock()
	g.generation = &requestEpoch{ctx: c, cancel: cancel, drained: make(chan struct{})}
	g.mu.Unlock()
	return nil
}
