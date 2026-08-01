//go:build linux || darwin

package client

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	ipc "github.com/endless-net/client/ipc/v1"
)

func DefaultLocalServiceSocketPath() string {
	if runtime.GOOS == "darwin" {
		return ipc.DefaultDarwinSocket
	}
	return ipc.DefaultUnixSocket
}

func ListenUnixServiceSocket(path string) (net.Listener, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultLocalServiceSocketPath()
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create service IPC socket directory %s: %w", dir, err)
	}
	if err := removeStaleUnixSocket(path); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("chmod service IPC socket %s: %w", path, err)
	}
	return &unixServiceSocketListener{Listener: listener, path: path}, nil
}

func removeStaleUnixSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect service IPC socket %s: %w", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("service IPC socket path %s exists and is not a socket", path)
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("service IPC socket %s is already in use", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale service IPC socket %s: %w", path, err)
	}
	return nil
}

type unixServiceSocketListener struct {
	net.Listener
	path string
}

func (l *unixServiceSocketListener) Close() error {
	err := l.Listener.Close()
	if strings.TrimSpace(l.path) != "" {
		if removeErr := os.Remove(l.path); err == nil && removeErr != nil && !os.IsNotExist(removeErr) {
			err = removeErr
		}
	}
	return err
}

func UnixServiceIPCConnContext(ctx context.Context, conn net.Conn) context.Context {
	peer, err := UnixServiceIPCPeerForConn(conn)
	if err != nil {
		return ctx
	}
	return ContextWithServiceIPCPeer(ctx, peer)
}
