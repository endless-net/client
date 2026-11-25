//go:build linux

package headless

import (
	"context"
	"errors"
	"time"

	"github.com/endless-net/client/internal/localipc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func OpenLinux(ctx context.Context, digest [32]byte) (*grpc.ClientConn, error) {
	instance, err := localipc.ReadLinuxInstance(localipc.LinuxRuntimeDirectory)
	if err != nil {
		return nil, errors.New("служба Client недоступна или команда требует sudo")
	}
	channel, err := localipc.ConnectLinux(ctx, localipc.LinuxSocketConfig{
		Directory: localipc.LinuxRuntimeDirectory, InstanceID: instance,
		ContractDigest: digest, HandshakeTimeout: 2 * time.Second,
	})
	if err != nil {
		if status.Code(err) == codes.FailedPrecondition {
			return nil, errors.New("версии службы и консольного клиента не совпадают; требуется согласованная установка")
		}
		return nil, errors.New("не удалось подключиться к доверенной локальной службе Client")
	}
	return channel, nil
}
