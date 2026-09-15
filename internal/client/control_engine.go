package client

import (
	"errors"
	"net/http"
	"slices"
)

// ControlPlaneHTTPClient snapshots the owned underlay while the caller holds
// the runtime effect lock. The caller must cancel requests before changing the
// runtime identity and close this client's idle connections after use.
func (e *WireGuardEngine) ControlPlaneHTTPClient(cfg Config) (*http.Client, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var mark uint32
	if e.exitGuard != nil {
		bound := e.exitConfig
		if bound.NodeID == "" || bound.NodeCredential == "" || !sameExitControlIdentity(cfg, bound) {
			return nil, errors.New("control underlay does not match protected runtime identity")
		}
		mark = e.exitGuard.mark
	} else if cfg.ExitSelection != nil {
		return nil, errors.New("control underlay requires protected exit recovery")
	}
	return newControlUnderlayHTTPClient(cfg.ControlURLs(), mark, nil)
}

func sameExitControlIdentity(a, b Config) bool {
	return a.NodeID == b.NodeID && a.NodeCredential == b.NodeCredential && a.NetworkID == b.NetworkID && slices.Equal(a.ControlURLs(), b.ControlURLs())
}
