package clientapp

import (
	"context"
	"net"
	"syscall"
	"time"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"go.uber.org/fx"
)

func newLinuxIPC(lifecycle fx.Lifecycle, signals *Signals, config localipc.LinuxSocketConfig, owner localipc.OwnerReader, service clientv0.ClientServiceServer, maxMessageBytes, maxConnections int, maxConcurrent uint32, drain time.Duration) (*IPC, error) {
	if !signals.valid() {
		return nil, ErrIPC
	}
	server, err := localipc.NewLinuxServer(config, owner, service, maxMessageBytes, maxConcurrent,
		localipc.Observation{Unary: signals.Unary, Stream: signals.Stream})
	if err != nil {
		return nil, err
	}
	ipc, err := newIPC(lifecycle, signals, server, func() (net.Listener, error) {
		return localipc.ListenLinux(config)
	}, func(ctx context.Context) error {
		conn, err := localipc.ConnectLinux(ctx, config)
		if err != nil {
			return err
		}
		return conn.Close()
	}, maxConnections, drain)
	if err != nil {
		return nil, err
	}
	var remove func() error
	lifecycle.Append(signals.hook("ipc", func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		remove, err = localipc.PublishLinuxInstance(config)
		return err
	}, func(context.Context) error {
		return remove()
	}))
	return ipc, nil
}

func ipcSupportsCredentials(connection net.Conn) bool {
	_, ok := connection.(syscall.Conn)
	return ok
}

func (c *ipcConnection) SyscallConn() (syscall.RawConn, error) {
	return c.Conn.(syscall.Conn).SyscallConn()
}
