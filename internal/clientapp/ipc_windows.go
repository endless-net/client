//go:build windows

package clientapp

import (
	"context"
	"net"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"go.uber.org/fx"
)

// NewIPC validates the transport without opening a pipe. Explicit allowed SIDs
// and the service SID come from installation configuration, never RPC metadata.
func NewIPC(lifecycle fx.Lifecycle, signals *Signals, config localipc.PipeConfig, allowed []string, owner localipc.OwnerReader, service clientv0.ClientServiceServer, maxMessageBytes, maxConnections int, maxConcurrent uint32, drain time.Duration) (*IPC, error) {
	if !signals.valid() || len(allowed) == 0 {
		return nil, ErrIPC
	}
	server, err := localipc.NewServer(config, owner, service, maxMessageBytes, maxConcurrent, localipc.Observation{Unary: signals.Unary, Stream: signals.Stream})
	if err != nil {
		return nil, ErrIPC
	}
	allowed = append([]string(nil), allowed...)
	return newIPC(lifecycle, signals, server, func() (net.Listener, error) {
		return localipc.Listen(config, allowed)
	}, func(ctx context.Context) error {
		conn, err := localipc.Connect(ctx, config)
		if err != nil {
			return err
		}
		return conn.Close()
	}, maxConnections, drain)
}

func ipcSupportsCredentials(connection net.Conn) bool {
	_, ok := connection.(interface{ Fd() uintptr })
	return ok
}

// The Windows credentials must inspect the original pipe handle. A generic
// net.Conn wrapper that drops Fd would disable OS peer authentication entirely.
func (c *ipcConnection) Fd() uintptr { return c.Conn.(interface{ Fd() uintptr }).Fd() }
