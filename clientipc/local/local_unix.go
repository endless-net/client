//go:build linux || darwin

package local

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

func validateEndpoint(endpoint string) (string, error) {
	if endpoint == "" {
		endpoint = DefaultUnixSocket
		if runtime.GOOS == "darwin" {
			endpoint = DefaultDarwinSocket
		}
	}
	if !filepath.IsAbs(endpoint) {
		return "", errors.New("IPC endpoint must be an absolute Unix socket path")
	}
	return endpoint, nil
}

func listen(endpoint string) (net.Listener, error) {
	endpoint, err := validateEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	// The runtime/package owner creates the protected parent directory and
	// handles explicit stale-socket recovery; never delete a caller-supplied path.
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(endpoint, 0660); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

func dial(ctx context.Context, endpoint string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", endpoint)
}
