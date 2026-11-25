//go:build windows

package clientapp

import (
	"net/http"
)

// NewHTTPClient prepares the native control transport without opening a socket.
// Control adapters keep their own trust/proof/response validation; this client
// adds no workload credential, cookie jar, retry or registration fallback.
func (s *Signals) NewHTTPClient(config Config) (*http.Client, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return s.newControlHTTPClient(config.CallTimeout, config.MaxConnections)
}
