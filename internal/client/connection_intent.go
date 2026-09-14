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
	DesiredState string `json:"desired_state"`
	Reason       string `json:"reason,omitempty"`
	UpdatedAt    string `json:"updated_at"`
}

type ConnectionIntentStore struct {
	config *ConfigStore
	now    func() time.Time
}

func NewConnectionIntentStore(config *ConfigStore) ConnectionIntentStore {
	return ConnectionIntentStore{config: config, now: time.Now}
}

// InitializeRuntimeIntent implements the runtime-start baseline: keep an
// explicit intent and start disconnected when none has been saved. The agent
// calls it under its lifetime lock, before starting workers or network effects.
func (s ConnectionIntentStore) InitializeRuntimeIntent() error {
	if s.config == nil {
		return errors.New("config store is required")
	}
	return s.config.Update(func(cfg *Config) error {
		if cfg.ConnectionIntent != nil {
			switch cfg.ConnectionIntent.DesiredState {
			case ConnectionIntentDesiredConnected, ConnectionIntentDesiredDisconnected:
				return nil
			default:
				return errors.New("unsupported saved runtime connection intent")
			}
		}
		cfg.ConnectionIntent = &ConnectionIntent{
			DesiredState: ConnectionIntentDesiredDisconnected,
			Reason:       "runtime_start_no_saved_intent",
			UpdatedAt:    s.now().UTC().Format(time.RFC3339),
		}
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
