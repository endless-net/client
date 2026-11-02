//go:build windows

package clientapp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"unicode"

	"github.com/endless-net/client/internal/state"
	"go.uber.org/fx"
)

var ErrProtectedState = errors.New("client protected state unavailable")

// ProtectedState binds the existing Windows backend to Fx. Store is available
// for constructor wiring, but refuses operations before successful startup and
// after shutdown begins. Startup performs the explicit format migration before
// Load; it never calls Initialize, resets state or generates keys.
type ProtectedState struct {
	store  *state.Store
	gate   *stateGate
	config Config
}

func NewProtectedState(lifecycle fx.Lifecycle, signals *Signals, config Config) (*ProtectedState, error) {
	if lifecycle == nil || !signals.valid() {
		return nil, ErrConfig
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	protector, err := state.NewDPAPI(config.InstallationID)
	if err != nil {
		return nil, ErrProtectedState
	}
	idle := make(chan struct{})
	close(idle)
	gate := &stateGate{idle: idle, closed: make(chan struct{})}
	store, err := state.New(gate, protector, config.InstallationID, config.MaxStatePlainBytes, config.MaxStateCipherBytes)
	if err != nil {
		return nil, ErrProtectedState
	}
	s := &ProtectedState{store: store, gate: gate, config: config}
	lifecycle.Append(signals.hook("state", s.start, gate.stop))
	return s, nil
}

func (s *ProtectedState) Store() *state.Store { return s.store }

func (s *ProtectedState) start(ctx context.Context) error {
	if _, bounded := ctx.Deadline(); !bounded || ctx.Err() != nil {
		return ErrProtectedState
	}
	g := s.gate
	g.mu.Lock()
	if g.used || g.stopping {
		g.mu.Unlock()
		return ErrProtectedState
	}
	g.used = true
	g.mu.Unlock()
	backend, err := state.OpenWindows(s.config.StateDirectory, s.config.ServiceSID, s.config.StateLockRetry)
	if err != nil {
		return ErrProtectedState
	}
	// Validate through a temporary store before publishing the backend. This
	// prevents constructor consumers from observing unverified startup state.
	protector, err := state.NewDPAPI(s.config.InstallationID)
	if err != nil {
		_ = backend.Close()
		return ErrProtectedState
	}
	startup, err := state.New(backend, protector, s.config.InstallationID, s.config.MaxStatePlainBytes, s.config.MaxStateCipherBytes)
	if err != nil {
		_ = backend.Close()
		return ErrProtectedState
	}
	if err := startup.Upgrade(ctx); err != nil {
		_ = backend.Close()
		return ErrProtectedState
	}
	snapshot, err := startup.Load(ctx)
	if err != nil || !s.accepts(snapshot) || ctx.Err() != nil {
		_ = backend.Close()
		return ErrProtectedState
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.stopping || ctx.Err() != nil {
		_ = backend.Close()
		return ErrProtectedState
	}
	g.backend = backend
	g.life, g.cancel = context.WithCancel(context.Background())
	g.admitting = true
	return nil
}

func (s *ProtectedState) accepts(snapshot state.State) bool {
	c := s.config
	if len(snapshot.Profiles) > c.MaxProfiles || (snapshot.OwnerSID != "" && !configSID(snapshot.OwnerSID)) {
		return false
	}
	for _, p := range snapshot.Profiles {
		if !controlOrigin(p.ControlOrigin) || strings.TrimSpace(p.DisplayName) == "" || len(p.DisplayName) > c.MaxDisplayNameBytes || strings.IndexFunc(p.DisplayName, unicode.IsControl) >= 0 || len(p.Hostname) > c.MaxHostnameBytes || strings.IndexFunc(p.Hostname, unicode.IsControl) >= 0 || len(p.Trust.Bundle) > c.MaxBootstrapBytes || len(p.Secrets.RegistrationRequest) > c.MaxRequestBytes || len(p.Secrets.NodeCredential) > c.MaxCredentialBytes {
			return false
		}
	}
	return true
}

type stateGate struct {
	mu                        sync.Mutex
	backend                   *state.WindowsBackend
	life                      context.Context
	cancel                    context.CancelFunc
	used, admitting, stopping bool
	closing                   bool
	active                    int
	idle, closed              chan struct{}
	closeErr                  error
}

func (g *stateGate) Exclusive(ctx context.Context, operation func(state.Slot) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	if !g.admitting || g.stopping {
		g.mu.Unlock()
		return ErrProtectedState
	}
	if g.active == 0 {
		g.idle = make(chan struct{})
	}
	g.active++
	backend, life := g.backend, g.life
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.active--
		if g.active == 0 {
			close(g.idle)
		}
		g.mu.Unlock()
	}()
	call, cancel := context.WithCancel(ctx)
	stopCancellation := context.AfterFunc(life, cancel)
	defer stopCancellation()
	defer cancel()
	return backend.Exclusive(call, operation)
}

func (g *stateGate) stop(ctx context.Context) error {
	g.mu.Lock()
	g.stopping, g.admitting = true, false
	if g.cancel != nil {
		g.cancel()
	}
	if !g.closing {
		g.closing = true
		backend, idle := g.backend, g.idle
		go func() {
			// Never invalidate OS handles underneath a still-active state
			// transaction. If it ignores cancellation, Stop fails by deadline.
			<-idle
			var err error
			if backend != nil {
				err = backend.Close()
			}
			g.mu.Lock()
			if err != nil {
				g.closeErr = ErrProtectedState
			}
			g.mu.Unlock()
			close(g.closed)
		}()
	}
	g.mu.Unlock()
	if _, bounded := ctx.Deadline(); !bounded {
		return g.failStop()
	}
	select {
	case <-ctx.Done():
		return g.failStop()
	case <-g.closed:
		if ctx.Err() != nil {
			return g.failStop()
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.closeErr
	}
}

func (g *stateGate) failStop() error {
	g.mu.Lock()
	g.closeErr = ErrProtectedState
	g.mu.Unlock()
	return ErrProtectedState
}
