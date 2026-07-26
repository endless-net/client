//go:build windows

package v1

import (
	"context"
	"net"
	"net/http"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func NewLocalClient(endpoint string) (*Client, error) {
	if endpoint == "" {
		endpoint = DefaultWindowsPipe
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return winio.DialPipeAccessImpLevel(ctx, endpoint, windows.GENERIC_READ|windows.GENERIC_WRITE, winio.PipeImpLevelImpersonation)
	}}
	return NewClient(&http.Client{Transport: transport}), nil
}
