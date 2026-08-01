//go:build linux || darwin

package v2

import (
	"context"
	"net"
	"net/http"
	"runtime"
	"strings"
)

func NewLocalClient(endpoint string) (*Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		endpoint = DefaultUnixSocket
		if runtime.GOOS == "darwin" {
			endpoint = DefaultDarwinSocket
		}
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", endpoint)
	}}
	return NewClient(&http.Client{Transport: transport}), nil
}
