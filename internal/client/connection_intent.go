package client

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ConnectionIntentDesiredConnected    = "connected"
	ConnectionIntentDesiredDisconnected = "disconnected"
)

type ConnectionIntent struct {
	DesiredState    string                      `json:"desired_state"`
	Reason          string                      `json:"reason,omitempty"`
	UpdatedAt       string                      `json:"updated_at"`
	StartupRecovery *clientRuntimeStartRecovery `json:"startup_recovery,omitempty"`
}

type ConnectionIntentStore struct {
	config *ConfigStore
	now    func() time.Time
}

func NewConnectionIntentStore(config *ConfigStore) ConnectionIntentStore {
	return ConnectionIntentStore{config: config, now: time.Now}
}

// InitializeRuntimeIntent resolves startup preferences before workers or network
// effects, under the agent lifetime lock. Unavailable policy keeps IPC recovery
// accessible while the network loop remains disconnected.
func (s ConnectionIntentStore) InitializeRuntimeIntent() error {
	if s.config == nil {
		return errors.New("config store is required")
	}
	return s.config.Update(func(cfg *Config) error {
		if cfg.ConnectionIntent != nil {
			switch cfg.ConnectionIntent.DesiredState {
			case ConnectionIntentDesiredConnected, ConnectionIntentDesiredDisconnected:
			default:
				return errors.New("unsupported saved runtime connection intent")
			}
		}
		cfg.ConnectionIntent = runtimeStartWithRecovery(*cfg, s.now())
		return nil
	})
}

func (s ConnectionIntentStore) Load() (ConnectionIntent, bool, error) {
	if s.config == nil {
		return ConnectionIntent{}, false, errors.New("config store is required")
	}
	cfg := s.config.Read()
	if cfg.ConnectionIntent == nil {
		return ConnectionIntent{}, false, nil
	}
	return *cfg.ConnectionIntent, true, nil
}

func (s ConnectionIntentStore) Disconnected() (ConnectionIntent, bool, error) {
	intent, ok, err := s.Load()
	if err != nil || !ok {
		return intent, false, err
	}
	switch strings.ToLower(strings.TrimSpace(intent.DesiredState)) {
	case ConnectionIntentDesiredDisconnected:
		return intent, true, nil
	case "", ConnectionIntentDesiredConnected:
		return intent, false, nil
	default:
		return intent, false, fmt.Errorf("unsupported connection intent desired_state %q", intent.DesiredState)
	}
}

func (s ConnectionIntentStore) SetDisconnected(reason string) error {
	if s.config == nil {
		return errors.New("config store is required")
	}
	intent := ConnectionIntent{
		DesiredState: ConnectionIntentDesiredDisconnected,
		Reason:       strings.TrimSpace(reason),
		UpdatedAt:    s.now().UTC().Format(time.RFC3339),
	}
	return s.config.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = &intent
		return nil
	})
}

func (s ConnectionIntentStore) Clear() error {
	if s.config == nil {
		return errors.New("config store is required")
	}
	return s.config.Update(func(cfg *Config) error {
		cfg.ConnectionIntent = nil
		return nil
	})
}
