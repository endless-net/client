package testcontrol

import (
	"net"
	"testing"
)

// NewTLS is the explicit loopback fixture for native profiles, whose control
// origin must be HTTPS. Consumers must trust TLSCertificatePEM, never skip
// verification. Existing HTTP-only lower-level fixtures are not silently changed.
func NewTLS(t testing.TB) *Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return NewWithListener(t, listener)
}
