package client

import (
	"crypto/sha256"
	"encoding/hex"

	backend "github.com/endless-net/client-api/clientapi/v1/clientrpc"
)

// StoredUserSession is private core state in the protected ConfigStore. Backend
// authority uses producer types, never a local IPC message or duplicated DTO.
type StoredUserSession struct {
	ControlOrigin string                      `json:"control_origin"`
	TokenBinding  string                      `json:"token_binding"`
	Response      *backend.GetSessionResponse `json:"response"`
}

func sessionTokenBinding(token string) string {
	if token == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func clearStaleUserSessions(cfg *Config) {
	if cfg.UserSession != nil && (cfg.Token == "" || cfg.UserSession.TokenBinding != sessionTokenBinding(cfg.Token)) {
		cfg.UserSession = nil
	}
	if cfg.RPCState != nil {
		for id, profile := range cfg.RPCState.Profiles {
			if profile.Configuration.UserSession != nil && (profile.Configuration.Token == "" || profile.Configuration.UserSession.TokenBinding != sessionTokenBinding(profile.Configuration.Token)) {
				profile.Configuration.UserSession = nil
				cfg.RPCState.Profiles[id] = profile
			}
		}
	}
}
